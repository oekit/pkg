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

// TestMatchSimpleGlob_StarCrossesSlash 钉住「* 跨 /」语义（#210）：
// 与 MatchGlob 同（TestMatchGlob_StarCrossesSlash）、与 filepath.Match 反。
// 实现即 HasPrefix/HasSuffix/Contains，天然不感知分隔符——名字含 `/` 的
// 消费方（分支名/K8s 资源名）按 filepath.Match 直觉预期「星不跨段」会
// 静默多匹配。
func TestMatchSimpleGlob_StarCrossesSlash(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"kube-*", "kube-system/nested", true},   // 后缀星吞掉整段斜杠路径
		{"*/nested", "kube-system/nested", true}, // 前缀星跨命名空间段
		{"*system*", "kube-system/nested", true}, // Contains 不感知斜杠
		{"*", "a/b", true},                       // 任何非空串（含斜杠）
		{"kube-*", "release/v1", false},          // 跨段前提仍是前后缀匹配
	}
	for _, c := range cases {
		if got := MatchSimpleGlob(c.pattern, c.name); got != c.want {
			t.Fatalf("MatchSimpleGlob(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}
