package webhook

// per-platform 验签器注册表（#270）：平台路由分发用，container.SafeMap
// 背书（并发读写安全）。下游 653 行私有实现里的 WebhookValidatorRegistry
// 原语化落点。

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/oekit/pkg/container"
)

// Registry per-platform 验签器注册表：按平台名路由到对应 Validator。
// 请求进来先按平台标识（header / URL 路径段 / 配置路由，由服务端自行
// 判定）取验证器，再裁决。
type Registry struct {
	m *container.SafeMap[string, Validator]
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{m: container.NewSafeMap[string, Validator]()}
}

// Register 注册/替换平台的验证器（后写生效——轮换签名密钥时以同名替换
// 平滑热更）。platform 空串或 validator 为 nil 是构造期配置错误，panic
// （fail-fast，仓规总则①）。
func (rg *Registry) Register(platform string, v Validator) {
	if platform == "" {
		panic("webhook: Registry.Register 的 platform 为空串（fail-fast，仓规总则①）")
	}
	if v == nil {
		panic(fmt.Sprintf("webhook: 平台 %q 注册的 Validator 为 nil（fail-fast，仓规总则①）", platform))
	}
	rg.m.Set(platform, v)
}

// Validator 取平台验证器（不存在返回 false——调用方自行决定拒绝语义；
// 要统一错误用 Validate）。
func (rg *Registry) Validator(platform string) (Validator, bool) {
	return rg.m.Get(platform)
}

// Validate 按平台路由验签。平台未注册返回带平台名的 ErrUnknownPlatform
// （errors.Is 可判定）——「没注册」是服务端配置/路由问题，与签名对错
// 分属两类，不得混报 ErrBadSignature。
func (rg *Registry) Validate(platform string, r *http.Request, body []byte) error {
	v, ok := rg.m.Get(platform)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownPlatform, platform)
	}
	return v.Validate(r, body)
}

// Platforms 已注册平台名的有序快照（整体拷贝出口，经 SafeMap.Snapshot
// 锁内拷贝、锁外交付；调试转储「现在注册了哪些平台」一行完成）。
func (rg *Registry) Platforms() []string {
	snap := rg.m.Snapshot()
	names := make([]string, 0, len(snap))
	for name := range snap {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
