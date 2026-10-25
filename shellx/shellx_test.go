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
