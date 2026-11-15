package shellx

import (
	"os/exec"
	"strings"
	"testing"
)

func TestQuote(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "abc", "'abc'"},
		{"empty", "", "''"},
		{"space", "a b", "'a b'"},
		{"single quote", "it's", `'it'\''s'`},
		{"leading quote", "'x", `''\''x'`},
		{"trailing quote", "x'", `'x'\'''`},
		{"semicolon", "a;rm -rf /", "'a;rm -rf /'"},
		{"command substitution", "$(whoami)", "'$(whoami)'"},
		{"backtick", "`id`", "'`id`'"},
		{"dollar", "$HOME", "'$HOME'"},
		{"double quote", `a"b`, `'a"b'`},
		{"backslash", `a\b`, `'a\b'`},
		{"hash is not a comment here", "a # b", "'a # b'"},
		{"newline", "a\nb", "'a\nb'"},
		{"tab", "a\tb", "'a\tb'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Quote(tt.in); got != tt.want {
				t.Errorf("Quote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestQuoteArgv(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"empty", nil, ""},
		{"one", []string{"cmd"}, "'cmd'"},
		{"several", []string{"cmd", "-f", "/tmp/x"}, "'cmd' '-f' '/tmp/x'"},
		{"keeps an empty argument", []string{"cmd", "", "arg"}, "'cmd' '' 'arg'"},
		{"quotes every argument", []string{"a b", "c;d"}, "'a b' 'c;d'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := QuoteArgv(tt.in...); got != tt.want {
				t.Errorf("QuoteArgv(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// The strongest check available: hand the quoted value to a real shell and
// see whether what comes back is the original string. A table of expected
// outputs can only prove that Quote matches what the author believed; this
// proves what the shell actually does.
func TestQuoteSurvivesARealShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	inputs := []string{
		"plain",
		"",
		"a b",
		"it's",
		"a;rm -rf /",
		"$(whoami)",
		"`id`",
		"$HOME",
		`a"b`,
		`a\b`,
		"a # b",
		"a\nb",
		"--flag=value",
		"trailing space ",
	}
	for _, in := range inputs {
		out, err := exec.Command(sh, "-c", "printf '%s' "+Quote(in)).Output()
		if err != nil {
			t.Errorf("sh -c 'printf %%s %s': %v", Quote(in), err)
			continue
		}
		if string(out) != in {
			t.Errorf("shell returned %q for input %q, want %q", string(out), in, in)
		}
	}
}

// The same check for a whole argument list: every argument must arrive as
// exactly one argument, with empty ones still present.
func TestQuoteArgvSurvivesARealShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	args := []string{"first", "second with space", "", "third;not-a-command", "it's"}
	// The command and its format string are ours, not the caller's data, so
	// only the arguments go through QuoteArgv.
	cmd := "printf '%s\\n' " + QuoteArgv(args...)
	out, err := exec.Command(sh, "-c", cmd).Output()
	if err != nil {
		t.Fatalf("sh -c %q: %v", cmd, err)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(got) != len(args) {
		t.Fatalf("shell produced %d arguments, want %d: %q", len(got), len(args), got)
	}
	for i := range args {
		if got[i] != args[i] {
			t.Errorf("argument %d = %q, want %q", i, got[i], args[i])
		}
	}
}

// TestIsSafeWord 契约：ASCII \w 与 @%+=:,./- 可以原样进命令行；其余字符
// （含非 ASCII、空白、shell 元字符）一律不安全。空串不是可原样输出的词。
func TestIsSafeWord(t *testing.T) {
	safe := []string{
		"ls", "A", "a0", "_", "A0_z",
		"-x", "--flag=value", "a=b",
		"10.0.0.1:22", "/tmp/a-b_c.d",
		"@%+=:,./-", "a@b", "a%b", "a+b", "a,b",
	}
	for _, s := range safe {
		if !IsSafeWord(s) {
			t.Errorf("IsSafeWord(%q) = false，应为 true（可原样输出）", s)
		}
	}

	unsafe := []string{
		"",
		" ", "a b",
		"~", "!", "*", "?", "[", "]", "{", "}",
		"$HOME", "`id`", "$(whoami)", "'", `"`, `\`,
		";", "|", "&", "<", ">", "(", ")", "#",
		"\n", "\t", "中文", "é", "a\x00b",
	}
	for _, s := range unsafe {
		if IsSafeWord(s) {
			t.Errorf("IsSafeWord(%q) = true，应为 false（须引用）", s)
		}
	}
}

// TestQuoteMinimal 契约：CPython shlex.quote 的黄金用例——安全词原样、
// 空串 "”"、危险词走单引号形态（内嵌单引号按 Quote 拼接）。
func TestQuoteMinimal(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "''"},
		{"plain", "ls", "ls"},
		{"assignment", "a=b", "a=b"},
		{"flag", "-x", "-x"},
		{"address", "10.0.0.1:22", "10.0.0.1:22"},
		{"path", "/tmp/a-b_c.d", "/tmp/a-b_c.d"},
		{"space", "a b", "'a b'"},
		{"tilde", "~", "'~'"},
		{"bang", "!", "'!'"},
		{"glob", "*", "'*'"},
		{"non-ascii", "中文", "'中文'"},
		{"tab", "a\tb", "'a\tb'"},
		{"dollar", "$HOME", "'$HOME'"},
		{"semicolon", "a;rm -rf /", "'a;rm -rf /'"},
		{"newline", "a\nb", "'a\nb'"},
		{"embedded quote", "a'b", `'a'\''b'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := QuoteMinimal(tt.in); got != tt.want {
				t.Errorf("QuoteMinimal(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}

	// 危险词路径不分叉：引号拼接与 Quote 逐字节一致（不引入第二套转义）。
	for _, in := range []string{"a b", "~", "!", "*", "中文", "a\tb", "a'b", "$HOME"} {
		if got, want := QuoteMinimal(in), Quote(in); got != want {
			t.Errorf("QuoteMinimal(%q) = %q，应与 Quote 一致 %q", in, got, want)
		}
	}
}

// 与 Quote 同一强度检查：把 QuoteMinimal 的产物交给真实 shell，原样取回
// 输入——安全词不加引号后仍必须是一个词，危险词引用后必须不被解释。
func TestQuoteMinimalSurvivesARealShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	inputs := []string{
		"ls", "a=b", "-x", "10.0.0.1:22", "/tmp/a-b_c.d",
		"a b", "~", "!", "*", "中文", "a\tb", "it's", "$HOME", "",
		"a;rm -rf /", "`id`", `a\b`,
	}
	for _, in := range inputs {
		out, err := exec.Command(sh, "-c", "printf '%s' "+QuoteMinimal(in)).Output()
		if err != nil {
			t.Errorf("sh -c 'printf %%s %s': %v", QuoteMinimal(in), err)
			continue
		}
		if string(out) != in {
			t.Errorf("shell returned %q for input %q, want %q", string(out), in, in)
		}
	}
}
