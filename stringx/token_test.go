package stringx

import (
	"encoding/base64"
	"encoding/hex"
	stderrors "errors"
	"strings"
	"testing"
)

// #528：RandomToken / RandomTokenOrFallback 的行为契约。

func TestRandomToken_LengthAndDecodes(t *testing.T) {
	cases := []struct {
		name    string
		enc     TokenEncoding
		nBytes  int
		wantLen int // 编码后字符数（std 含 padding）
	}{
		{"hex-1B", TokenHex, 1, 2},
		{"hex-16B", TokenHex, 16, 32},
		{"hex-32B", TokenHex, 32, 64},
		{"std-1B", TokenBase64Std, 1, 4},
		{"std-16B", TokenBase64Std, 16, 24},
		{"std-32B", TokenBase64Std, 32, 44},
		{"rawurl-1B", TokenBase64RawURL, 1, 2},
		{"rawurl-16B", TokenBase64RawURL, 16, 22},
		{"rawurl-32B", TokenBase64RawURL, 32, 43},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok, err := RandomToken(tc.nBytes, tc.enc)
			if err != nil {
				t.Fatalf("RandomToken(%d, %d) 意外出错: %v", tc.nBytes, tc.enc, err)
			}
			if len(tok) != tc.wantLen {
				t.Fatalf("令牌长度 = %d, want %d (tok=%q)", len(tok), tc.wantLen, tok)
			}
			// 编码可逆性：解码必须还原 nBytes 字节（顺带校验字符集合法性）。
			var got []byte
			switch tc.enc {
			case TokenHex:
				got, err = hex.DecodeString(tok)
			case TokenBase64Std:
				if strings.ContainsAny(tok, "-_") {
					t.Fatalf("std 编码不应出现 URL 安全替代字符: %q", tok)
				}
				got, err = base64.StdEncoding.DecodeString(tok)
			case TokenBase64RawURL:
				if strings.ContainsAny(tok, "+/=") {
					t.Fatalf("raw URL 编码不应含 +/= : %q", tok)
				}
				got, err = base64.RawURLEncoding.DecodeString(tok)
			}
			if err != nil {
				t.Fatalf("令牌应可逆解码, tok=%q: %v", tok, err)
			}
			if len(got) != tc.nBytes {
				t.Fatalf("解码后字节数 = %d, want %d", len(got), tc.nBytes)
			}
		})
	}
}

func TestRandomToken_Unique(t *testing.T) {
	// 16 字节（128 bit）随机性下 200 连发撞车概率工程上为零；这是护栏而
	// 非统计证明——若随机源被换成时间戳/固定值，这里立刻红。
	seen := make(map[string]bool, 200)
	for i := 0; i < 200; i++ {
		tok, err := RandomToken(16, TokenHex)
		if err != nil {
			t.Fatalf("第 %d 次生成意外出错: %v", i, err)
		}
		if seen[tok] {
			t.Fatalf("令牌撞车: %q", tok)
		}
		seen[tok] = true
	}
}

func TestRandomToken_InvalidParams(t *testing.T) {
	cases := []struct {
		name    string
		nBytes  int
		enc     TokenEncoding
		wantErr string
	}{
		{"零字节", 0, TokenHex, "nBytes 必须为正，实得 0"},
		{"负字节", -1, TokenBase64Std, "nBytes 必须为正，实得 -1"},
		{"非法编码", 16, TokenEncoding(42), "非法 TokenEncoding 42"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RandomToken(tc.nBytes, tc.enc)
			if err == nil {
				t.Fatal("参数非法应返回 error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("错误文案应含 %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestRandomToken_RandFailure_Propagated(t *testing.T) {
	// 随机源失败显式上抛、不静默退化（仓规总则③）。
	injected := stderrors.New("injected rand failure")
	old := randRead
	randRead = func(b []byte) (int, error) { return 0, injected }
	defer func() { randRead = old }()

	_, err := RandomToken(16, TokenHex)
	if err == nil {
		t.Fatal("随机源失败应上抛 error，不得静默退化")
	}
	// 错误链可判到注入的随机源错误（errors.Wrapf 以 %w 保身份）。
	if !stderrors.Is(err, injected) {
		t.Fatalf("errors.Is 应沿错误链命中随机源错误, got %v", err)
	}
}

func TestRandomTokenOrFallback_SuccessAndDegradation(t *testing.T) {
	t.Run("成功路径返回随机令牌", func(t *testing.T) {
		tok, err := RandomTokenOrFallback(16, TokenBase64RawURL, func() string {
			t.Fatal("成功路径不应调用 fallback")
			return ""
		})
		if err != nil {
			t.Fatalf("意外出错: %v", err)
		}
		if len(tok) != 22 {
			t.Fatalf("令牌长度 = %d, want 22", len(tok))
		}
	})

	t.Run("随机源失败退 fallback 且 err 为 nil", func(t *testing.T) {
		injected := stderrors.New("injected rand failure")
		old := randRead
		randRead = func(b []byte) (int, error) { return 0, injected }
		defer func() { randRead = old }()

		tok, err := RandomTokenOrFallback(16, TokenHex, func() string { return "fallback-token" })
		if err != nil {
			t.Fatalf("降级后 err 应为 nil（否则 if err != nil 风格的调用方会把降级值当失败丢掉）, got %v", err)
		}
		if tok != "fallback-token" {
			t.Fatalf("应返回 fallback 结果, got %q", tok)
		}
	})

	t.Run("参数错误不降级且不调 fallback", func(t *testing.T) {
		called := false
		_, err := RandomTokenOrFallback(0, TokenHex, func() string {
			called = true
			return "should-not-happen"
		})
		if err == nil {
			t.Fatal("确定性参数错误应原样上抛，不得降级")
		}
		if called {
			t.Fatal("参数错误不应触发 fallback")
		}
	})

	t.Run("nil fallback 报错", func(t *testing.T) {
		_, err := RandomTokenOrFallback(16, TokenHex, nil)
		if err == nil || !strings.Contains(err.Error(), "fallback 不能为 nil") {
			t.Fatalf("nil fallback 应报错, got %v", err)
		}
	})
}

func TestRandomToken_EncodingsIndependent(t *testing.T) {
	// 三种编码各发两枚：可正常生成且互不重复（编码器各自独立、不串号）。
	for _, enc := range []TokenEncoding{TokenHex, TokenBase64Std, TokenBase64RawURL} {
		a, err := RandomToken(16, enc)
		if err != nil {
			t.Fatalf("enc=%d 意外出错: %v", enc, err)
		}
		b, err := RandomToken(16, enc)
		if err != nil {
			t.Fatalf("enc=%d 意外出错: %v", enc, err)
		}
		if a == b {
			t.Fatalf("enc=%d 连续两枚相同: %q", enc, a)
		}
	}
}
