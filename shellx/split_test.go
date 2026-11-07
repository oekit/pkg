package shellx

import (
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"whitespace only", "  \t \n ", nil},
		{"single word", "cmd", []string{"cmd"}},
		{"multiple words", "cmd -f /tmp/x", []string{"cmd", "-f", "/tmp/x"}},
		{"collapses whitespace runs", "a \t\n b", []string{"a", "b"}},
		// 单引号：一切原样（含反斜杠、双引号、$）
		{"single quotes verbatim", `'a\b'`, []string{`a\b`}},
		{"single quote keeps dollar", "'$HOME'", []string{"$HOME"}},
		{"single quote keeps double quote", `'a"b'`, []string{`a"b`}},
		{"sh -c single quoted", "sh -c 'echo hi'", []string{"sh", "-c", "echo hi"}},
		// 双引号：反斜杠只转义 " \ $ ` 与换行；\x 字面保留
		{"double quote escape quote", `"a\"b"`, []string{`a"b`}},
		{"double quote escape backslash", `"a\\b"`, []string{`a\b`}},
		{"double quote escape dollar", `"a\$b"`, []string{`a$b`}},
		{"double quote escape backtick", "\"a\\`b\"", []string{"a`b"}},
		{"double quote keeps non-escape backslash", `"a\d\b"`, []string{`a\d\b`}},
		{"double quote keeps dollar unescaped", `"a$HOME"`, []string{"a$HOME"}},
		{"double quoted space stays one word", `"a b" c`, []string{"a b", "c"}},
		// 行拼接：反斜杠+换行消失（裸/双引号内同）
		{"line continuation bare", "a\\\nb", []string{"ab"}},
		{"line continuation in double quotes", "\"a\\\nb\"", []string{"ab"}},
		// 词内引号段与裸段粘连
		{"quote gluing", "a'b'c", []string{"abc"}},
		{"glued empty quotes", "a''b", []string{"ab"}},
		// 空参数保留
		{"empty argument", "cmd '' arg", []string{"cmd", "", "arg"}},
		{"only empty quotes", "''", []string{""}},
		{"empty double quotes", `""`, []string{""}},
		// 未加引号的 ; | & 是独立 token，相邻同类合并成 && / ||
		{"operators standalone", "a ; b", []string{"a", ";", "b"}},
		{"pipe token", "a | b", []string{"a", "|", "b"}},
		{"ampersand token", "a & b", []string{"a", "&", "b"}},
		{"&& glued to words", "a&&b", []string{"a", "&&", "b"}},
		{"|| glued to words", "a||b", []string{"a", "||", "b"}},
		{"&& then single &", "a&&&b", []string{"a", "&&", "&", "b"}},
		{"semicolons never merge", "a;;b", []string{"a", ";", ";", "b"}},
		{"quoted operator stays in word", `a '&&' b`, []string{"a", "&&", "b"}},
		{"quoted semicolon stays in word", `echo 'a;b'`, []string{"echo", "a;b"}},
		{"double quoted semicolon stays in word", `echo "a;b"`, []string{"echo", "a;b"}},
		// 引号内的 ; | & 不拆（判据按 token 序列走）
		{"pipe inside single quotes", `'a|b'`, []string{"a|b"}},
		// 多字节 UTF-8 按字节扫不误伤（元字符全是 ASCII，UTF-8 续字节 ≥0x80）
		{"utf-8 word", "echo 中文", []string{"echo", "中文"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Split(tt.in)
			if err != nil {
				t.Fatalf("Split(%q) unexpected error: %v", tt.in, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Split(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSplitErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"unclosed single quote", "echo 'unclosed", ErrUnclosedQuote},
		{"unclosed double quote", `echo "unclosed`, ErrUnclosedQuote},
		{"unclosed double quote after escape", `echo "unclosed\`, ErrUnclosedQuote},
		{"trailing backslash", `echo trailing\`, ErrTrailingEscape},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Split(tt.in)
			if err == nil {
				t.Fatalf("Split(%q) = %q, want error %v", tt.in, got, tt.want)
			}
			if err != tt.want {
				t.Errorf("Split(%q) error = %v, want %v", tt.in, err, tt.want)
			}
		})
	}
}

// TestSplitQuoteArgvRoundTrip 钉住 #243 的往返判据：
// Split(QuoteArgv(args...)) 必须逐项还原 args。这条判据的价值在「只测想
// 得到的样例」钉不住口径——往返测试随 Quote 的改动自动报警。
func TestSplitQuoteArgvRoundTrip(t *testing.T) {
	cases := [][]string{
		{"cmd"},
		{"cmd", "-f", "/tmp/a b.txt"},
		{"cmd", "", "arg"},                          // 空参数保留（不错位）
		{"sh", "-c", "echo 'quoted' \"$HOME\""},     // 引号/双引号/$ 混排
		{"echo", "it's", "a\"b", `a\b`},             // 单引号/双引号/反斜杠
		{"echo", "$HOME", "`id`", "$(whoami)"},      // 变量/命令替换形态（不展开）
		{"echo", "a;rm -rf /", "a|b", "a&&b"},       // 分隔符作为参数内容
		{"echo", "a\nb", "a\tb", "trailing space "}, // 换行/制表/尾随空格
		{"echo", "--flag=value", "#", "中文 参数"},      // 等号/井号/UTF-8
		{"cmd", "'", `"`, `\`},                      // 单个特殊字符参数
	}
	for _, args := range cases {
		line := QuoteArgv(args...)
		got, err := Split(line)
		if err != nil {
			t.Fatalf("Split(QuoteArgv(%q)) error: %v", args, err)
		}
		if !reflect.DeepEqual(got, args) {
			t.Errorf("Split(QuoteArgv(%q)) = %q (line %q), want roundtrip identity", args, got, line)
		}
	}
	// 空 argv：QuoteArgv 渲染为空串，Split 还原为 0 个 token。
	got, err := Split(QuoteArgv())
	if err != nil || len(got) != 0 {
		t.Errorf("Split(QuoteArgv()) = %q, %v; want empty, nil", got, err)
	}
}
