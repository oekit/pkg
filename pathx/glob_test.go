package pathx

import "testing"

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		in      string
		want    bool
	}{
		// '*' 跨 '/'（与 filepath.Match 的关键差异）
		{"star matches all", "*", "owner/repo", true},
		{"star matches plain", "*", "main", true},
		{"star matches empty", "*", "", true},
		{"star matches nested path", "feature/*", "feature/x/y", true},
		{"star matches one segment", "feature/*", "feature/x", true},
		{"star prefix crosses slash", "owner/*", "owner/repo/sub", true},
		{"star mid pattern", "a*b", "a/x/y/b", true},

		// '*' 不匹配时
		{"star no match prefix", "feature/*", "other/x", false},
		{"star no match empty tail", "feature/*", "feature", false},
		{"star no match other prefix", "other/*", "feature/x", false},
		{"literal mismatch", "abc", "abd", false},
		{"literal prefix mismatch", "abc", "ab", false},
		{"literal longer input", "abc", "abcd", false},
		{"empty pattern empty input", "", "", true},
		{"empty pattern nonempty input", "", "a", false},

		// '?' 匹配一个 rune（不是字节）
		{"question single char", "a?c", "abc", true},
		{"question no match empty", "a?c", "ac", false},
		{"question no match two chars", "a?c", "abbc", false},
		{"question matches multibyte rune", "a?c", "a中c", true},
		{"question alone multibyte", "?", "中", true},
		{"question alone ascii", "?", "a", true},
		{"question alone empty", "?", "", false},
		{"question alone two runes", "?", "ab", false},

		// 字面量比较（无 shell 风格转义、无字符类）
		{"dot is literal", "a.c", "abc", false},
		{"dot literal match", "a.c", "a.c", true},
		{"bracket is literal", "[ab]", "a", false},
		{"bracket literal match", "[ab]", "[ab]", true},
		{"backslash is literal", `a\b`, `a\b`, true},
		{"backslash does not escape star", `a\*b`, `a\b`, true},
		{"backslash does not escape question", `a\?b`, `ab`, false},

		// 组合
		{"star and question", "*/x?", "owner/repo/xy", true},
		{"question then star", "a?/*", "ab/x/y", true},
		{"double star same as single", "a**b", "axxxb", true},

		// 模式里的 '*' 永远是通配符——不能因为被匹配的串里也有 '*'
		// 就当成字面量相等放行（否则 "*" 匹配不到 "*/[*" 这类串）。
		{"star pattern vs literal star in input", "*", "*/[*", true},
		{"star pattern vs literal star", "*", "*", true},
		{"star pattern vs trailing star", "*", "a*", true},
		{"question vs literal question in input", "?", "?", true},
		{"question pattern vs literal star input", "?", "*", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchGlob(tt.pattern, tt.in); got != tt.want {
				t.Errorf("MatchGlob(%q, %q) = %v, want %v", tt.pattern, tt.in, got, tt.want)
			}
		})
	}
}

// MatchGlob 与 MatchesPath 的关键差异必须被钉住：
// '*' 跨 '/'（MatchesPath 的 filepath.Match 语义不跨）。
func TestMatchGlob_StarCrossesSlash(t *testing.T) {
	const pattern = "feature/*"
	const target = "feature/x/y"

	if !MatchGlob(pattern, target) {
		t.Errorf("MatchGlob(%q, %q) 应为 true：'*' 需跨 '/'", pattern, target)
	}
	// MatchesPath 是路径段语义（不跨 '/'），两者不该被混用——显式记下差异。
	if MatchesPath([]string{pattern}, target) {
		t.Errorf("MatchesPath(%q, %q) 为 true：路径段语义下 '*' 不跨 '/'", pattern, target)
	}
}

// 「排除所有」这个真实误配形态必须成立：pattern "*" 能匹配仓库全名。
func TestMatchGlob_ExcludeAllPattern(t *testing.T) {
	const repoFullName = "owner/repo"
	if !MatchGlob("*", repoFullName) {
		t.Errorf("MatchGlob(\"*\", %q) 应为 true：这是「排除全部」配置依赖的语义", repoFullName)
	}
}

// TestMatchesPath_GlobstarBoundaries 钉住 #424 的两条 /** 边界（此前零声明，
// 审计/跳过清单按直觉配置会漏面）：
//  1. 裸父不匹配——"/api/**" 不含 "/api" 自身（前缀分支要求以 "/api/" 起头）；
//  2. 中段 "**" 不是 globstar——"*" 不跨 "/"，中段 ** 退化为两个单段 *，
//     只碰巧匹配单段；跨层级子树只有后缀位 "/**" 一种写法。
func TestMatchesPath_GlobstarBoundaries(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		// 裸父边界
		{"/api/**", "/api", false},
		{"/api/**", "/api/", true},
		{"/api/**", "/api/users", true},
		{"/api/**", "/api/users/42/detail", true},
		// 要连根入面必须显式并列两条
		{"/api", "/api", true},
		{"/api", "/api/users", false},
		// 中段 ** 语义（filepath.Match 原样：* 不跨 /）
		{"/api/**/detail", "/api/x/detail", true},    // 恰好单段（两段 * 各吃一段）
		{"/api/**/detail", "/api/x/y/detail", false}, // 不跨层级
		// 后缀 globstar 跨层级（对照正例）
		{"/api/**", "/api/x/y/detail", true},
	}
	for _, c := range cases {
		if got := MatchesPath([]string{c.pattern}, c.path); got != c.want {
			t.Errorf("MatchesPath(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
		// 严格档与宽松档同口径。
		gotStrict, err := MatchesPathStrict([]string{c.pattern}, c.path)
		if err != nil || gotStrict != c.want {
			t.Errorf("MatchesPathStrict(%q, %q) = (%v, %v), want (%v, nil)", c.pattern, c.path, gotStrict, err, c.want)
		}
	}
}
