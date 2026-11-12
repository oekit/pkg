package secretx

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
)

// mustKey 生成合法长度的随机主密钥。
func mustKey(t *testing.T, n int) []byte {
	t.Helper()
	k := make([]byte, n)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

// TestRoundTrip 主链路：Seal → Open 还原；密文形态 = "v1:" + base64(nonce‖ct)，
// 与 domain-manage 手搓形态逐字节兼容（存量数据零迁移的前提）。
func TestRoundTrip(t *testing.T) {
	box, err := NewBox(mustKey(t, 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, pt := range []string{"", "short", "sk-abc123XYZ/+=密文也行", strings.Repeat("x", 4096)} {
		sealed, err := box.Seal([]byte(pt))
		if err != nil {
			t.Fatalf("Seal(%q): %v", pt, err)
		}
		if !strings.HasPrefix(sealed, "v1:") {
			t.Fatalf("密文应带 v1 前缀，got %q", sealed[:min(10, len(sealed))])
		}
		payload := strings.TrimPrefix(sealed, "v1:")
		raw, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			t.Fatalf("负载应是合法 base64: %v", err)
		}
		// 12B nonce + 16B GCM tag + 明文
		if len(raw) != 12+16+len(pt) {
			t.Fatalf("密文长度=%d，want %d", len(raw), 12+16+len(pt))
		}
		got, err := box.Open(sealed)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if string(got) != pt {
			t.Fatalf("round trip 不还原：got %q want %q", got, pt)
		}
	}
}

// TestSealRandomizedNonce 同一明文两次 Seal 产出不同密文（随机 nonce）。
func TestSealRandomizedNonce(t *testing.T) {
	box, _ := NewBox(mustKey(t, 32))
	s1, _ := box.Seal([]byte("same"))
	s2, _ := box.Seal([]byte("same"))
	if s1 == s2 {
		t.Fatal("随机 nonce 下两次 Seal 不应产出相同密文")
	}
}

// TestSealAllKeySizes 16/24/32 字节主密钥全档可用。
func TestSealAllKeySizes(t *testing.T) {
	for _, n := range []int{16, 24, 32} {
		box, err := NewBox(mustKey(t, n))
		if err != nil {
			t.Fatalf("%d 字节密钥应合法: %v", n, err)
		}
		sealed, err := box.Seal([]byte("x"))
		if err != nil {
			t.Fatalf("%d 字节: %v", n, err)
		}
		if _, err := box.Open(sealed); err != nil {
			t.Fatalf("%d 字节 round trip: %v", n, err)
		}
	}
}

// TestConstructFailFast 构造期 fail-fast：缺钥 ErrNoKey、长度非法 ErrBadKeySize
// ——加密原语没有「带病运行」的合法形态，更没有缺钥明文透传。
func TestConstructFailFast(t *testing.T) {
	if _, err := NewBox(nil); !errors.Is(err, ErrNoKey) {
		t.Fatalf("缺钥应 ErrNoKey，got %v", err)
	}
	if _, err := NewBox([]byte{}); !errors.Is(err, ErrNoKey) {
		t.Fatalf("空钥应 ErrNoKey，got %v", err)
	}
	for _, n := range []int{1, 15, 17, 28, 31, 33, 64} {
		if _, err := NewBox(mustKey(t, n)); !errors.Is(err, ErrBadKeySize) {
			t.Fatalf("%d 字节应 ErrBadKeySize，got %v", n, err)
		}
	}
	if _, err := ParseKey(""); !errors.Is(err, ErrNoKey) {
		t.Fatalf("ParseKey 空串应 ErrNoKey，got %v", err)
	}
	if _, err := ParseKey("not-base64!!"); err == nil {
		t.Fatal("ParseKey 非法 base64 应报错")
	}
	// 错误带实际长度上下文
	_, err := ParseKey(base64.StdEncoding.EncodeToString(mustKey(t, 20)))
	if err == nil || !strings.Contains(err.Error(), "20") {
		t.Fatalf("ParseKey 错误应带实际长度，got %v", err)
	}
	if !errors.Is(err, ErrBadKeySize) {
		t.Fatalf("应包 ErrBadKeySize，got %v", err)
	}
}

// TestBadCiphertextUnified 解密失败统一 ErrBadCiphertext：密文损坏与错钥两种
// 情形不可区分（不向日志泄露「哪条密文是用哪把钥封的」）。
func TestBadCiphertextUnified(t *testing.T) {
	box, _ := NewBox(mustKey(t, 32))
	otherBox, _ := NewBox(mustKey(t, 32))
	sealed, _ := box.Seal([]byte("secret"))

	cases := map[string]string{
		"非 base64 负载": "v1:!!!not-base64!!!",
		"负载过短":        "v1:" + base64.StdEncoding.EncodeToString([]byte("short")),
		"篡改密文":        tamper(t, sealed),
		"错钥解封":        mustSeal(t, otherBox, "secret"),
	}
	for name, ct := range cases {
		_, err := box.Open(ct)
		if !errors.Is(err, ErrBadCiphertext) {
			t.Fatalf("%s：应统一 ErrBadCiphertext，got %v", name, err)
		}
	}
}

// TestUnsupportedVersion 版本识别位：无前缀 / 未知前缀 → ErrUnsupportedVersion，
// 主密钥轮换时下游据此选钥重封。
func TestUnsupportedVersion(t *testing.T) {
	box, _ := NewBox(mustKey(t, 32))
	for _, ct := range []string{"", "rawlegacybase64", "v9:" + base64.StdEncoding.EncodeToString(make([]byte, 32))} {
		_, err := box.Open(ct)
		if !errors.Is(err, ErrUnsupportedVersion) {
			t.Fatalf("ct=%q 应 ErrUnsupportedVersion，got %v", ct, err)
		}
	}
}

// TestAADBinding AAD 绑定：缺 AAD / 错 AAD 解封必失败，AAD 不入密文。
func TestAADBinding(t *testing.T) {
	box, _ := NewBox(mustKey(t, 32))
	aad := []byte("credential:row-42")
	sealed, err := box.SealWithAAD([]byte("secret"), aad)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := box.OpenWithAAD(sealed, aad); err != nil || string(got) != "secret" {
		t.Fatalf("同 AAD 解封应成功：got %q err %v", got, err)
	}
	if _, err := box.OpenWithAAD(sealed, []byte("credential:row-43")); !errors.Is(err, ErrBadCiphertext) {
		t.Fatalf("挪用行 AAD 应失败，got %v", err)
	}
	if _, err := box.Open(sealed); !errors.Is(err, ErrBadCiphertext) {
		t.Fatalf("缺 AAD 解封应失败，got %v", err)
	}
}

// TestConcurrent Box 可并发使用（aead 无状态）。
func TestConcurrent(t *testing.T) {
	box, _ := NewBox(mustKey(t, 32))
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			sealed, err := box.Seal([]byte("concurrent"))
			if err != nil {
				t.Error(err)
				return
			}
			got, err := box.Open(sealed)
			if err != nil || !bytes.Equal(got, []byte("concurrent")) {
				t.Errorf("concurrent round trip: %v", err)
			}
		})
	}
	wg.Wait()
}

// TestZeroBoxMisuse 零值/nil Box 显式报错，不静默产出。
func TestZeroBoxMisuse(t *testing.T) {
	var b *Box
	if _, err := b.Seal([]byte("x")); err == nil {
		t.Fatal("nil Box 应报错")
	}
	zero := &Box{}
	if _, err := zero.Seal([]byte("x")); err == nil {
		t.Fatal("零值 Box 应报错")
	}
}

// ---- helpers ----

func mustSeal(t *testing.T, b *Box, pt string) string {
	t.Helper()
	s, err := b.Seal([]byte(pt))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// tamper 翻转密文负载中的一个比特（保持 base64 合法）。
func tamper(t *testing.T, sealed string) string {
	t.Helper()
	prefix, payload, _ := strings.Cut(sealed, ":")
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 0x01
	return prefix + ":" + base64.StdEncoding.EncodeToString(raw)
}
