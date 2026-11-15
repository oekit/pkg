package shellx

import (
	"strings"
	"unicode/utf8"
)

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

// IsSafeWord 按 Python shlex.quote 口径判定一个词是否可安全地不加引号：
// 非 ASCII 一律不安全（与 CPython `re.ASCII` 语义一致——中文等落单引号
// 形态）；ASCII 范围内 `\w`（字母/数字/下划线）与 `@%+=:,./-` 之外一律
// 不安全。空串不是安全词（空参数必须以 ” 形态在场，见 [Quote]）。
//
// 它只是「哪些词可以省引号」的策略判定，不是拒绝策略——本包不做拒绝
// （见 [Quote] 文档），危险词交给 [QuoteMinimal]/[Quote] 转义。
func IsSafeWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r >= utf8.RuneSelf { // 非 ASCII（含非法 UTF-8 的 RuneError）不安全
			return false
		}
		if r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			continue
		}
		switch r {
		case '@', '%', '+', '=', ':', ',', '.', '/', '-':
			continue
		}
		return false
	}
	return true
}

// QuoteMinimal = Python shlex.quote：安全词原样、其余按 [Quote] 单引号
// 形态（可读性优先的引用形态，shell 语义与 Quote 等价——POSIX shell 对
// 两种形态解析出同一个词）。空串无安全词可言，落 Quote 的 ”。
//
// 危险词路径不分叉：QuoteMinimal 与 Quote 对同一危险词产出逐字节一致，
// 不引入第二套转义算法。
func QuoteMinimal(s string) string {
	if IsSafeWord(s) {
		return s
	}
	return Quote(s)
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
