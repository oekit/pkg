package webhook

// HMAC 验签器家族（#270）：任意算法（crypto.Hash 暴露 sha1/sha256/384/512
// 全族）/任意 header（含回退链）/可选前缀/hex 或 base64 编码——GitHub
// sha256=<hex> 双形态、Forgejo/Gitea 裸 hex、非主流平台的自定义约定，全部
// 落在同一个原语上。

import (
	"crypto"
	"crypto/hmac"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

// HMACValidator 泛型 HMAC 验签器：签名值 = enc(hmac(algo, key=secret,
// message=请求体))，按序在 header 链上取第一个在场的值。
type HMACValidator struct {
	secret    []byte
	algo      crypto.Hash
	headers   []string // 主 header 在前，回退链在后
	prefix    string   // 可选签名值前缀（如 "sha256="，大小写不敏感剥离）
	useBase64 bool     // false = hex（缺省），true = base64（std）
}

// HMACOption HMACValidator 的构造选项（仓规禁裸 Option）。
type HMACOption func(*HMACValidator)

// WithFallbackHeaders 主 header 缺席时的回退 header（按序消歧——分叉平台
// 的 header 约定漂移：Forgejo 老版本用 Gitea 的 header，「谁在用谁」由
// 调用方按序声明）。重复声明主 header 不去重（无害：命中即停）。
func WithFallbackHeaders(names ...string) HMACOption {
	return func(v *HMACValidator) {
		for _, n := range names {
			if n == "" {
				panic("webhook: WithFallbackHeaders 含空 header 名（fail-fast，仓规总则①）")
			}
			v.headers = append(v.headers, n)
		}
	}
}

// WithBase64 签名值按 base64（std）解码；缺省 hex。Gitee 家族是 base64、
// GitHub/Forgejo/Gitea/GitCode 家族是 hex——按平台约定显式声明。
func WithBase64() HMACOption {
	return func(v *HMACValidator) { v.useBase64 = true }
}

// WithSignaturePrefix 签名值前缀（如 GitHub 的 "sha256="）：比对时大小写
// 不敏感地剥离；前缀在场但不匹配时**不剥离**、交由裸解码失败拒绝——
// fail-closed（sha1= 的旧签名喂给 sha256 验签器必须死，不能当裸 hex 活）。
func WithSignaturePrefix(prefix string) HMACOption {
	return func(v *HMACValidator) {
		if prefix == "" {
			panic("webhook: WithSignaturePrefix 前缀为空串（fail-fast，仓规总则①）")
		}
		v.prefix = prefix
	}
}

// NewHMACValidator 构造泛型 HMAC 验签器。
//
// secret 为空是构造期配置错误且**安全错误**：**空密钥使 HMAC 验证对可预测
// 载荷形同虚设**——panic fail-closed（#270 纪律第 2 条）。algo 支持
// crypto.SHA256/crypto.SHA384/crypto.SHA512（sha1/sha224 仅存量平台兼容
// 用），算法白名单外的值非法 panic；header 为空串非法 panic。
func NewHMACValidator(secret []byte, algo crypto.Hash, header string, opts ...HMACOption) *HMACValidator {
	if len(secret) == 0 {
		panic("webhook: HMAC secret 为空——空密钥使 HMAC 验证对可预测载荷形同虚设，拒绝构造（fail-closed）")
	}
	switch algo {
	case crypto.SHA1, crypto.SHA224, crypto.SHA256, crypto.SHA384, crypto.SHA512:
	default:
		panic(fmt.Sprintf("webhook: 不支持的哈希算法 crypto.Hash(%d)——HMAC 家族限 SHA-1/224/256/384/512（fail-fast，仓规总则①）", uint(algo)))
	}
	if header == "" {
		panic("webhook: HMAC 的 header 为空串（fail-fast，仓规总则①）")
	}
	v := &HMACValidator{
		secret:  append([]byte(nil), secret...),
		algo:    algo,
		headers: []string{header},
	}
	for _, opt := range opts {
		if opt == nil {
			panic("webhook: HMACOption 为 nil（fail-fast，仓规总则①）")
		}
		opt(v)
	}
	return v
}

// Validate 按 header 链取首个在场签名，剥前缀、解码、常量时间比对。
func (v *HMACValidator) Validate(r *http.Request, body []byte) error {
	val := ""
	for _, h := range v.headers {
		if got := r.Header.Get(h); got != "" {
			val = got
			break
		}
	}
	if val == "" {
		return ErrMissingSignature
	}
	got, err := decodeSignature(val, v.prefix, v.useBase64)
	if err != nil {
		return err
	}
	if !hmac.Equal(v.sum(body), got) {
		return ErrBadSignature
	}
	return nil
}

func (v *HMACValidator) sum(body []byte) []byte {
	mac := hmac.New(v.algo.New, v.secret)
	mac.Write(body)
	return mac.Sum(nil)
}

// decodeSignature 签名值统一解码：trim → 前缀剥离（大小写不敏感、匹配才
// 剥）→ hex/base64 解码。解码失败一律 ErrBadSignature（fail-closed，不
// 区分「编码坏」与「值坏」——攻击面不靠错误文案区分）。
func decodeSignature(val, prefix string, useBase64 bool) ([]byte, error) {
	val = strings.TrimSpace(val)
	if prefix != "" {
		if len(val) > len(prefix) && strings.EqualFold(val[:len(prefix)], prefix) {
			val = val[len(prefix):]
		}
	}
	var (
		got []byte
		err error
	)
	if useBase64 {
		got, err = base64.StdEncoding.DecodeString(val)
	} else {
		got, err = hex.DecodeString(val)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: 签名值解码失败", ErrBadSignature)
	}
	return got, nil
}
