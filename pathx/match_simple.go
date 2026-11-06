package pathx

import "strings"

// MatchSimpleGlob 是**单星**名字通配匹配：只有**第一颗** `*` 是通配符，
// 其余字符（含第二颗起的 `*` 与 `?`）一律按字面量比较。
//
// 口径（与 full-backtracking 的 MatchGlob 是**两个语义各归其位**的原语，不要混用）：
//
//   - pattern 或 name 为空串 ⇒ **不匹配**（MatchGlob 里 "*" 匹配空串）；
//   - pattern 恒为 "*" ⇒ 一切非空 name 都匹配；
//   - pattern 不含 `*` ⇒ 精确相等；
//   - "*x*"（长度 > 2）⇒ Contains(name, "x")；
//   - 其余含 `*` ⇒ 第一颗星切前后缀，HasPrefix && HasSuffix && len(name) >= len(prefix)+len(suffix)
//     ——注意第二颗起的 `*` 落在**字面量**里（"a*b*c" 的后缀是字面量 "b*c"）。
//
// 什么时候用它而不是 MatchGlob：消费方要的是**保守的单星匹配**（多星/`?` 不展开），
// 且**空串必须判不匹配**——两条都是 MatchGlob 的反语义，混用会静默改判。
//
// 用法:
//
//	pathx.MatchSimpleGlob("*", "kube-system") // true
//	pathx.MatchSimpleGlob("kube-*", "kube-system") // true
//	pathx.MatchSimpleGlob("kube-*", "")        // false（空串不匹配）
//	pathx.MatchSimpleGlob("a*b*c", "axb*c")    // true（第二颗星按字面量）
func MatchSimpleGlob(pattern, s string) bool {
	if pattern == "" || s == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == s
	}
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") && len(pattern) > 2 {
		return strings.Contains(s, pattern[1:len(pattern)-1])
	}
	starIdx := strings.Index(pattern, "*")
	if starIdx < 0 {
		// 不可达（上方 Contains("*") 已过滤），gocritic offBy1 防御位
		return pattern == s
	}
	prefix := pattern[:starIdx]
	suffix := pattern[starIdx+1:]
	if prefix == "" && suffix == "" {
		return true
	}
	if prefix == "" {
		return strings.HasSuffix(s, suffix)
	}
	if suffix == "" {
		return strings.HasPrefix(s, prefix)
	}
	return strings.HasPrefix(s, prefix) && strings.HasSuffix(s, suffix) && len(s) >= len(prefix)+len(suffix)
}
