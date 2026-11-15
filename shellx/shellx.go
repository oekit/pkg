package shellx

import "strings"

// Quote quotes a string so that a POSIX shell passes it through as exactly
// one word, whatever it contains.
//
// The result is always a single-quoted word. Nothing inside single quotes is
// special to the shell, so the only character that needs care is the quote
// itself: it is closed, escaped and reopened ("'\”"), which keeps the
// surrounding word intact.
//
// An empty string becomes "”". An empty argument is still an argument, and
// dropping it would silently shift the whole command line: "cmd ” arg"
// must not become "cmd arg".
//
// Quote does not decide what to do with a newline or a path that climbs out
// of its root -- it only escapes. Those are policy questions, and the caller
// is the one that knows them.
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// QuoteArgv renders a command line from a list of arguments: every argument
// is quoted, and the results are joined with a single space.
//
// The empty argument list renders as the empty string.
func QuoteArgv(args ...string) string {
	if len(args) == 0 {
		return ""
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = Quote(a)
	}
	return strings.Join(quoted, " ")
}

// IsSafeWord 判定 s 是否可以不加引号直接作为 shell 参数（CPython
// shlex.quote 口径）：仅 ASCII \w（字母、数字、下划线）与 @%+=:,./-
// 六个符号视为安全，其余一律不安全。非 ASCII 一律不安全——对齐 CPython
// re.ASCII，避免 Unicode 的 \w 把中文/带变音符的字母误判为安全。
//
// 空串返回 false：空参数不是可以原样输出的词，必须写成一对单引号才能
// 占住位置（否则 cmd 后的空参数会消失，后续参数整体错位）。
func IsSafeWord(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9', c == '_':
			// ASCII \w：安全。
		case c == '@' || c == '%' || c == '+' || c == '=' || c == ':' || c == ',' || c == '.' || c == '/' || c == '-':
			// shlex.quote 的安全符号集：路径/版本/地址/参数形态常用。
		default:
			return false
		}
	}
	return true
}

// QuoteMinimal 按 CPython shlex.quote 的语义做「最小引用」：空串返回
// 一对单引号组成的空词；[IsSafeWord] 为真的词原样返回；其余走 [Quote]。
//
// 它只是 [Quote] 的策略包装，不引入第二套转义算法：危险词的输出与
// Quote 逐字节相同（内嵌单引号沿用 Quote 的关引号-转义-重开引号拼法）。
// 可读性优先（ls 仍是 ls，只有 a b 才变 'a b'），shell 语义与 Quote
// 等价。注意 CPython 的内嵌单引号拼法与 Quote 不同，但两者 shell 语义
// 相同、字节不同；要与 Quote 字节一致时统一用本函数的输出。
func QuoteMinimal(s string) string {
	if s == "" {
		return "''"
	}
	if IsSafeWord(s) {
		return s
	}
	return Quote(s)
}
