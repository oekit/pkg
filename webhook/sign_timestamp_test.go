package webhook

// 时间戳签名器测试（#325/#328）：Gitee 平台对 NewGiteeSigner↔
// NewGiteeValidator round-trip、自定义 message 对、URL 编码对、时间戳
// 毫秒形态、构造期 fail-closed。

import (
	"crypto"
	"errors"
	"testing"
	"time"
)

// TestTimestampHMACSigner_GiteePairing Gitee 平台配对：发出侧毫秒时间戳 +
// HMAC-base64 喂给 NewGiteeValidator（纯签名模式）必过；错 secret / 超窗
// / 篡改 body（自定义 message 形态）判死。
func TestTimestampHMACSigner_GiteePairing(t *testing.T) {
	const secret = "gitee-pairing-secret"
	body := []byte(`{"ref":"refs/heads/main"}`)
	signer := NewGiteeSigner([]byte(secret), TimestampSignerWithClock(func() time.Time { return fixedClock }))
	validator := NewGiteeValidator([]byte(secret), nil, WithClock(func() time.Time { return fixedClock }))

	hdr, err := signer.Sign(body)
	if err != nil {
		t.Fatalf("Sign 不应出错, got %v", err)
	}
	req := mkReq(t, map[string]string{
		"X-Gitee-Timestamp": hdr.Get("X-Gitee-Timestamp"),
		"X-Gitee-Token":     hdr.Get("X-Gitee-Token"),
	}, body)
	if err := validator.Validate(req, body); err != nil {
		t.Fatalf("Gitee 配对 round-trip 应通过, got %v", err)
	}
	// 时间戳是毫秒形态（Gitee 官方 13 位）
	if got := hdr.Get("X-Gitee-Timestamp"); got != itoa(fixedClock.UnixMilli()) {
		t.Fatalf("时间戳应为毫秒形态 %s, got %s", itoa(fixedClock.UnixMilli()), got)
	}

	// 错 secret 的签名判死
	bad := NewGiteeSigner([]byte("other"), TimestampSignerWithClock(func() time.Time { return fixedClock }))
	hdrBad, _ := bad.Sign(body)
	req = mkReq(t, map[string]string{
		"X-Gitee-Timestamp": hdrBad.Get("X-Gitee-Timestamp"),
		"X-Gitee-Token":     hdrBad.Get("X-Gitee-Token"),
	}, body)
	if err := validator.Validate(req, body); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("错 secret 签名应 ErrBadSignature, got %v", err)
	}
}

// TestTimestampHMACSigner_CustomMessagePairing body 参与签名的平台形态：
// message = timestamp + body，SignerWithMessage 与收侧 WithMessage 用同
// 一个组装（两侧共用同一份钩子——配对漂移在构造期就被排除）。
func TestTimestampHMACSigner_CustomMessagePairing(t *testing.T) {
	const secret = "msg-secret"
	join := func(ts string, body []byte) []byte { return []byte(ts + string(body)) }
	signer := NewTimestampHMACSigner([]byte(secret), crypto.SHA256, "TS", "SIG",
		SignerWithMessage(join),
		TimestampSignerWithClock(func() time.Time { return fixedClock }))
	validator := NewTimestampHMACValidator([]byte(secret), crypto.SHA256, "TS", "SIG",
		WithMessage(join), WithClock(func() time.Time { return fixedClock }))

	body := []byte(`{"n":1}`)
	hdr, _ := signer.Sign(body)
	req := mkReq(t, map[string]string{
		"TS":  hdr.Get("TS"),
		"SIG": hdr.Get("SIG"),
	}, body)
	if err := validator.Validate(req, body); err != nil {
		t.Fatalf("自定义 message 配对应通过, got %v", err)
	}

	// body 参与签名后，篡改 body 必死
	tampered := []byte(`{"n":2}`)
	req = mkReq(t, map[string]string{"TS": hdr.Get("TS"), "SIG": hdr.Get("SIG")}, tampered)
	if err := validator.Validate(req, tampered); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("篡改 body 应 ErrBadSignature, got %v", err)
	}
}

// TestTimestampHMACSigner_URLEncodedPairing Java URLEncoder 形态对：发出
// 侧 QueryEscape、收侧 PathUnescape + base64 解码——+/= 经转义后两侧还原
// 一致（header 载体）。
func TestTimestampHMACSigner_URLEncodedPairing(t *testing.T) {
	const secret = "urlenc-secret"
	signer := NewTimestampHMACSigner([]byte(secret), crypto.SHA256, "TS", "SIG",
		SignerWithURLEncoded(),
		TimestampSignerWithClock(func() time.Time { return fixedClock }))
	validator := NewTimestampHMACValidator([]byte(secret), crypto.SHA256, "TS", "SIG",
		WithURLEncoded(), WithClock(func() time.Time { return fixedClock }))

	body := []byte("b")
	hdr, _ := signer.Sign(body)
	rawSig := giteeSign(t, secret, hdr.Get("TS"), body)
	escaped := urlEscapeB64(rawSig)
	if hdr.Get("SIG") != escaped {
		t.Fatalf("URL 编码签名应为 %s, got %s", escaped, hdr.Get("SIG"))
	}

	req := mkReq(t, map[string]string{"TS": hdr.Get("TS"), "SIG": hdr.Get("SIG")}, body)
	if err := validator.Validate(req, body); err != nil {
		t.Fatalf("URL 编码配对应通过, got %v", err)
	}
}

// urlEscapeB64 base64 的 +/= 转 %2B/%2F/%3D（Java URLEncoder 对 base64 的
// 实际效果；测试独立实现，不引被测代码）。
func urlEscapeB64(s string) string {
	repl := func(r rune) string {
		switch r {
		case '+':
			return "%2B"
		case '/':
			return "%2F"
		case '=':
			return "%3D"
		}
		return string(r)
	}
	out := ""
	for _, r := range s {
		out += repl(r)
	}
	return out
}

// TestTimestampHMACSigner_IndependentVector 与测试独立实现逐字节对照：
// base64(HMAC-SHA256(key=secret, msg=ts+"\n"+secret))。
func TestTimestampHMACSigner_IndependentVector(t *testing.T) {
	const secret = "vec-secret"
	ts := itoa(fixedClock.UnixMilli())
	signer := NewTimestampHMACSigner([]byte(secret), crypto.SHA256, "TS", "SIG",
		TimestampSignerWithClock(func() time.Time { return fixedClock }))

	hdr, err := signer.Sign(nil) // 缺省 message 不含 body，payload 无关
	if err != nil {
		t.Fatalf("Sign 不应出错, got %v", err)
	}
	want := giteeSign(t, secret, ts, nil)
	if hdr.Get("SIG") != want {
		t.Fatalf("签名应为独立实现值 %s, got %s", want, hdr.Get("SIG"))
	}
}

func TestTimestampHMACSigner_FailClosed(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"空secret", func() { _ = NewTimestampHMACSigner(nil, crypto.SHA256, "TS", "SIG") }},
		{"零值算法", func() { _ = NewTimestampHMACSigner([]byte("s"), crypto.Hash(0), "TS", "SIG") }},
		{"白名单外算法", func() { _ = NewTimestampHMACSigner([]byte("s"), crypto.MD5, "TS", "SIG") }},
		{"空tsName", func() { _ = NewTimestampHMACSigner([]byte("s"), crypto.SHA256, "", "SIG") }},
		{"空sigName", func() { _ = NewTimestampHMACSigner([]byte("s"), crypto.SHA256, "TS", "") }},
		{"nil选项", func() { _ = NewTimestampHMACSigner([]byte("s"), crypto.SHA256, "TS", "SIG", nil) }},
		{"nilmessage", func() { _ = NewTimestampHMACSigner([]byte("s"), crypto.SHA256, "TS", "SIG", SignerWithMessage(nil)) }},
		{"nil时钟", func() {
			_ = NewTimestampHMACSigner([]byte("s"), crypto.SHA256, "TS", "SIG", TimestampSignerWithClock(nil))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("构造期非法值应 panic（仓规总则①）")
				}
			}()
			tc.fn()
		})
	}
}

// Signer 接口形态守卫：两个签名器家族都满足接口（调用方按接口持有）。
var (
	_ Signer = (*HMACSigner)(nil)
	_ Signer = (*TimestampHMACSigner)(nil)
)
