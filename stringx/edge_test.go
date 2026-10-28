package stringx

import (
	"testing"
)

// TestToCamelCase_InvalidUTF8 回归测试：非法 UTF-8 字节不得 panic。
//
// 缺陷成因：range 把非法字节解成 U+FFFD，而 utf8.RuneLen(U+FFFD) = 3（U+FFFD
// 是 3 字节 rune），但非法字节**实际**只占 1 字节 ⇒ 用 RuneLen 切片会越界：
// s = "\xff"（1 字节）⇒ s[3:] ⇒ panic "slice bounds out of range [3:1]"。
func TestToCamelCase_InvalidUTF8(t *testing.T) {
	cases := []string{
		"\xff",
		"\xff\xfe",
		"a\xff",
		"\xc3",         // 截断的 2 字节序列
		"\xe4\xb8",     // 截断的 3 字节序列
		"\xf0\x9f\x92", // 截断的 4 字节序列
	}
	for _, s := range cases {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("ToCamelCase(%q) panic: %v", s, r)
				}
			}()
			// 尾部原样透传（函数只处理首 rune），因此输出可能仍含非法字节
			// ——这不是缺陷，只要求「不 panic」且首 rune 已被小写化。
			got := ToCamelCase(s)
			if got == "" && s != "" {
				t.Errorf("ToCamelCase(%q) 返回空串", s)
			}
		}()
	}
}

// TestToCamelCase_Valid 合法输入的行为不变（回归护栏）。
func TestToCamelCase_Valid(t *testing.T) {
	cases := map[string]string{
		"":       "",
		"A":      "a",
		"ABC":    "aBC",
		"abc":    "abc",
		"汉字":     "汉字",
		"Ärger":  "ärger",
		"ǅungla": "ǆungla",
	}
	for in, want := range cases {
		if got := ToCamelCase(in); got != want {
			t.Errorf("ToCamelCase(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestPluralize_EmptyWord_KeepsCount 回归测试：word 为空时计数不得丢失。
//
// 缺陷成因：空词分支返回字面量 "0"——Pluralize(5, "") 返回的也是 "0"。
func TestPluralize_EmptyWord_KeepsCount(t *testing.T) {
	cases := map[int]string{
		0: "0 ",
		2: "2 ",
		5: "5 ",
	}
	for count, want := range cases {
		if got := Pluralize(count, ""); got != want {
			t.Errorf("Pluralize(%d, \"\") = %q，期望 %q", count, got, want)
		}
	}
}

// TestPluralize_Normal 既有行为不变（回归护栏）。
func TestPluralize_Normal(t *testing.T) {
	cases := []struct {
		count int
		word  string
		want  string
	}{
		{1, "file", "1 file"},
		{3, "file", "3 files"},
		{5, "story", "5 stories"},
		{2, "box", "2 boxes"},
		{2, "dish", "2 dishes"},
		{2, "church", "2 churches"},
	}
	for _, c := range cases {
		if got := Pluralize(c.count, c.word); got != c.want {
			t.Errorf("Pluralize(%d, %q) = %q，期望 %q", c.count, c.word, got, c.want)
		}
	}
}
