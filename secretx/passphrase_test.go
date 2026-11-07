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
	// 篡改负载尾字节（密文区）——**字节级翻转**。
	//
	// ⚠️ 不能用字符级翻转：base64 末组（剩 2 字节 ⇒ 3 字符 + '='）的第 3 个
	// 字符只承载 2 个**有效位**，其余 4 位是解码时丢弃的 padding 位 ⇒ 翻它
	// 可能只改到 padding 位 ⇒ **解码后字节完全不变** ⇒ 密文根本没被篡改 ⇒
	// GCM 认证当然通过 ⇒ Open 返回 nil ⇒ 本用例按 1/16 概率假红
	// （实测 2026-11-07 CI 红即此因，见 pkg#275）。
	tampered := tamperBase64Byte(t, sealed, -1)
	if _, err := box.Open(tampered); !errors.Is(err, ErrBadCiphertext) {
		t.Fatalf("篡改密文应 ErrBadCiphertext, got %v", err)
	}
	// 篡改 salt 区（前缀后的头几个 base64 字节）——派生钥改变，认证失败
	tamperedSalt := flipB64(sealed, 3)
	assertDecodedChanged(t, sealed, tamperedSalt)
	if _, err := box.Open(tamperedSalt); !errors.Is(err, ErrBadCiphertext) {
		t.Fatalf("篡改 salt 应 ErrBadCiphertext, got %v", err)
	}
}

// flipB64 把 base64 字符串的一个字符换成另一个（保证串长不变、仍是合法
// base64 字符集）。
//
// ⚠️ **只对「6 位全有效」的位置安全**：base64 末组的第 3 字符只有 2 个有效位
// （其余是丢弃的 padding 位）⇒ 翻它可能不改变解码后的字节 ⇒ 篡改是空操作。
// 载荷区改动请用 tamperBase64Byte（字节级）。
func flipB64(s string, i int) string {
	c := byte('A')
	if s[i] == 'A' {
		c = 'B'
	}
	return s[:i] + string(rune(c)) + s[i+1:]
}

// tamperBase64Byte 解码 base64 负载 → 翻指定字节的最低位 → 重新编码。
// byteIdx < 0 表示「从尾部数」（-1 = 最后一个字节）。
//
// 字节级翻转保证「篡改一定是真的」——字符级翻转会命中 base64 的丢弃位
// （见 flipB64 的说明），那样「篡改」实为空操作，用例变成随机的假红。
func tamperBase64Byte(t *testing.T, sealed string, byteIdx int) string {
	t.Helper()
	prefix, payload, ok := strings.Cut(sealed, ":")
	if !ok {
		t.Fatalf("密文 %q 缺版本前缀", sealed)
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if byteIdx < 0 {
		byteIdx = len(raw) + byteIdx
	}
	if byteIdx < 0 || byteIdx >= len(raw) {
		t.Fatalf("byteIdx %d 越界（负载 %d 字节）", byteIdx, len(raw))
	}
	raw[byteIdx] ^= 0x01
	return prefix + ":" + base64.StdEncoding.EncodeToString(raw)
}

// assertDecodedChanged 断言两个密文的 base64 负载**解码后确实不同**
// ——「篡改」必须是真的，否则「未被检出」证明不了任何事。
func assertDecodedChanged(t *testing.T, a, b string) {
	t.Helper()
	_, pa, _ := strings.Cut(a, ":")
	_, pb, _ := strings.Cut(b, ":")
	ra, err := base64.StdEncoding.DecodeString(pa)
	if err != nil {
		t.Fatalf("decode a: %v", err)
	}
	rb, err := base64.StdEncoding.DecodeString(pb)
	if err != nil {
		t.Fatalf("decode b: %v", err)
	}
	if bytes.Equal(ra, rb) {
		t.Fatalf("篡改后负载逐字节相同 ⇒ 这次「篡改」是空操作，断言无意义")
	}
}

// TestFlipB64_ChangesPayload 钉住 helper 的性质：**6 位全有效**的字符位置上，
// 字符级翻转必然改变解码后的负载；而 base64 末组的第 3 字符只有 2 个有效位
// （其余 4 位是解码时丢弃的 padding 位）⇒ 翻它可能**不改变**负载 ⇒ 篡改是
// 空操作（pkg#275 的根因）。本用例把「哪些位置安全」写成可证伪的门禁。
func TestFlipB64_ChangesPayload(t *testing.T) {
	raw := make([]byte, 50) // 与 v2 载荷同长：salt16+nonce12+密文6+tag16 ⇒ 末组剩 2 字节
	for i := range raw {
		raw[i] = byte(i * 7)
	}
	payload := base64.StdEncoding.EncodeToString(raw)
	if len(payload) < 4 || payload[len(payload)-1] != '=' {
		t.Fatalf("夹具形态不对：末字符 %q（期望 '='）", payload[len(payload)-1])
	}
	// 末组第 3 字符（= len-2）是危险位：它的低 4 位是丢弃位 ⇒ 翻转可能无效。
	danger := len(payload) - 2
	for i := 0; i < len(payload)-4; i++ {
		if i == danger {
			continue
		}
		flipped := flipB64(payload, i)
		a, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			t.Fatal(err)
		}
		b, err := base64.StdEncoding.DecodeString(flipped)
		if err != nil {
			t.Fatalf("位置 %d 翻转后不是合法 base64: %v", i, err)
		}
		if bytes.Equal(a, b) {
			t.Fatalf("位置 %d 翻转后负载逐字节相同 ⇒ 该位置不该用字符级翻转", i)
		}
	}
	// 危险位：若恰好翻到丢弃位，解码结果不变 —— 这正是 pkg#275 的成因，
	// 故断言「存在这种可能」，把「字符级翻转不可用于载荷尾」写成门禁。
	sawNoop := false
	for v := 0; v < 16 && !sawNoop; v++ {
		raw2 := append([]byte(nil), raw...)
		raw2[len(raw2)-1] = byte(v << 2)
		p2 := base64.StdEncoding.EncodeToString(raw2)
		f2 := flipB64(p2, danger)
		a2, _ := base64.StdEncoding.DecodeString(p2)
		b2, _ := base64.StdEncoding.DecodeString(f2)
		if bytes.Equal(a2, b2) {
			sawNoop = true
		}
	}
	if !sawNoop {
		t.Fatalf("危险位上竟没出现「翻转为空操作」的情形——base64 末组语义与本用例假设不符，需重新核对")
	}
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
