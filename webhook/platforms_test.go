package webhook

// 平台约定构造器测试（#270）：issue 列举的五形态逐个钉住——GitHub
// sha256=<hex> 双形态、Forgejo/Gitea 双 header 回退、GitCode 三模回退、
// Gitee 时间戳+密码双模消歧（签名优先、防同 secret 交叉认证）。

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

var platClock = func() time.Time { return fixedClock }

func hexSign(t *testing.T, secret string, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestGitHubValidator(t *testing.T) {
	body := []byte(`{"action":"push"}`)
	v := NewGitHubValidator([]byte(testSecret))

	// sha256=<hex> 官方形态
	req := mkReq(t, map[string]string{"X-Hub-Signature-256": "sha256=" + hexSign(t, testSecret, body)}, body)
	if err := v.Validate(req, body); err != nil {
		t.Fatalf("GitHub sha256= 形态应通过, got %v", err)
	}
	// 裸 hex 双形态
	req = mkReq(t, map[string]string{"X-Hub-Signature-256": hexSign(t, testSecret, body)}, body)
	if err := v.Validate(req, body); err != nil {
		t.Fatalf("GitHub 裸 hex 形态应通过, got %v", err)
	}
	// 错签
	req = mkReq(t, map[string]string{"X-Hub-Signature-256": "sha256=" + hexSign(t, "wrong", body)}, body)
	if err := v.Validate(req, body); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("错签应 ErrBadSignature, got %v", err)
	}
	// 旧 X-Hub-Signature（sha1）不得顶替 256 验签器
	req = mkReq(t, map[string]string{"X-Hub-Signature": "sha1=abc"}, body)
	if err := v.Validate(req, body); !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("仅旧 header 应 ErrMissingSignature, got %v", err)
	}
}

func TestForgejoValidator_HeaderFallback(t *testing.T) {
	body := []byte(`{"action":"sync"}`)
	v := NewForgejoValidator([]byte(testSecret))

	// 新 header
	req := mkReq(t, map[string]string{"X-Forgejo-Signature": hexSign(t, testSecret, body)}, body)
	if err := v.Validate(req, body); err != nil {
		t.Fatalf("Forgejo 新 header 应通过, got %v", err)
	}
	// 老分叉 header 回退（Forgejo 老版本沿用 Gitea 的 header）
	req = mkReq(t, map[string]string{"X-Gitea-Signature": hexSign(t, testSecret, body)}, body)
	if err := v.Validate(req, body); err != nil {
		t.Fatalf("老分叉 header 回退应通过, got %v", err)
	}
	// 双 header 在场：主 header 优先（错回退值不裁决）
	req = mkReq(t, map[string]string{
		"X-Forgejo-Signature": hexSign(t, testSecret, body),
		"X-Gitea-Signature":   "deadbeef",
	}, body)
	if err := v.Validate(req, body); err != nil {
		t.Fatalf("主 header 在场应优先裁决, got %v", err)
	}
}

func TestGiteaValidator(t *testing.T) {
	body := []byte(`{"ref":"main"}`)
	v := NewGiteaValidator([]byte(testSecret))
	req := mkReq(t, map[string]string{"X-Gitea-Signature": hexSign(t, testSecret, body)}, body)
	if err := v.Validate(req, body); err != nil {
		t.Fatalf("Gitea 应通过, got %v", err)
	}
}

func TestGitCodeValidator_ThreeModes(t *testing.T) {
	body := []byte(`{"event":"push"}`)
	sig := hexSign(t, testSecret, body)
	const staticTok = "gitcode-static-token"

	// 无令牌构造：纯签名模式
	sigOnly := NewGitCodeValidator([]byte(testSecret), nil)
	req := mkReq(t, map[string]string{"X-GitCode-Signature": sig}, body)
	if err := sigOnly.Validate(req, body); err != nil {
		t.Fatalf("GitCode 签名模式应通过, got %v", err)
	}
	// 静态令牌在但未配置令牌：不落到令牌模式（fail-closed 默认）
	req = mkReq(t, map[string]string{"X-GitCode-Token": staticTok}, body)
	if err := sigOnly.Validate(req, body); !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("未配置令牌时不应落入令牌模式, got %v", err)
	}

	// 带令牌构造：三模回退 GitCode-Signature → Gitea-Signature → GitCode-Token
	withTok := NewGitCodeValidator([]byte(testSecret), []byte(staticTok))
	if err := withTok.Validate(mkReq(t, map[string]string{"X-GitCode-Signature": sig}, body), body); err != nil {
		t.Fatalf("签名模式应通过, got %v", err)
	}
	if err := withTok.Validate(mkReq(t, map[string]string{"X-Gitea-Signature": sig}, body), body); err != nil {
		t.Fatalf("回退 gitea 形 header 应通过, got %v", err)
	}
	if err := withTok.Validate(mkReq(t, map[string]string{"X-GitCode-Token": staticTok}, body), body); err != nil {
		t.Fatalf("静态令牌模式应通过, got %v", err)
	}
	// 错令牌：判死
	err := withTok.Validate(mkReq(t, map[string]string{"X-GitCode-Token": "bad"}, body), body)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("错令牌应 ErrBadSignature, got %v", err)
	}
}

func TestGiteeValidator_DualMode(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	const secret = "gitee-webhook-secret"
	const password = "gitee-webhook-password"
	v := NewGiteeValidator([]byte(secret), []byte(password),
		WithClock(platClock))

	// 签名模式（timestamp + HMAC-base64）
	ts := itoa(platClock().UnixMilli())
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "\n" + secret))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	req := mkReq(t, map[string]string{"X-Gitee-Timestamp": ts, "X-Gitee-Token": sig}, body)
	if err := v.Validate(req, body); err != nil {
		t.Fatalf("Gitee 签名模式应通过, got %v", err)
	}

	// 密码模式（无时间戳、明文密码令牌）——显式 opt-in 后可用
	req = mkReq(t, map[string]string{"X-Gitee-Token": password}, body)
	if err := v.Validate(req, body); err != nil {
		t.Fatalf("Gitee 密码模式应通过, got %v", err)
	}

	// 密码对、签名模式 header 伪造（timestamp 在场但签名=密码）——签名优先
	// 裁决判死：防同 secret 交叉认证
	req = mkReq(t, map[string]string{"X-Gitee-Timestamp": ts, "X-Gitee-Token": password}, body)
	if err := v.Validate(req, body); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("签名 header 在场必须按签名裁决, got %v", err)
	}

	// 超窗签名判死
	oldTs := itoa(platClock().Add(-2 * time.Hour).UnixMilli())
	mac2 := hmac.New(sha256.New, []byte(secret))
	mac2.Write([]byte(oldTs + "\n" + secret))
	oldSig := base64.StdEncoding.EncodeToString(mac2.Sum(nil))
	req = mkReq(t, map[string]string{"X-Gitee-Timestamp": oldTs, "X-Gitee-Token": oldSig}, body)
	if err := v.Validate(req, body); !errors.Is(err, ErrStaleTimestamp) {
		t.Fatalf("超窗应 ErrStaleTimestamp, got %v", err)
	}
}

func TestGiteeValidator_PasswordModeOff(t *testing.T) {
	body := []byte("b")
	const secret = "gitee-secret"
	// 密码为空：纯签名模式（AllowPasswordMode 默认拒绝）
	v := NewGiteeValidator([]byte(secret), nil, WithClock(platClock))
	req := mkReq(t, map[string]string{"X-Gitee-Token": "任何令牌"}, body)
	if err := v.Validate(req, body); !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("未 opt-in 密码模式时不应接受静态令牌, got %v", err)
	}
}

func TestPlatformConstructors_FailClosed(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"GitHub空secret", func() { _ = NewGitHubValidator(nil) }},
		{"Forgejo空secret", func() { _ = NewForgejoValidator([]byte{}) }},
		{"Gitea空secret", func() { _ = NewGiteaValidator(nil) }},
		{"GitCode空secret", func() { _ = NewGitCodeValidator(nil, []byte("t")) }},
		{"Gitee空secret", func() { _ = NewGiteeValidator(nil, []byte("p")) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("空密钥应 fail-closed panic（仓规总则①）")
				}
			}()
			tc.fn()
		})
	}
}
