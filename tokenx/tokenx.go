package tokenx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ClaimExp 保留键：过期时刻（unix 秒），由 [Sign] 写入、[Verify] 判定。
// 调用方 claims 携带同名键按冲突拒绝（fail-fast，仓规总则①）。
const ClaimExp = "exp"

// 哨兵错误：各失败形态 errors.Is 可判定（文案带上下文）。
var (
	ErrNoSecret  = errors.New("tokenx: secret 未配置（fail-closed）")
	ErrBadExpiry = errors.New("tokenx: 过期时间未设置")
	ErrBadToken  = errors.New("tokenx: 令牌形态非法")
	ErrBadMAC    = errors.New("tokenx: MAC 校验失败")
	ErrBinding   = errors.New("tokenx: 载荷绑定不符")
	ErrExpired   = errors.New("tokenx: 令牌已过期")
)

// Claims 载荷键值对：两端共同约定的字段契约（如 approval_id/case_id/
// cmd_hash/host）。值必须是普通字符串；键序不影响签发结果
// （encoding/json 字典序钉死，同 claims 签出同令牌）。
type Claims map[string]string

// Sign 签发能力令牌：载荷 = claims + exp（由 expiresAt 折算），令牌 =
// `base64url(payload) + "." + base64url(HMAC-SHA256(secret, payload 文本))`。
// 同 claims/expiresAt 签出逐字节相同的令牌（载荷键序无关）。secret 为空
// 拒绝签发（fail-closed）；expiresAt 零值拒绝（疑似漏填，非「立即过期」
// 的合法用法）；claims 携带保留键 [ClaimExp] 拒绝。
func Sign(secret string, claims Claims, expiresAt time.Time) (string, error) {
	if secret == "" {
		return "", ErrNoSecret
	}
	if expiresAt.IsZero() {
		return "", ErrBadExpiry
	}
	if _, reserved := claims[ClaimExp]; reserved {
		return "", fmt.Errorf("tokenx: %s 是保留键（exp 由 Sign 写入）", ClaimExp)
	}
	payload := make(map[string]string, len(claims)+1)
	for k, v := range claims {
		payload[k] = v
	}
	payload[ClaimExp] = strconv.FormatInt(expiresAt.Unix(), 10)

	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("tokenx: 载荷编码失败: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// Verify 校验并解出载荷。三段判定次序钉死（契约见包文档）：
// 常数时间 MAC → 载荷绑定（bind 每个键在载荷中存在且值相等）→ 时效。
// 返回的 Claims 含 exp。bind 为 nil 表示只验 MAC 与时效；secret 为空拒绝
// 校验（fail-closed，不存在无钥降级）。
func Verify(secret, token string, bind Claims) (Claims, error) {
	return VerifyAt(secret, token, bind, time.Now())
}

// VerifyAt = [Verify] 且显式指定「当前时刻」：校验端时钟可注入（测试/
// 跨机时钟漂移演练）。判定次序与 [Verify] 相同。
func VerifyAt(secret, token string, bind Claims, now time.Time) (Claims, error) {
	if secret == "" {
		return nil, ErrNoSecret
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, ErrBadToken
	}
	body, macB64 := parts[0], parts[1]

	// 第一段：常数时间 MAC（crypto/hmac.Equal），MAC 罩住载荷文本。
	macBytes, err := base64.RawURLEncoding.DecodeString(macB64)
	if err != nil {
		return nil, ErrBadToken
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	if !hmac.Equal(mac.Sum(nil), macBytes) {
		return nil, ErrBadMAC
	}

	// MAC 已过：解码载荷。
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, ErrBadToken
	}
	var claims Claims
	err = json.Unmarshal(raw, &claims)
	if err != nil {
		return nil, ErrBadToken
	}

	// 第二段：载荷绑定——挪用的令牌在这里死（先于时效：绑定错说明令牌
	// 被挪用，比「过期」更可判）。
	for k, want := range bind {
		got, ok := claims[k]
		if !ok {
			return nil, fmt.Errorf("%w: 缺少绑定键 %q", ErrBinding, k)
		}
		if got != want {
			return nil, fmt.Errorf("%w: 绑定键 %q 不符", ErrBinding, k)
		}
	}

	// 第三段：时效。本包签出的令牌必带 exp；MAC 合法却缺 exp 说明不是
	// 本内核签出的令牌，按形态非法拒绝（fail-closed）。
	expStr, ok := claims[ClaimExp]
	if !ok {
		return nil, fmt.Errorf("%w: 缺少 %s 键", ErrBadToken, ClaimExp)
	}
	expUnix, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: %s 键不是 unix 秒", ErrBadToken, ClaimExp)
	}
	if now.Unix() > expUnix {
		return nil, ErrExpired
	}
	return claims, nil
}

// CanonicalCmdHash 命令集规范化哈希：逐词 TrimSpace（首尾空白不进哈希、
// 词内空白保留）→ "\n" 连接 → sha256 十六进制。规则钉死见包文档：
// 命令序敏感（序列不是集合）、不合并重复、不丢空词——所写即所批。
// 空表 = sha256("")，仍是确定值（签发端校验端同规则同结果）。
func CanonicalCmdHash(cmds []string) string {
	trimmed := make([]string, len(cmds))
	for i, c := range cmds {
		trimmed[i] = strings.TrimSpace(c)
	}
	sum := sha256.Sum256([]byte(strings.Join(trimmed, "\n")))
	return hex.EncodeToString(sum[:])
}
