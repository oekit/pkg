package pathx

// MatchGlob 通配匹配：`*` 匹配任意字符序列（**可跨 `/`**），`?` 匹配单个字符
// （按 **rune** 计，不是字节）。
//
// 与 MatchesPath 是**两个语义各归其位**的原语，不要混用：
//
//   - MatchesPath 是 filepath.Match 语义：`*` **不跨** `/`，用于**路径段**
//     匹配（".gitignore 风格"的路径过滤）；
//   - MatchGlob 用于**名字**匹配——分支名（"feature/x"）、ref 名
//     （"refs/heads/x"）、仓库全名（"owner/repo"）这类对象本身含 `/`，
//     用 filepath.Match 语义答不了："feature/*" 匹配不到 "feature/x"。
//
// 为什么需要它：`*` 不跨 `/` 这一点让「排除所有」这类配置**彻底失效且毫无
// 察觉**——exclude_globs 写 ["*"]（期望排除全部）时，因为 `*` 匹配不到
// "owner/repo"，这条配置等于没配；同理 include_globs 写 ["*"] 会把仓库
// **全部过滤掉**（一个都匹配不上）。配置语法合法、无报错、只是不生效。
//
// 口径（与本包既有原语的风格一致，写死在这里）：
//
//   - `*` 匹配任意**字符序列**（可跨 `/`，也可匹配空）；
//   - `?` 匹配**一个 rune**（不是字节——按字节切会把中文名切出半个字符）；
//   - 其余字符按**字面量**比较，**不做 shell 风格转义**（`\` 只是普通字符，
//     不引入 `[...]` 字符类——那会让「模式」本身变成需要另一套解析器的语言）。
//
// 用法:
//
//	pathx.MatchGlob("*", "owner/repo")            // true（排除全部）
//	pathx.MatchGlob("feature/*", "feature/x")     // true
//	pathx.MatchGlob("feature/*", "feature/x/y")   // true（* 跨 /）
//	pathx.MatchGlob("a?c", "abc")                 // true
//	pathx.MatchGlob("a?c", "a中c")                 // true（按 rune 计）
//	pathx.MatchGlob("a?c", "ac")                  // false
func MatchGlob(pattern, s string) bool {
	p := []rune(pattern)
	v := []rune(s)

	var (
		i, j     int
		starIdx  = -1 // 最近一个 '*' 在模式中的位置
		matchIdx int  // 该 '*' 目前已吞掉的字符数
	)

	for j < len(v) {
		switch {
		case i < len(p) && p[i] == '*':
			// 记下 '*' 的位置，先假定它匹配空串，失败时回溯加长。
			//
			// 这条必须**先于**字面量比较判断：模式里的 '*' 永远是通配符，
			// 不能因为被匹配的串里恰好也有 '*' 就当成字面量相等放行。
			starIdx = i
			matchIdx = j
			i++
		case i < len(p) && (p[i] == '?' || p[i] == v[j]):
			// 字面量相等，或 '?' 吃掉一个 rune。
			i++
			j++
		case starIdx >= 0:
			// 回溯：把上一个 '*' 多吃一个字符再试。
			i = starIdx + 1
			matchIdx++
			j = matchIdx
		default:
			return false
		}
	}
	// 模式剩余部分必须全是 '*'（它们可以匹配空串）。
	for i < len(p) && p[i] == '*' {
		i++
	}
	return i == len(p)
}
