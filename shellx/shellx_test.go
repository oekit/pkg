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

// TestIsSafeWord shlex.quote 口径的安全词判定：ASCII \w 与 @%+=:,./- 之外
// 一律不安全（re.ASCII 语义——非 ASCII 含中文一律落引号）；空串不是安全词。
func TestIsSafeWord(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"plain", "ls", true},
		{"underscore", "a_b", true},
		{"digits", "10", true},
		{"assign", "a=b", true},
		{"flag", "-x", true},
		{"host:port", "10.0.0.1:22", true},
		{"percent", "%s", true},
		{"path", "./bin/x", true},
		{"space", "a b", false},
		{"tilde", "~", false},
		{"bang", "!", false},
		{"glob", "*", false},
		{"semicolon", "a;b", false},
		{"dollar", "$HOME", false},
		{"cjk", "中文", false},
		{"tab", "a\tb", false},
		{"newline", "a\nb", false},
		{"quote", "a'b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSafeWord(tt.in); got != tt.want {
				t.Errorf("IsSafeWord(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestQuoteMinimal 与 CPython shlex.quote 黄金用例逐例一致。唯一刻意分叉：
// 内嵌单引号的拼接按本包 [Quote]（`'\”`）而非 CPython 的双引号拼接
// （`'"'"'`）——两种拼接 POSIX 语义等价，本包不引入第二套转义算法。
func TestQuoteMinimal(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "''"},
		{"safe word stays bare", "ls", "ls"},
		{"assign", "a=b", "a=b"},
		{"flag", "-x", "-x"},
		{"host:port", "10.0.0.1:22", "10.0.0.1:22"},
		{"path", "./bin/x", "./bin/x"},
		{"space", "a b", "'a b'"},
		{"tilde", "~", "'~'"},
		{"bang", "!", "'!'"},
		{"glob", "*", "'*'"},
		{"cjk", "中文", "'中文'"},
		{"tab", "a\tb", "'a\tb'"},
		{"embedded quote", "a'b", `'a'\''b'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := QuoteMinimal(tt.in); got != tt.want {
				t.Errorf("QuoteMinimal(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestQuoteMinimalDangerousPathByteIdentical 危险词路径不分叉：非安全词
// 经 QuoteMinimal 与 Quote 产出逐字节一致（省引号只是 Quote 的策略包装）。
func TestQuoteMinimalDangerousPathByteIdentical(t *testing.T) {
	inputs := []string{
		"", "a b", "it's", "a;rm -rf /", "$(whoami)", "`id`", "$HOME",
		`a"b`, `a\b`, "a # b", "a\nb", "trailing space ", "中文", "~", "!", "*",
	}
	for _, in := range inputs {
		if got, want := QuoteMinimal(in), Quote(in); got != want {
			t.Errorf("QuoteMinimal(%q) = %q, want Quote 的原样输出 %q", in, got, want)
		}
	}
}

// TestQuoteMinimalSurvivesARealShell 最强校验：把最小引用形态交给真实
// shell，回到的仍是原词（含被加引号的危险词与原样的安全词）。
func TestQuoteMinimalSurvivesARealShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	inputs := []string{
		"ls", "-x", "10.0.0.1:22", "a=b", "", "a b", "it's", "中文",
		"a;rm -rf /", "$(whoami)", "trailing space ",
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
