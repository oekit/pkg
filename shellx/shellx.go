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
