package pathx

import "testing"

// MatchSimpleGlob 语义固化：单星、无 ?、空串不匹配。
// 来源：kuboard-go simpleGlobMatch（Spring 复刻已验证实现）逐字上移。
func TestMatchSimpleGlob(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"*", "anything", true},
		{"*", "", false}, // name 空串不匹配（与 MatchGlob 的关键差异）
		{"", "x", false},
		{"", "", false},
		{"kube-*", "kube-system", true},
		{"kube-*", "other", false},
		{"*system", "kube-system", true},
		{"*system", "system", true}, // * 可匹配空（后缀）
		{"kube*", "kube", true},     // 前缀命中即可（HasPrefix 语义）
		{"exact", "exact", true},
		{"exact", "exact2", false},
		{"a*b", "axb", true},
		{"a*b", "ab", true}, // len(name)=2 >= len(a)+len(b)=2（约束是 >=）
		{"a*b", "xa", false},
		// 第二颗星起按字面量处理（单星语义）
		{"a*b*c", "axb*c", true},
		{"a*b*c", "axbyc", false},
		{"*a*", "xa", true},
		{"*a*", "a", true}, // Contains("a", "a") = true
		{"**", "**", true}, // "**"：suffix="*"（字面量）
		{"a?c", "abc", false},
		{"a?c", "a?c", true},
	}
	for _, c := range cases {
		if got := MatchSimpleGlob(c.pattern, c.name); got != c.want {
			t.Fatalf("MatchSimpleGlob(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}
