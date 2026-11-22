package pii

// 数字形态归一化（issue #297）。
//
// # 这组用例补的是哪条缝
//
// 识别此前只认**ASCII 连续数字**：全角数字、零宽字符插入、分组分隔符
// （空格/连字符/点）全部漏检，而这些形态会整串原样外发且 `blocked=false`——
// 宿主以为已保护，实际什么都没有。根因不是正则写错，而是缺一层归一化。
//
// 归一化与判定共用一条路径：Detect 在归一化文本上匹配，再把 span 映射回
// 原串字节偏移，处置按原串切片（含内部分隔符整段替换）。
//
// 已知边界（文档同步声明，不在本组用例里假装覆盖）：阿拉伯-印度数字、
// 字母/汉字夹在数字之间、数字间夹非分隔类符号（如 `+`）。

import (
	"strings"
	"testing"
)

func toFullWidth(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r - '0' + '０')
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'ａ')
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'Ａ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func spanTexts(s string, spans []Span) []string {
	out := make([]string, 0, len(spans))
	for _, sp := range spans {
		out = append(out, s[sp.Start:sp.End])
	}
	return out
}

// TestDetectNormalizedDigitForms 全角 / 零宽 / 分组分隔三类形态必须被检出，
// 且 span 覆盖原串里对应的整段（含内部分隔符）。
func TestDetectNormalizedDigitForms(t *testing.T) {
	restore(t)
	cases := []struct {
		name string
		in   string
		kind Kind
		text string
	}{
		{"全角手机号", toFullWidth("13812345678"), KindMobile, toFullWidth("13812345678")},
		{"零宽插入", "138\u200b12345678", KindMobile, "138\u200b12345678"},
		{"零宽连接符", "138\u200d12345678", KindMobile, "138\u200d12345678"},
		{"空格分组", "138 1234 5678", KindMobile, "138 1234 5678"},
		{"连字符分组", "138-1234-5678", KindMobile, "138-1234-5678"},
		{"点分组", "138.1234.5678", KindMobile, "138.1234.5678"},
		{"全角空格分组", "138\u30001234\u30005678", KindMobile, "138\u30001234\u30005678"},
		{"全角+全角空格", toFullWidth("138") + "\u3000" + toFullWidth("1234") + "\u3000" + toFullWidth("5678"), KindMobile, toFullWidth("138") + "\u3000" + toFullWidth("1234") + "\u3000" + toFullWidth("5678")},
		{"全角身份证", toFullWidth("110105199001011232"), KindResidentID, toFullWidth("110105199001011232")},
		{"空格分组身份证", "110105 19900101 1232", KindResidentID, "110105 19900101 1232"},
	}
	for _, c := range cases {
		spans := Detect(c.in)
		if len(spans) != 1 {
			t.Errorf("%s: Detect(%q) = %+v, want 1 span", c.name, c.in, spans)
			continue
		}
		if spans[0].Kind != c.kind || spans[0].Start != 0 || spans[0].End != len(c.in) {
			t.Errorf("%s: span = %+v (text %q), want %s [0,%d)",
				c.name, spans[0], spanTexts(c.in, spans), c.kind, len(c.in))
		}
	}
}

// TestApplyNormalizesBeforeMask 处置按归一化后的值打码：全角变半角、分组
// 分隔符随命中段落一起消失，输出不会再带出可复原的分组形态。
func TestApplyNormalizesBeforeMask(t *testing.T) {
	restore(t)
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"全角手机号", toFullWidth("13812345678"), "138****5678"},
		{"零宽插入", "138\u200b12345678", "138****5678"},
		{"空格分组", "138 1234 5678", "138****5678"},
		{"连字符分组", "138-1234-5678", "138****5678"},
		{"点分组", "138.1234.5678", "138****5678"},
		{"前缀 + 分组", "+86 138 1234 5678", "+86138****5678"},
		{"全角身份证", toFullWidth("110105199001011232"), "1101**************"},
		{"分组身份证", "110105 19900101 1232", "1101**************"},
	}
	for _, c := range cases {
		if got := redactIn(t, c.in); got != c.want {
			t.Errorf("%s: Apply(input, %q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestNormalizationDoesNotWidenFalsePositives 归一化不得把既有防误命中用例
// 拉进命中集合：跨字段拼接出来的长数字串、字母紧邻、12 位长度都必须仍不命中。
func TestNormalizationDoesNotWidenFalsePositives(t *testing.T) {
	restore(t)
	cases := []struct{ name, in string }{
		{"分组后多一位（12 位）", "138 1234 5678 9"},
		{"字母紧邻全角号", "a" + toFullWidth("13812345678")},
		{"全角字母紧邻", toFullWidth("a13812345678")},
		{"全角 12 位", toFullWidth("138123456789")},
		{"全角毫秒时间戳", toFullWidth("1728643200000")},
		{"随机 18 位全角数字", toFullWidth("123456789012345678")},
	}
	for _, c := range cases {
		if spans := Detect(c.in); len(spans) != 0 {
			t.Errorf("%s: Detect(%q) = %+v，归一化不得扩大误检面", c.name, c.in, spans)
		}
		if got := redactIn(t, c.in); got != c.in {
			t.Errorf("%s: 不应改写，实得 %q", c.name, got)
		}
	}

	// 既有「空格边界」语义不得被归一化收紧：独立的数字 token 后跟裸号
	// （如版本号 + 空格 + 手机号）仍按空格边界识别，不因为「把 1 和手机号
	// 拼成 12 位长串」而被拒。
	if got := redactIn(t, "版本 1 13812345678"); got != "版本 1 138****5678" {
		t.Errorf("空格边界下的裸号仍应识别，实得 %q", got)
	}
}

// TestNormalizedHashCorrelates 同一值的不同书写形态必须产出同一摘要
// （归一化在处置路径上共用，不只是识别路径）。
func TestNormalizedHashCorrelates(t *testing.T) {
	restore(t)
	if err := setDigestKeyForTest(t, "0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if err := setStrategyForTest(t, StrategyHash); err != nil {
		t.Fatal(err)
	}
	plain, _ := Apply(SurfaceInput, "13812345678")
	grouped, _ := Apply(SurfaceInput, "138 1234 5678")
	fullWidth, _ := Apply(SurfaceInput, toFullWidth("13812345678"))
	if plain != grouped || plain != fullWidth {
		t.Fatalf("同一值的不同形态应得同一摘要：plain=%q grouped=%q fullwidth=%q", plain, grouped, fullWidth)
	}
	if plain == "13812345678" {
		t.Fatal("hash 档不应原样输出")
	}
}
