package webhook

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"net/http"
)

// 验签错误哨兵（errors.Is 判定；语义见包文档）。
var (
	// ErrMissingSignature 验签 header 整体缺席。Fallback 组合子据此落到
	// 下一个候选模式；到达调用方时一般视作「非本平台/未配签名」拒绝。
	ErrMissingSignature = errors.New("webhook: 验签 header 缺失")
	// ErrBadSignature 签名在场但不匹配（含前缀不符、编码解码失败）——
	// 硬失败，绝不降级到其他模式（防同 secret 交叉认证）。
	ErrBadSignature = errors.New("webhook: 签名不匹配")
	// ErrStaleTimestamp 时间戳缺席、非法或超出容忍窗——防重放硬失败。
	ErrStaleTimestamp = errors.New("webhook: 时间戳缺失/非法/超出容忍窗")
	// ErrUnknownPlatform 注册表查无此平台。
	ErrUnknownPlatform = errors.New("webhook: 平台未注册")
)

// Validator 单个 webhook 验签器。
//
// r 是已收到的请求（验签器只读 header 与 URL，不读 body）；body 是服务端
// 已读出的**原始**请求体字节（框架各异，读体不是原语的职责）。实现必须
// 并发安全，且全部比对走常量时间（crypto/hmac.Equal）。
type Validator interface {
	Validate(r *http.Request, body []byte) error
}

// ValidatorFunc 函数适配器——让普通函数当验证器（如调用方先做平台路由
// 再委托既有校验逻辑）。
type ValidatorFunc func(r *http.Request, body []byte) error

// Validate 实现 Validator。
func (f ValidatorFunc) Validate(r *http.Request, body []byte) error {
	return f(r, body)
}

// Fallback 按序尝试多个验证器的回退组合子——多平台 header 回退与多模
// （签名/密码）消歧的单一实现。
//
// 裁决语义（多模消歧的优先序，#270 现场纪律第 5 条）：
//
//   - 候选返回 ErrMissingSignature（它的验签 header 缺席）→ **落下一个
//     候选**——「该模式不在场」不是失败，是让位；
//   - 候选返回**任何其他错误**（ErrBadSignature/ErrStaleTimestamp/自定义）
//     → **立即整体失败**，不再尝试后续候选——签名 header 在场即裁决，
//     错签硬失败不降级：把签名 header 换成伪造的静态令牌 header 不能
//     「降级」洗白，同 secret 交叉认证被优先序挡死；
//   - 全部候选都 ErrMissingSignature → 返回 ErrMissingSignature。
//
// 空候选列表是构造期配置错误，panic（仓规总则①）。候选内的 nil 元素
// 同样 panic——静默跳过会把「配了个空模式」藏成永远让位。
func Fallback(validators ...Validator) Validator {
	if len(validators) == 0 {
		panic("webhook: Fallback 需要至少一个候选验证器（fail-fast，仓规总则①）")
	}
	for i, v := range validators {
		if v == nil {
			panic(fmt.Sprintf("webhook: Fallback 候选 #%d 为 nil（fail-fast，仓规总则①）", i))
		}
	}
	return fallbackValidator{candidates: append([]Validator(nil), validators...)}
}

type fallbackValidator struct {
	candidates []Validator
}

func (f fallbackValidator) Validate(r *http.Request, body []byte) error {
	for _, v := range f.candidates {
		err := v.Validate(r, body)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrMissingSignature) {
			continue // 该模式不在场：让位给下一候选
		}
		return err // 在场但判死：整体失败，不降级
	}
	return ErrMissingSignature
}

// StaticTokenValidator 静态令牌验签：比对 header 值与配置令牌的**字面
// 相等**（常量时间）——Gitee/GitCode 密码模式、简单平台的固定令牌形态。
// 与 HMAC 签名模式的分工由 Fallback 组合子裁决（签名优先）。
type StaticTokenValidator struct {
	header string
	token  []byte
}

// NewStaticTokenValidator 构造静态令牌验签器。
//
// token 为空是构造期配置错误且**安全错误**：空令牌等于「带着验签器放行
// 一切」——panic fail-closed（与 HMAC 空 secret 同口径，仓规总则①）。
func NewStaticTokenValidator(header string, token []byte) *StaticTokenValidator {
	if header == "" {
		panic("webhook: StaticToken 的 header 为空串（fail-fast，仓规总则①）")
	}
	if len(token) == 0 {
		panic("webhook: StaticToken 的令牌为空——空令牌使验签形同虚设，拒绝构造（fail-closed）")
	}
	return &StaticTokenValidator{header: header, token: append([]byte(nil), token...)}
}

// Validate 常量时间比对 header 值与配置令牌。
func (v *StaticTokenValidator) Validate(r *http.Request, body []byte) error {
	got := r.Header.Get(v.header)
	if got == "" {
		return ErrMissingSignature
	}
	if !hmac.Equal([]byte(got), v.token) {
		return ErrBadSignature
	}
	return nil
}
