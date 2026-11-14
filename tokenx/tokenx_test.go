package tokenx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testSecret = "approval-secret-0"

var goodExpiry = time.Unix(2000000000, 0).UTC() // 2033-05-18T03:33:20Z

// signGood 签一张标准令牌（载荷四键：两端共同约定的字段契约形态）。
func signGood(t *testing.T) string {
	t.Helper()
	tok, err := Sign(testSecret, Claims{
		"approval_id": "ap-42",
		"case_id":     "cs-7",
		"host":        "worker-1",
		"cmd_hash":    CanonicalCmdHash([]string{"kubectl get pods", "systemctl restart api"}),
	}, goodExpiry)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return tok
}

// split2 拆令牌两段。
func split2(t *testing.T, tok string) (string, string) {
	t.Helper()
	parts := strings.Split(tok, ".")
	if len(parts) != 2 {
		t.Fatalf("令牌形态非法: %q", tok)
	}
	return parts[0], parts[1]
}

// macFor 用测试 secret 对任意 body 文本求 MAC 段（构造「MAC 合法但载荷
// 非本内核形态」的令牌用）。
func macFor(t *testing.T, body string) string {
	t.Helper()
	sum := hmac.New(sha256.New, []byte(testSecret))
	sum.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(sum.Sum(nil))
}

// flipChar 翻转段首字符（保持 base64url 字母表内）。
func flipChar(s string) string {
	if s[0] == 'A' {
		return "B" + s[1:]
	}
	return "A" + s[1:]
}

// TestSignVerifyRoundTrip 契约：签发-校验闭环，载荷绑定子集通过、完整
// 载荷（含 exp）解出、令牌形态为两段无填充 base64url。
func TestSignVerifyRoundTrip(t *testing.T) {
	cmdHash := CanonicalCmdHash([]string{"kubectl get pods", "systemctl restart api"})
	tok := signGood(t)

	if segs := strings.Count(tok, "."); segs != 1 {
		t.Fatalf("令牌应为两段式（1 个点），got %d 个点", segs)
	}
	if strings.ContainsAny(tok, "=+/") {
		t.Fatalf("两段应为 Raw URL base64（无填充、无 +/），got %q", tok)
	}

	claims, err := Verify(testSecret, tok, Claims{
		"approval_id": "ap-42", // 校验端只绑定本次执行关心的键
		"cmd_hash":    cmdHash,
		"host":        "worker-1",
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if want := strconv.FormatInt(goodExpiry.Unix(), 10); claims[ClaimExp] != want {
		t.Fatalf("exp = %q，应为 %q", claims[ClaimExp], want)
	}
	if claims["approval_id"] != "ap-42" || claims["case_id"] != "cs-7" {
		t.Fatalf("载荷解出失真: %+v", claims)
	}
}

// TestSignDeterministic 契约：同 claims 签出逐字节相同的令牌（载荷键序
// 由 encoding/json 钉死，map 构造顺序无关）。
func TestSignDeterministic(t *testing.T) {
	a, err := Sign(testSecret, Claims{"case_id": "cs-7", "approval_id": "ap-42"}, goodExpiry)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	b, err := Sign(testSecret, Claims{"approval_id": "ap-42", "case_id": "cs-7"}, goodExpiry)
	if err != nil {
		t.Fatalf("Sign(2): %v", err)
	}
	if a != b {
		t.Fatalf("同 claims 应同令牌：%q vs %q", a, b)
	}
}

// TestTamperMustFail 契约：篡改载荷、篡改 MAC、跨令牌拼接载荷——各形态
// 必败且归 ErrBadMAC（MAC 罩住载荷文本）。
func TestTamperMustFail(t *testing.T) {
	tok := signGood(t)
	body, mac := split2(t, tok)

	if _, err := Verify(testSecret, flipChar(body)+"."+mac, nil); !errors.Is(err, ErrBadMAC) {
		t.Fatalf("篡改载荷应 ErrBadMAC，got %v", err)
	}
	if _, err := Verify(testSecret, body+"."+flipChar(mac), nil); !errors.Is(err, ErrBadMAC) {
		t.Fatalf("篡改 MAC 应 ErrBadMAC，got %v", err)
	}

	// 两张各自合法的令牌拼接载荷段：必败（MAC 覆盖的是各自载荷文本）。
	other, err := Sign(testSecret, Claims{"approval_id": "ap-99"}, goodExpiry)
	if err != nil {
		t.Fatalf("Sign(other): %v", err)
	}
	otherBody, _ := split2(t, other)
	if _, err := Verify(testSecret, otherBody+"."+mac, nil); !errors.Is(err, ErrBadMAC) {
		t.Fatalf("跨令牌拼接载荷应 ErrBadMAC，got %v", err)
	}
}

// TestBadTokenFormsMustFail 契约：形态非法各形态必败且哨兵可判定。
func TestBadTokenFormsMustFail(t *testing.T) {
	goodBody, goodMac := split2(t, signGood(t))
	badB64 := "!!bad-base64!!" // base64url 解不开的 body 文本

	cases := []struct {
		name  string
		token string
		want  error
	}{
		{"空令牌", "", ErrBadToken},
		{"单段", "only-body", ErrBadToken},
		{"三段", "a.b.c", ErrBadToken},
		{"空 body", ".abc", ErrBadToken},
		{"空 mac", "abc.", ErrBadToken},
		{"body 解不开", badB64 + "." + macFor(t, badB64), ErrBadToken},
		{"载荷非 JSON", base64.RawURLEncoding.EncodeToString([]byte("not-json")) + "." + macFor(t, base64.RawURLEncoding.EncodeToString([]byte("not-json"))), ErrBadToken},
		{"mac 解不开", goodBody + ".!!!", ErrBadToken},
		{"篡改 body", flipChar(goodBody) + "." + goodMac, ErrBadMAC},
		{"篡改 mac", goodBody + "." + flipChar(goodMac), ErrBadMAC},
	}
	for _, c := range cases {
		if _, err := Verify(testSecret, c.token, nil); !errors.Is(err, c.want) {
			t.Fatalf("%s (%q): 应 %v，got %v", c.name, c.token, c.want, err)
		}
	}
}

// TestBindingMustFail 契约：载荷绑定失败各形态必败——绑定键缺失、值不符、
// 命令集变更（cmd_hash 与本地执行计划不一致）；且绑定判定先于时效判定。
func TestBindingMustFail(t *testing.T) {
	tok := signGood(t)

	if _, err := Verify(testSecret, tok, Claims{"approval_id": "ap-42", "trace_id": "t-1"}); !errors.Is(err, ErrBinding) {
		t.Fatalf("缺绑定键应 ErrBinding，got %v", err)
	}
	if _, err := Verify(testSecret, tok, Claims{"approval_id": "ap-43"}); !errors.Is(err, ErrBinding) {
		t.Fatalf("绑定值不符应 ErrBinding，got %v", err)
	}
	// 命令集变更：令牌签的是 A 命令集，本地执行计划已是 B。
	otherHash := CanonicalCmdHash([]string{"rm -rf /"})
	if _, err := Verify(testSecret, tok, Claims{"cmd_hash": otherHash}); !errors.Is(err, ErrBinding) {
		t.Fatalf("命令集变更应 ErrBinding，got %v", err)
	}

	// 绑定先于时效：过期 + 绑定错 → ErrBinding。
	past := goodExpiry.Add(-time.Hour)
	expired, err := Sign(testSecret, Claims{"approval_id": "ap-42"}, past)
	if err != nil {
		t.Fatalf("Sign(expired): %v", err)
	}
	if _, err := VerifyAt(testSecret, expired, Claims{"approval_id": "ap-43"}, past.Add(time.Minute)); !errors.Is(err, ErrBinding) {
		t.Fatalf("绑定应先于时效判定，got %v", err)
	}
}

// TestExpiryBoundary 契约：有效期含端点——now == exp 仍有效、now > exp
// 判过期；MAC 合法但缺 exp 按形态非法拒绝（fail-closed）。
func TestExpiryBoundary(t *testing.T) {
	tok, err := Sign(testSecret, Claims{"approval_id": "ap-42"}, goodExpiry)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if _, err := VerifyAt(testSecret, tok, nil, goodExpiry.Add(-time.Second)); err != nil {
		t.Fatalf("有效期内应通过，got %v", err)
	}
	if _, err := VerifyAt(testSecret, tok, nil, goodExpiry); err != nil {
		t.Fatalf("now == exp 应仍有效（含端点），got %v", err)
	}
	if _, err := VerifyAt(testSecret, tok, nil, goodExpiry.Add(time.Second)); !errors.Is(err, ErrExpired) {
		t.Fatalf("过期应 ErrExpired，got %v", err)
	}

	// 手工构造缺 exp 的 MAC 合法令牌 → ErrBadToken。
	raw, _ := json.Marshal(map[string]string{"approval_id": "x"})
	body := base64.RawURLEncoding.EncodeToString(raw)
	if _, err := Verify(testSecret, body+"."+macFor(t, body), nil); !errors.Is(err, ErrBadToken) {
		t.Fatalf("缺 exp 应 ErrBadToken，got %v", err)
	}
}

// TestFailClosed 契约：空 secret 签发与校验一律拒绝（不存在无钥降级）。
func TestFailClosed(t *testing.T) {
	if _, err := Sign("", Claims{"a": "b"}, goodExpiry); !errors.Is(err, ErrNoSecret) {
		t.Fatalf("空 secret 签发应 ErrNoSecret，got %v", err)
	}
	tok := signGood(t)
	if _, err := Verify("", tok, nil); !errors.Is(err, ErrNoSecret) {
		t.Fatalf("空 secret 校验应 ErrNoSecret，got %v", err)
	}
}

// TestReservedAndZeroExpiry 保留键与零过期时刻 fail-fast（仓规总则①）。
func TestReservedAndZeroExpiry(t *testing.T) {
	if _, err := Sign(testSecret, Claims{ClaimExp: "123"}, goodExpiry); err == nil {
		t.Fatal("claims 携带保留键 exp 应拒绝")
	}
	if _, err := Sign(testSecret, Claims{"a": "b"}, time.Time{}); !errors.Is(err, ErrBadExpiry) {
		t.Fatalf("零值 expiresAt 应 ErrBadExpiry，got %v", err)
	}
}

// TestCanonicalCmdHash 契约：规范化规则钉死——逐词 TrimSpace（首尾空白
// 不进哈希）、词内空白保留、命令序敏感（序列不是集合）、不合并重复、
// 不丢空词、跨「进程」可复现。
func TestCanonicalCmdHash(t *testing.T) {
	want := func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])
	}

	if got := CanonicalCmdHash([]string{" systemctl restart api"}); got != want("systemctl restart api") {
		t.Fatalf("TrimSpace 规则漂移: %s", got)
	}
	if got := CanonicalCmdHash([]string{"echo a  b"}); got != want("echo a  b") {
		t.Fatalf("词内空白应保留: %s", got)
	}
	if CanonicalCmdHash([]string{"a", "b"}) == CanonicalCmdHash([]string{"b", "a"}) {
		t.Fatal("命令序敏感：序列语义不得被集合化")
	}
	if got := CanonicalCmdHash([]string{"a", "", " b "}); got != want("a\n\nb") {
		t.Fatalf("空词与去重规则漂移: %s", got)
	}
	if got := CanonicalCmdHash(nil); got != want("") {
		t.Fatalf("空命令集应 sha256(\"\"): %s", got)
	}

	// 跨「进程」复现：签发端与校验端各算一遍，TrimSpace 后同哈希。
	h1 := CanonicalCmdHash([]string{"kubectl get pods", " systemctl restart api "})
	h2 := CanonicalCmdHash([]string{" kubectl get pods", "systemctl restart api"})
	if h1 != h2 {
		t.Fatalf("两端规范化后应同哈希：%s vs %s", h1, h2)
	}
}
