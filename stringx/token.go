package stringx

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"

	"github.com/oekit/pkg/errors"
)

// randRead 随机源（测试注入点：模拟 crypto/rand 失败；生产恒为
// crypto/rand.Read）。
var randRead = rand.Read

// TokenEncoding 是 [RandomToken] 的令牌字节编码格式。
type TokenEncoding int

const (
	// TokenHex 小写十六进制（encoding/hex）：每个字节 2 字符，nBytes=16
	// 得 32 字符。字符集最保守（0-9a-f），日志 / URL / header 全场景无歧义。
	TokenHex TokenEncoding = iota

	// TokenBase64Std 标准 base64（encoding/base64 StdEncoding，含 +/ 与
	// = padding）：非 URL 安全，适合入库、报文等不经 URL 的场景。
	TokenBase64Std

	// TokenBase64RawURL URL 安全 base64、无 padding（RawURLEncoding）：
	// 字符集 A-Za-z0-9-_，适合放进 URL path/query 与 HTTP header。
	TokenBase64RawURL
)

// valid 报告 enc 是否为已定义的 TokenEncoding。
func (e TokenEncoding) valid() bool {
	switch e {
	case TokenHex, TokenBase64Std, TokenBase64RawURL:
		return true
	}
	return false
}

// RandomToken 生成 nBytes 字节纯随机令牌并按 enc 编码（#528）。
//
// 与 [GenerateID] 的分工：GenerateID = 时间戳前缀 + 4 字节随机的**可读 ID**
// （日志关联、临时标识这类非严格唯一场景）；本函数是**纯随机令牌**——
// 分布式锁 ownerID、版本/幂等令牌、会话 ID 等安全性敏感或必须不可预测的
// 场景，没有时间戳语义，不接受降级。
//
// 长度（nBytes 字节随机性恒定，编码只影响字符数）：
//   - TokenHex：2×nBytes 字符；
//   - TokenBase64Std / TokenBase64RawURL：4×⌈nBytes/3⌉ 字符（std 带 Equal
//     padding，raw URL 无 padding）。
//
// 错误（仓规总则①③）：
//   - nBytes <= 0 或 enc 非法：参数非法，返回 error——确定性编程错误，
//     fail-fast，不做任何隐式替换；
//   - crypto/rand 读取失败：显式上抛，**不静默退化**（Go 1.24+ 的
//     crypto/rand.Read 保证填满且不返回错误，熵源不可用直接 panic 在其
//     内部——此分支仅为显式契约，理论不可达；静默退化会产出可预测令牌，
//     对安全令牌是不可接受的下界）。需要降级语义的调用方请用
//     [RandomTokenOrFallback]，降级策略由调用方显式提供。
func RandomToken(nBytes int, enc TokenEncoding) (string, error) {
	if err := validateTokenParams(nBytes, enc); err != nil {
		return "", err
	}
	b, err := randomTokenBytes(nBytes)
	if err != nil {
		return "", err
	}
	return encodeTokenBytes(b, enc)
}

// RandomTokenOrFallback 是 [RandomToken] 的显式降级变体（#528）：随机源
// 失败时改返回 fallback() 的结果——fallback 由调用方提供（如退回
// [GenerateID] 时间戳 ID），包不做隐式退化。
//
// 返回值语义：
//   - err 非 nil：仅参数/用法错误（nBytes<=0、enc 非法、fallback 为 nil）
//     ——确定性编程错误**不触发降级**（fallback 每次都吞掉同一个 bug 会让
//     假令牌静默常态化），原样上抛；
//   - err 为 nil 且值为 fallback 结果：仅随机源失败这一运行期失败发生时，
//     按调用方的显式选择降级。降级成功返回 nil error——若随值带回原始
//     错误，`if err != nil` 风格的调用方会把降级值当失败丢掉，本变体形同
//     虚设；调用方选择降级即已知晓失败可能，需要观测时应在 fallback 内
//     自行记录。
func RandomTokenOrFallback(nBytes int, enc TokenEncoding, fallback func() string) (string, error) {
	if fallback == nil {
		return "", errors.Newf("stringx: RandomTokenOrFallback 的 fallback 不能为 nil")
	}
	if err := validateTokenParams(nBytes, enc); err != nil {
		return "", err
	}
	b, err := randomTokenBytes(nBytes)
	if err != nil {
		return fallback(), nil
	}
	return encodeTokenBytes(b, enc)
}

// validateTokenParams 校验 RandomToken 两参数（两入口共用一套口径）。
func validateTokenParams(nBytes int, enc TokenEncoding) error {
	if nBytes <= 0 {
		return errors.Newf("stringx: nBytes 必须为正，实得 %d", nBytes)
	}
	if !enc.valid() {
		return errors.Newf("stringx: 非法 TokenEncoding %d", int(enc))
	}
	return nil
}

// randomTokenBytes 取 nBytes 字节加密级随机数。
func randomTokenBytes(nBytes int) ([]byte, error) {
	b := make([]byte, nBytes)
	if _, err := randRead(b); err != nil {
		// 随机源失败显式上抛不静默退化：令牌的价值在不可预测性，降级产物
		// （时间戳/固定值）是可预测的，等于把安全令牌换成了明文标签。
		return nil, errors.Wrapf(err, "stringx: 生成随机令牌的 crypto/rand 读取失败")
	}
	return b, nil
}

// encodeTokenBytes 按 enc 编码随机字节（enc 合法性已由 validateTokenParams
// 保证，default 分支为防御兜底）。
func encodeTokenBytes(b []byte, enc TokenEncoding) (string, error) {
	switch enc {
	case TokenHex:
		return hex.EncodeToString(b), nil
	case TokenBase64Std:
		return base64.StdEncoding.EncodeToString(b), nil
	case TokenBase64RawURL:
		return base64.RawURLEncoding.EncodeToString(b), nil
	default:
		return "", errors.Newf("stringx: 非法 TokenEncoding %d", int(enc))
	}
}
