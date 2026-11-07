package secretx

// passphrase 派生形态测试（#212）：roundtrip、每密文随机 salt、
// 跨形态版本位拒绝、错误语义统一。
//
// 全强度 KDF（600k 迭代）下每条 roundtrip ≈ 两次派生（Seal/Open 各一），
// 用例数量按「每个语义一条」控制。

import (
	"bytes"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

const testPassphrase = "correct horse battery staple"

func newTestPassphraseBox(t *testing.T) *Box {
	t.Helper()
	box, err := NewPassphraseBox(testPassphrase)
	if err != nil {
		t.Fatalf("NewPassphraseBox: %v", err)
	}
	return box
}

func TestNewPassphraseBox_EmptyPassphrase(t *testing.T) {
	if _, err := NewPassphraseBox(""); !errors.Is(err, ErrNoPassphrase) {
		t.Fatalf("空 passphrase 应返回 ErrNoPassphrase, got %v", err)
	}
}

func TestPassphraseBox_SealPrefixV2(t *testing.T) {
	box := newTestPassphraseBox(t)
	sealed, err := box.Seal([]byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if !strings.HasPrefix(sealed, "v2:") {
		t.Fatalf("passphrase 形态应产出 v2 前缀, got %q...", sealed[:8])
	}
}

func TestPassphraseBox_RoundTrip(t *testing.T) {
	box := newTestPassphraseBox(t)
	plaintext := []byte("LDAP bind password — 云账号 SecretKey")
	sealed, err := box.Seal(plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	got, err := box.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("roundtrip 不一致, got %q", got)
	}
}

func TestPassphraseBox_RoundTripWithAAD(t *testing.T) {
	box := newTestPassphraseBox(t)
	sealed, err := box.SealWithAAD([]byte("token"), []byte("credential:row-7"))
	if err != nil {
		t.Fatalf("SealWithAAD: %v", err)
	}
	// AAD 一致解封成功
	if _, err := box.OpenWithAAD(sealed, []byte("credential:row-7")); err != nil {
		t.Fatalf("OpenWithAAD(一致 AAD): %v", err)
	}
	// AAD 不一致（密文挪行）解封必失败
	if _, err := box.OpenWithAAD(sealed, []byte("credential:row-8")); !errors.Is(err, ErrBadCiphertext) {
		t.Fatalf("换 AAD 应 ErrBadCiphertext, got %v", err)
	}
}

// TestPassphraseBox_PerCiphertextSalt 同 passphrase 加密同一明文两次：
// 密文不同（随机 salt ⇒ 派生键不同），且两条都能解封——「每密文随机 salt，
// 而非每 Box 固定 salt」的钉住测试。
func TestPassphraseBox_PerCiphertextSalt(t *testing.T) {
	box := newTestPassphraseBox(t)
	a, err := box.Seal([]byte("same plaintext"))
	if err != nil {
		t.Fatalf("Seal#1: %v", err)
	}
	b, err := box.Seal([]byte("same plaintext"))
	if err != nil {
		t.Fatalf("Seal#2: %v", err)
	}
	if a == b {
		t.Fatal("随机 salt 下两条密文不应相同")
	}
	for i, sealed := range []string{a, b} {
		if got, err := box.Open(sealed); err != nil || !bytes.Equal(got, []byte("same plaintext")) {
			t.Fatalf("第 %d 条密文应可解封, got (%q, %v)", i+1, got, err)
		}
	}
}

func TestPassphraseBox_WrongPassphrase(t *testing.T) {
	box := newTestPassphraseBox(t)
	sealed, err := box.Seal([]byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	wrong, err := NewPassphraseBox("wrong passphrase")
	if err != nil {
		t.Fatalf("NewPassphraseBox: %v", err)
	}
	// 错误语义统一：钥不匹配也是 ErrBadCiphertext（不泄露「是钥不对」）
	if _, err := wrong.Open(sealed); !errors.Is(err, ErrBadCiphertext) {
		t.Fatalf("错误 passphrase 应 ErrBadCiphertext, got %v", err)
	}
}

func TestPassphraseBox_TamperedCiphertext(t *testing.T) {
	box := newTestPassphraseBox(t)
	sealed, err := box.Seal([]byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// 篡改负载尾字节（密文区）
	tampered := flipB64(sealed, len(sealed)-2)
	if _, err := box.Open(tampered); !errors.Is(err, ErrBadCiphertext) {
		t.Fatalf("篡改密文应 ErrBadCiphertext, got %v", err)
	}
	// 篡改 salt 区（前缀后的头几个 base64 字节）——派生钥改变，认证失败
	tamperedSalt := flipB64(sealed, 3)
	if _, err := box.Open(tamperedSalt); !errors.Is(err, ErrBadCiphertext) {
		t.Fatalf("篡改 salt 应 ErrBadCiphertext, got %v", err)
	}
}

// flipB64 把 base64 字符串的一个字符换成另一个（保证串长不变、仍是合法
// base64 字符集）。
func flipB64(s string, i int) string {
	c := byte('A')
	if s[i] == 'A' {
		c = 'B'
	}
	return s[:i] + string(rune(c)) + s[i+1:]
}

// TestPassphraseBox_CrossVersionRejection 跨形态解封按版本位拒绝：
// 原始钥 Box 开 v2、passphrase Box 开 v1，都是 ErrUnsupportedVersion
// （按前缀选 Box——轮换识别位的设计用法）。
func TestPassphraseBox_CrossVersionRejection(t *testing.T) {
	rawBox, err := NewBox(bytes.Repeat([]byte{0x01}, 32))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	v1Sealed, err := rawBox.Seal([]byte("raw"))
	if err != nil {
		t.Fatalf("Seal(v1): %v", err)
	}

	passBox := newTestPassphraseBox(t)
	v2Sealed, err := passBox.Seal([]byte("pass"))
	if err != nil {
		t.Fatalf("Seal(v2): %v", err)
	}

	if _, err := rawBox.Open(v2Sealed); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("原始钥 Box 开 v2 密文应 ErrUnsupportedVersion, got %v", err)
	}
	if _, err := passBox.Open(v1Sealed); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("passphrase Box 开 v1 密文应 ErrUnsupportedVersion, got %v", err)
	}
	// 未识别版本
	if _, err := passBox.Open("v9:AAAA"); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("未识别版本应 ErrUnsupportedVersion, got %v", err)
	}
}

// TestPassphraseBox_ShortPayload 短负载（< salt+nonce+tag）预检拒绝，
// 不进入 KDF。
func TestPassphraseBox_ShortPayload(t *testing.T) {
	box := newTestPassphraseBox(t)
	short := "v2:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x00}, passphraseSaltSize))
	if _, err := box.Open(short); !errors.Is(err, ErrBadCiphertext) {
		t.Fatalf("短负载应 ErrBadCiphertext, got %v", err)
	}
}

// TestPassphraseBox_ZeroBoxSealErrors 未初始化的 Box 在两形态下都拒绝。
func TestPassphraseBox_ZeroBoxSealErrors(t *testing.T) {
	var zero *Box
	if _, err := zero.Seal([]byte("x")); err == nil {
		t.Fatal("nil Box Seal 应报错")
	}
	if _, err := zero.Open("v2:AAAA"); err == nil {
		t.Fatal("nil Box Open 应报错")
	}
	var literal Box // 零值：既无钥也无 passphrase
	if _, err := literal.Seal([]byte("x")); err == nil {
		t.Fatal("零值 Box Seal 应报错")
	}
}

// TestDerivePassphraseAEAD_Deterministic KDF 确定性与 salt 区分度（低迭代
// 快速验证，不经完整 Seal/Open）。
func TestDerivePassphraseAEAD_Deterministic(t *testing.T) {
	const iters = 1000
	salt := []byte("0123456789abcdef")
	a1, err := derivePassphraseAEAD("pw", salt, iters)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := derivePassphraseAEAD("pw", salt, iters)
	if err != nil {
		t.Fatal(err)
	}
	// 相同输入确定性派生：两次 Seal 同 nonce 同明文 ⇒ 同密文
	n := []byte("000000000000")
	c1 := a1.Seal(nil, n, []byte("m"), nil)
	c2 := a2.Seal(nil, n, []byte("m"), nil)
	if !bytes.Equal(c1, c2) {
		t.Fatal("相同 (passphrase, salt, iterations) 应派生同一钥")
	}

	// salt 不同 ⇒ 派生钥不同
	a3, err := derivePassphraseAEAD("pw", []byte("ffffffffffffffff"), iters)
	if err != nil {
		t.Fatal(err)
	}
	c3 := a3.Seal(nil, n, []byte("m"), nil)
	if bytes.Equal(c1, c3) {
		t.Fatal("不同 salt 应派生不同钥")
	}

	// 底层 KDF 语义锚点：pbkdf2 同参数同输出、异参数异输出
	k1, _ := pbkdf2.Key(sha256.New, "pw", salt, iters, 32)
	k2, _ := pbkdf2.Key(sha256.New, "pw", salt, iters, 32)
	if !bytes.Equal(k1, k2) {
		t.Fatal("pbkdf2 应确定性")
	}
	if len(k1) != passphraseKeySize {
		t.Fatalf("派生钥应 32B, got %d", len(k1))
	}
}

// TestPassphraseBox_ConcurrentUse 并发 Seal/Open 安全（派生路径无共享可变
// 状态）；2 并发 × 全强度 KDF。
func TestPassphraseBox_ConcurrentUse(t *testing.T) {
	box := newTestPassphraseBox(t)
	done := make(chan error, 2)
	go func() {
		s, err := box.Seal([]byte("a"))
		if err != nil {
			done <- err
			return
		}
		_, err = box.Open(s)
		done <- err
	}()
	go func() {
		s, err := box.Seal([]byte("b"))
		if err != nil {
			done <- err
			return
		}
		_, err = box.Open(s)
		done <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("并发使用: %v", err)
		}
	}
}
