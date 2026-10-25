package encoding

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// Base64 无填充（Raw）变体（pkg#96）：JWT / OIDC / PKCE 协议段用的就是这一套。
//
// 关键契约：**带填充与无填充是两套不同的编解码器，不能互换**——
//
//	Base64URLDecode("aGVsbG8")   → 报错（illegal base64 data at input byte 6）
//	Base64RawURLDecode("aGVsbG8") → "hello" ✅
//
// 这正是收编进库时最容易踩的坑：方法名看着像能通用，实际协议形态对不上。
//
// 判据用 **stdlib 作独立 oracle**（encoding/base64 本身就是被收编的那份实现，
// 逐例差分而不是复述被检逻辑——差分能钉住「本方法 = stdlib 那个变体」这件事）。

// TestBase64Raw_DifferentialAgainstStdlib 本仓六个 Raw 方法逐例对表 stdlib：
// 编码逐字节相等、解码逐字节相等——口径漂移（多补填充 / 少解填充）当场暴露。
func TestBase64Raw_DifferentialAgainstStdlib(t *testing.T) {
	inputs := []string{
		"", "a", "ab", "abc", "hello", "你好，世界",
		strings.Repeat("x", 100), "\x00\xff\x10\xde\xad\xbe\xef",
	}
	for _, in := range inputs {
		raw := []byte(in)

		// 标准字母表
		if got, want := Base64RawEncode(in), base64.RawStdEncoding.EncodeToString(raw); got != want {
			t.Errorf("Base64RawEncode(%q) = %q, want %q", in, got, want)
		}
		if got, want := Base64RawEncodeBytes(raw), base64.RawStdEncoding.EncodeToString(raw); got != want {
			t.Errorf("Base64RawEncodeBytes(%q) = %q, want %q", in, got, want)
		}
		enc := base64.RawStdEncoding.EncodeToString(raw)
		if got, err := Base64RawDecode(enc); err != nil {
			t.Errorf("Base64RawDecode(%q) err = %v", enc, err)
		} else if got != in {
			t.Errorf("Base64RawDecode(%q) = %q, want %q", enc, got, in)
		}
		gotBytes, err := Base64RawDecodeBytes(enc)
		if err != nil {
			t.Errorf("Base64RawDecodeBytes(%q) err = %v", enc, err)
		} else if !bytes.Equal(gotBytes, raw) {
			t.Errorf("Base64RawDecodeBytes(%q) = %v, want %v", enc, gotBytes, raw)
		}

		// URL 字母表
		if got, want := Base64RawURLEncode(in), base64.RawURLEncoding.EncodeToString(raw); got != want {
			t.Errorf("Base64RawURLEncode(%q) = %q, want %q", in, got, want)
		}
		if got, want := Base64RawURLEncodeBytes(raw), base64.RawURLEncoding.EncodeToString(raw); got != want {
			t.Errorf("Base64RawURLEncodeBytes(%q) = %q, want %q", in, got, want)
		}
		urlEnc := base64.RawURLEncoding.EncodeToString(raw)
		if got, err := Base64RawURLDecode(urlEnc); err != nil {
			t.Errorf("Base64RawURLDecode(%q) err = %v", urlEnc, err)
		} else if got != in {
			t.Errorf("Base64RawURLDecode(%q) = %q, want %q", urlEnc, got, in)
		}
		urlBytes, urlErr := Base64RawURLDecodeBytes(urlEnc)
		if urlErr != nil {
			t.Errorf("Base64RawURLDecodeBytes(%q) err = %v", urlEnc, urlErr)
		} else if !bytes.Equal(urlBytes, raw) {
			t.Errorf("Base64RawURLDecodeBytes(%q) = %v, want %v", urlEnc, urlBytes, raw)
		}
	}
}

// TestBase64RawURL_BytesRoundTrip 钉住 RawURL 两个字节入口的往返一致性。
func TestBase64RawURL_BytesRoundTrip(t *testing.T) {
	inputs := []string{"", "a", "ab", "abc", "hello", "你好，世界", strings.Repeat("x", 100)}
	for _, in := range inputs {
		raw := []byte(in)
		enc := Base64RawURLEncodeBytes(raw)
		got, err := Base64RawURLDecodeBytes(enc)
		if err != nil {
			t.Errorf("Base64RawURLDecodeBytes(%q) err = %v", enc, err)
		} else if !bytes.Equal(got, raw) {
			t.Errorf("Base64RawURLDecodeBytes(%q) = %v, want %v", enc, got, raw)
		}
	}
}

// 字节入口的失败出口与字符串入口同款（非法输入报错，不返回部分结果）。
func TestBase64RawURL_BytesInvalidInput(t *testing.T) {
	if _, err := Base64RawURLDecodeBytes("!!not-base64!!"); err == nil {
		t.Error("非法输入必须报错")
	}
}

// 无填充变体的输出里不得出现 '='——JWT 段里出现填充字符即协议违规。
func TestBase64Raw_NoPaddingInOutput(t *testing.T) {
	// 长度 1/2/3 的输入在标准 base64 下都需要填充。
	for _, s := range []string{"a", "ab", "abc"} {
		for name, enc := range map[string]func(string) string{
			"Raw":    Base64RawEncode,
			"RawURL": Base64RawURLEncode,
		} {
			if got := enc(s); strings.Contains(got, "=") {
				t.Errorf("%s(%q) = %q, 不应含填充字符 '='", name, s, got)
			}
		}
	}
}

// URL 安全变体不得出现 '+' / '/'（标准字母表的两个非 URL 字符）。
func TestBase64RawURL_URLSafeAlphabet(t *testing.T) {
	// 这段字节的标准 base64 含 '+' 与 '/'。
	raw := []byte{0xfb, 0xff, 0xbe}
	if std := base64.StdEncoding.EncodeToString(raw); !strings.ContainsAny(std, "+/") {
		t.Skipf("样本未含 '+'/'/'（std = %q），跳过", std)
	}
	if got := Base64RawURLEncode(string(raw)); strings.ContainsAny(got, "+/") {
		t.Errorf("URL 安全变体不应含 '+'/'/': %q", got)
	}
}

// 陷阱钉死：**带填充变体解不了无填充串**——这正是 pkg#96 收编的根因。
func TestBase64Raw_VsPaddedVariants(t *testing.T) {
	const jwtSeg = "aGVsbG8" // "hello" 的无填充 base64url

	if _, err := Base64URLDecode(jwtSeg); err == nil {
		t.Errorf("Base64URLDecode（带填充）解无填充串应报错")
	}
	if _, err := Base64Decode(jwtSeg); err == nil {
		t.Errorf("Base64Decode（带填充）解无填充串应报错")
	}
	if got, err := Base64RawURLDecode(jwtSeg); err != nil || got != "hello" {
		t.Errorf("Base64RawURLDecode = (%q, %v), want (%q, nil)", got, err, "hello")
	}
	if got, err := Base64RawDecode(jwtSeg); err != nil || got != "hello" {
		t.Errorf("Base64RawDecode = (%q, %v), want (%q, nil)", got, err, "hello")
	}
}

// 反向边界：无填充变体也解不了带填充串（stdlib 同款严格语义，两套变体不互通）。
func TestBase64Raw_RejectsPaddedInput(t *testing.T) {
	if _, err := Base64RawDecode("aGVsbG8="); err == nil {
		t.Errorf("Base64RawDecode（无填充）解带填充串应报错")
	}
	if _, err := Base64RawURLDecode("aGVsbG8="); err == nil {
		t.Errorf("Base64RawURLDecode（无填充）解带填充串应报错")
	}
}

// 往返一致性：Raw 与带填充两套变体解出同一份明文。
func TestBase64Raw_RoundTrip(t *testing.T) {
	inputs := []string{"", "a", "ab", "abc", "hello", "你好，世界", strings.Repeat("x", 100)}
	for _, in := range inputs {
		if got, err := Base64RawDecode(Base64RawEncode(in)); err != nil || got != in {
			t.Errorf("Raw 往返 = (%q, %v), want (%q, nil)", got, err, in)
		}
		if got, err := Base64RawURLDecode(Base64RawURLEncode(in)); err != nil || got != in {
			t.Errorf("RawURL 往返 = (%q, %v), want (%q, nil)", got, err, in)
		}
	}
}

// 非法输入报错（与既有解码方法同款出口）。
func TestBase64Raw_InvalidInput(t *testing.T) {
	if _, err := Base64RawDecode("!!not-base64!!"); err == nil {
		t.Error("非法输入必须报错")
	}
	if _, err := Base64RawURLDecode("!!not-base64!!"); err == nil {
		t.Error("非法输入必须报错")
	}
	if _, err := Base64RawDecodeBytes("!!not-base64!!"); err == nil {
		t.Error("非法输入必须报错")
	}
}
