package webhook

// 时间戳防重放 HMAC 测试（#270）：Gitee 官方口径（ts+"\n"+secret 的
// HMAC-base64）、秒/毫秒自动识别、容忍窗、多模让位语义、query 来源、
// URL 编码签名、自定义 message、构造期 fail-closed。

import (
	"crypto"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// giteeSign 按被测口径独立计算期望签名（Gitee 官方文档算法）。
func giteeSign(t *testing.T, secret, timestamp string, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "\n" + secret))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// fixedClock 假时钟（确定性容忍窗测试）。
var fixedClock = time.Unix(1_800_000_000, 0) // 2027-01-15 前后，足够现代

func TestTimestampHMACValidator_GiteeForm(t *testing.T) {
	const secret = "gitee-secret"
	v := NewTimestampHMACValidator([]byte(secret), crypto.SHA256,
		"X-Gitee-Timestamp", "X-Gitee-Token",
		WithClock(func() time.Time { return fixedClock }))

	body := []byte(`{"zen":"keep it simple"}`)
	ts := fixedClock.UnixMilli() // 毫秒形态
	sig := giteeSign(t, secret, itoa(ts), body)

	req := mkReq(t, map[string]string{
		"X-Gitee-Timestamp": itoa(ts),
		"X-Gitee-Token":     sig,
	}, body)
	if err := v.Validate(req, body); err != nil {
		t.Fatalf("正确签名应通过, got %v", err)
	}

	// 签名不符
	req = mkReq(t, map[string]string{
		"X-Gitee-Timestamp": itoa(ts),
		"X-Gitee-Token":     giteeSign(t, "other-secret", itoa(ts), body),
	}, body)
	if err := v.Validate(req, body); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("错 secret 应 ErrBadSignature, got %v", err)
	}

	// body 被篡改（message 默认不含 body，故签名仍对——自定义 message
	// 形态见 TestTimestampHMACValidator_CustomMessage）
	req = mkReq(t, map[string]string{
		"X-Gitee-Timestamp": itoa(ts),
		"X-Gitee-Token":     sig,
	}, []byte(`{"zen":"tampered"}`))
	if err := v.Validate(req, []byte(`{"zen":"tampered"}`)); err != nil {
		t.Fatalf("message 不含 body 时改 body 不影响签名判定, got %v", err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestTimestampHMACValidator_SecondOrMilliAutoDetect(t *testing.T) {
	const secret = "s"
	v := NewTimestampHMACValidator([]byte(secret), crypto.SHA256,
		"TS", "SIG", WithClock(func() time.Time { return fixedClock }))
	body := []byte("b")

	for _, ts := range []string{
		itoa(fixedClock.Unix()),      // 秒
		itoa(fixedClock.UnixMilli()), // 毫秒
	} {
		sig := giteeSign(t, secret, ts, body)
		req := mkReq(t, map[string]string{"TS": ts, "SIG": sig}, body)
		if err := v.Validate(req, body); err != nil {
			t.Fatalf("秒/毫秒形态 %s 应自动识别并通过, got %v", ts, err)
		}
	}
}

func TestTimestampHMACValidator_DriftWindow(t *testing.T) {
	const secret = "s"
	now := fixedClock
	v := NewTimestampHMACValidator([]byte(secret), crypto.SHA256, "TS", "SIG",
		WithClock(func() time.Time { return now }),
		WithMaxDrift(5*time.Minute))
	body := []byte("b")

	// 窗内（恰 5 分钟）：通过
	ts := itoa(now.Add(-5 * time.Minute).Unix())
	sig := giteeSign(t, secret, ts, body)
	req := mkReq(t, map[string]string{"TS": ts, "SIG": sig}, body)
	if err := v.Validate(req, body); err != nil {
		t.Fatalf("恰窗内应通过, got %v", err)
	}

	// 超窗（5 分钟 + 1 秒，过去侧）
	tsPast := itoa(now.Add(-5*time.Minute - time.Second).Unix())
	req = mkReq(t, map[string]string{"TS": tsPast, "SIG": giteeSign(t, secret, tsPast, body)}, body)
	err := v.Validate(req, body)
	if !errors.Is(err, ErrStaleTimestamp) {
		t.Fatalf("超窗应 ErrStaleTimestamp, got %v", err)
	}

	// 未来侧超窗同样拒绝（防钟差滥用）
	tsFuture := itoa(now.Add(6 * time.Minute).Unix())
	req = mkReq(t, map[string]string{"TS": tsFuture, "SIG": giteeSign(t, secret, tsFuture, body)}, body)
	if err := v.Validate(req, body); !errors.Is(err, ErrStaleTimestamp) {
		t.Fatalf("未来侧超窗应 ErrStaleTimestamp, got %v", err)
	}

	// 时间戳非法
	req = mkReq(t, map[string]string{"TS": "not-a-number", "SIG": "AAAA"}, body)
	if err := v.Validate(req, body); !errors.Is(err, ErrStaleTimestamp) {
		t.Fatalf("非法时间戳应 ErrStaleTimestamp, got %v", err)
	}
}

// TestTimestampHMACValidator_MultiModeFallthrough 多模消歧：签名缺席或
// 时间戳缺席都**让位**（密码模式流量没有时间戳），双在场才判死。
func TestTimestampHMACValidator_MultiModeFallthrough(t *testing.T) {
	const secret = "s"
	v := NewTimestampHMACValidator([]byte(secret), crypto.SHA256, "TS", "SIG",
		WithClock(func() time.Time { return fixedClock }))
	body := []byte("b")

	// 只有签名没有时间戳：让位（密码模式候选接管）
	req := mkReq(t, map[string]string{"SIG": "AAAA"}, body)
	if err := v.Validate(req, body); !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("无时间戳应让位 ErrMissingSignature, got %v", err)
	}
	// 只有时间戳没有签名：让位
	req = mkReq(t, map[string]string{"TS": itoa(fixedClock.Unix())}, body)
	if err := v.Validate(req, body); !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("无签名应让位 ErrMissingSignature, got %v", err)
	}
	// 两者都在场：时间戳超窗判死（不让位——在场即裁决）
	ts := itoa(fixedClock.Add(-2 * time.Hour).Unix())
	req = mkReq(t, map[string]string{"TS": ts, "SIG": giteeSign(t, secret, ts, body)}, body)
	if err := v.Validate(req, body); !errors.Is(err, ErrStaleTimestamp) {
		t.Fatalf("双在场超窗应判死 ErrStaleTimestamp, got %v", err)
	}
}

func TestTimestampHMACValidator_QuerySourceAndURLEncode(t *testing.T) {
	const secret = "s"
	v := NewTimestampHMACValidator([]byte(secret), crypto.SHA256, "timestamp", "sign",
		WithClock(func() time.Time { return fixedClock }),
		WithQuerySource(), WithURLEncoded())
	body := []byte("b")
	ts := itoa(fixedClock.UnixMilli())

	// Java URLEncoder.encode 形态：+/= 被 %2B/%2F/%3D 转义
	raw := giteeSign(t, secret, ts, body)
	escaped := strings.NewReplacer("+", "%2B", "/", "%2F", "=", "%3D").Replace(raw)

	u := "/webhook?timestamp=" + ts + "&sign=" + escaped
	req := httptest.NewRequest(http.MethodPost, u, nil)
	if err := v.Validate(req, body); err != nil {
		t.Fatalf("query 来源 + URL 编码签名应通过, got %v", err)
	}

	// 未编码形态（裸 base64）同样应通过
	req = httptest.NewRequest(http.MethodPost, "/webhook?timestamp="+ts+"&sign="+raw, nil)
	if err := v.Validate(req, body); err != nil {
		t.Fatalf("裸 base64 签名应通过, got %v", err)
	}
}

func TestTimestampHMACValidator_CustomMessage(t *testing.T) {
	const secret = "s"
	// body 参与签名的平台形态：message = timestamp + body
	v := NewTimestampHMACValidator([]byte(secret), crypto.SHA256, "TS", "SIG",
		WithClock(func() time.Time { return fixedClock }),
		WithMessage(func(ts string, body []byte) []byte {
			return []byte(ts + string(body))
		}))
	body := []byte(`{"n":1}`)
	ts := itoa(fixedClock.Unix())
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + string(body)))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	req := mkReq(t, map[string]string{"TS": ts, "SIG": sig}, body)
	if err := v.Validate(req, body); err != nil {
		t.Fatalf("自定义 message 应通过, got %v", err)
	}

	// body 参与签名后，篡改 body 必死
	mac2 := hmac.New(sha256.New, []byte(secret))
	mac2.Write([]byte(ts + string(body)))
	sig2 := base64.StdEncoding.EncodeToString(mac2.Sum(nil))
	tampered := []byte(`{"n":2}`)
	req = mkReq(t, map[string]string{"TS": ts, "SIG": sig2}, tampered)
	if err := v.Validate(req, tampered); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("篡改 body 应 ErrBadSignature, got %v", err)
	}
}

func TestTimestampHMACValidator_FailClosed(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"空secret", func() {
			_ = NewTimestampHMACValidator(nil, crypto.SHA256, "TS", "SIG")
		}},
		{"零值算法", func() {
			_ = NewTimestampHMACValidator([]byte("s"), crypto.Hash(0), "TS", "SIG")
		}},
		{"空tsName", func() {
			_ = NewTimestampHMACValidator([]byte("s"), crypto.SHA256, "", "SIG")
		}},
		{"空sigName", func() {
			_ = NewTimestampHMACValidator([]byte("s"), crypto.SHA256, "TS", "")
		}},
		{"零容忍窗", func() {
			_ = NewTimestampHMACValidator([]byte("s"), crypto.SHA256, "TS", "SIG", WithMaxDrift(0))
		}},
		{"负容忍窗", func() {
			_ = NewTimestampHMACValidator([]byte("s"), crypto.SHA256, "TS", "SIG", WithMaxDrift(-time.Minute))
		}},
		{"nil时钟", func() {
			_ = NewTimestampHMACValidator([]byte("s"), crypto.SHA256, "TS", "SIG", WithClock(nil))
		}},
		{"nilmessage", func() {
			_ = NewTimestampHMACValidator([]byte("s"), crypto.SHA256, "TS", "SIG", WithMessage(nil))
		}},
		{"nil选项", func() {
			_ = NewTimestampHMACValidator([]byte("s"), crypto.SHA256, "TS", "SIG", nil)
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
