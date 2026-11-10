package shellx

import (
	"reflect"
	"strings"
	"testing"
)

// segs 是测试断言用的段形状（Op + 普通参数文本 + 重定向摘要）。
type wantRedirect struct {
	op    string
	fd    int
	write bool
	tText string
	tDyn  bool
}

type wantSeg struct {
	op        string
	words     []string
	dynamics  []bool
	redirects []wantRedirect
}

func analyzeOne(t *testing.T, line string) *Analysis {
	t.Helper()
	a, err := Analyze(line)
	if err != nil {
		t.Fatalf("Analyze(%q) unexpected error: %v", line, err)
	}
	return a
}

func checkSegs(t *testing.T, line string, a *Analysis, want []wantSeg) {
	t.Helper()
	if len(a.Segments) != len(want) {
		t.Fatalf("Analyze(%q) got %d segments %+v, want %d %+v", line, len(a.Segments), a.Segments, len(want), want)
	}
	for i, w := range want {
		got := a.Segments[i]
		if got.Op != w.op {
			t.Errorf("Analyze(%q) seg[%d].Op = %q, want %q", line, i, got.Op, w.op)
		}
		texts := got.Texts()
		if len(texts) != len(w.words) {
			t.Errorf("Analyze(%q) seg[%d].Texts() = %q, want %q", line, i, texts, w.words)
		} else {
			for j := range texts {
				if texts[j] != w.words[j] {
					t.Errorf("Analyze(%q) seg[%d].Texts() = %q, want %q", line, i, texts, w.words)
					break
				}
			}
		}
		if w.dynamics != nil {
			for j, d := range w.dynamics {
				if j < len(got.Words) && got.Words[j].Dynamic != d {
					t.Errorf("Analyze(%q) seg[%d].Words[%d].Dynamic = %v, want %v (text %q)",
						line, i, j, got.Words[j].Dynamic, d, got.Words[j].Text)
				}
			}
		}
		if len(got.Redirects) != len(w.redirects) {
			t.Errorf("Analyze(%q) seg[%d] got %d redirects %+v, want %d %+v",
				line, i, len(got.Redirects), got.Redirects, len(w.redirects), w.redirects)
			continue
		}
		for j, rw := range w.redirects {
			r := got.Redirects[j]
			if r.Op != rw.op || r.Fd != rw.fd || r.Write != rw.write ||
				r.Target.Text != rw.tText || r.Target.Dynamic != rw.tDyn {
				t.Errorf("Analyze(%q) seg[%d].Redirects[%d] = %+v, want %+v", line, i, j, r, rw)
			}
		}
	}
}

// TestAnalyzeRedirect 钉住 pkg#358 的核心判据：重定向运算符与目标可辨、
// 写侧/读侧可辨、显式 fd 可辨——前缀规则判据不再被「普通 token 排在
// argv 之后」整类绕过。
func TestAnalyzeRedirect(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []wantSeg
	}{
		// 反向验证第 1 条：两种粘连形态都报「写重定向 + 目标 /etc/passwd」
		{"write redirect spaced", "git status > /etc/passwd", []wantSeg{
			{op: "", words: []string{"git", "status"}, dynamics: []bool{false, false},
				redirects: []wantRedirect{{op: ">", fd: -1, write: true, tText: "/etc/passwd"}}},
		}},
		{"write redirect glued", "git status >/etc/passwd", []wantSeg{
			{op: "", words: []string{"git", "status"}, dynamics: []bool{false, false},
				redirects: []wantRedirect{{op: ">", fd: -1, write: true, tText: "/etc/passwd"}}},
		}},
		// issue 现状清单逐条
		{"append redirect", "echo hi >>out", []wantSeg{
			{op: "", words: []string{"echo", "hi"},
				redirects: []wantRedirect{{op: ">>", fd: -1, write: true, tText: "out"}}},
		}},
		{"explicit fd write", "git status 2>/dev/null", []wantSeg{
			{op: "", words: []string{"git", "status"},
				redirects: []wantRedirect{{op: ">", fd: 2, write: true, tText: "/dev/null"}}},
		}},
		{"read redirect", "cat < in.txt", []wantSeg{
			{op: "", words: []string{"cat"},
				redirects: []wantRedirect{{op: "<", fd: -1, write: false, tText: "in.txt"}}},
		}},
		{"heredoc", "cat <<EOF", []wantSeg{
			{op: "", words: []string{"cat"},
				redirects: []wantRedirect{{op: "<<", fd: -1, write: false, tText: "EOF"}}},
		}},
		{"heredoc strip tabs", "cat <<-EOF", []wantSeg{
			{op: "", words: []string{"cat"},
				redirects: []wantRedirect{{op: "<<-", fd: -1, write: false, tText: "EOF"}}},
		}},
		{"here string", "cat <<<data", []wantSeg{
			{op: "", words: []string{"cat"},
				redirects: []wantRedirect{{op: "<<<", fd: -1, write: false, tText: "data"}}},
		}},
		{"both streams", "cmd &>all", []wantSeg{
			{op: "", words: []string{"cmd"},
				redirects: []wantRedirect{{op: "&>", fd: -1, write: true, tText: "all"}}},
		}},
		{"both streams append", "cmd &>>all", []wantSeg{
			{op: "", words: []string{"cmd"},
				redirects: []wantRedirect{{op: "&>>", fd: -1, write: true, tText: "all"}}},
		}},
		{"dup fd", "cmd 2>&1", []wantSeg{
			{op: "", words: []string{"cmd"},
				redirects: []wantRedirect{{op: ">&", fd: 2, write: true, tText: "1"}}},
		}},
		{"dup fd read", "cmd 2<&1", []wantSeg{
			{op: "", words: []string{"cmd"},
				redirects: []wantRedirect{{op: "<&", fd: 2, write: false, tText: "1"}}},
		}},
		{"noclobber", "cmd >|out", []wantSeg{
			{op: "", words: []string{"cmd"},
				redirects: []wantRedirect{{op: ">|", fd: -1, write: true, tText: "out"}}},
		}},
		{"multi digit fd", "cmd 10>>log", []wantSeg{
			{op: "", words: []string{"cmd"},
				redirects: []wantRedirect{{op: ">>", fd: 10, write: true, tText: "log"}}},
		}},
		// 多重定向：顺序保留；fd 粘连才生效（`2 >x` 的 2 是普通参数）
		{"two redirects", "cmd >a 2>b", []wantSeg{
			{op: "", words: []string{"cmd"}, redirects: []wantRedirect{
				{op: ">", fd: -1, write: true, tText: "a"},
				{op: ">", fd: 2, write: true, tText: "b"},
			}},
		}},
		{"fd digits must touch op", "cmd 2 >x", []wantSeg{
			{op: "", words: []string{"cmd", "2"},
				redirects: []wantRedirect{{op: ">", fd: -1, write: true, tText: "x"}}},
		}},
		{"quoted fd digits are word", `cmd "2">x`, []wantSeg{
			{op: "", words: []string{"cmd", "2"},
				redirects: []wantRedirect{{op: ">", fd: -1, write: true, tText: "x"}}},
		}},
		{"redirect target may be quoted", `cmd >"a b"`, []wantSeg{
			{op: "", words: []string{"cmd"},
				redirects: []wantRedirect{{op: ">", fd: -1, write: true, tText: "a b"}}},
		}},
		{"glued word before op", "echo a>b", []wantSeg{
			{op: "", words: []string{"echo", "a"},
				redirects: []wantRedirect{{op: ">", fd: -1, write: true, tText: "b"}}},
		}},
		{"target then another redirect", "cmd >a>b", []wantSeg{
			{op: "", words: []string{"cmd"}, redirects: []wantRedirect{
				{op: ">", fd: -1, write: true, tText: "a"},
				{op: ">", fd: -1, write: true, tText: "b"},
			}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkSegs(t, tt.in, analyzeOne(t, tt.in), tt.want)
		})
	}
}

// TestAnalyzeQuotedNotRedirect 钉住反向验证第 2 条：引号内的 `>` 是字面
// 参数，不得报重定向。
func TestAnalyzeQuotedNotRedirect(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		texts []string
	}{
		{"double quoted gt", `echo "a>b"`, []string{"echo", "a>b"}},
		{"single quoted gt", `echo 'a>b'`, []string{"echo", "a>b"}},
		{"backtick subst holds gt", "echo `a>b`", []string{"echo", "`a>b`"}},
		{"dollar subst holds gt", "echo $(a>b)", []string{"echo", "$(a>b)"}},
		{"escaped gt", `echo a\>b`, []string{"echo", "a>b"}},
		{"double quoted lt", `echo "a<b"`, []string{"echo", "a<b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := analyzeOne(t, tt.in)
			if a.HasRedirect() {
				t.Errorf("Analyze(%q).HasRedirect() = true, want false（引号内/转义的 > 是字面参数）", tt.in)
			}
			checkSegs(t, tt.in, a, []wantSeg{{words: tt.texts}})
		})
	}
}

// TestAnalyzeDynamic 钉住反向验证第 3 条：命令替换/进程替换/变量标 dynamic，
// 单引号字面不标——注意 Text 相同、Dynamic 相反的成对样例是本判据的要害。
func TestAnalyzeDynamic(t *testing.T) {
	tests := []struct {
		name string
		in   string
		text string
		dyn  bool
	}{
		// bad（标 dynamic）
		{"backtick", "ls `whoami`", "`whoami`", true},
		{"dollar paren", "echo $(rm -rf /)", "$(rm -rf /)", true},
		{"bare var", "echo $HOME", "$HOME", true},
		{"double quoted var", `echo "$HOME"`, "$HOME", true},
		{"double quoted subst", `echo "$(id)"`, "$(id)", true},
		{"double quoted backtick", "echo \"`id`\"", "`id`", true},
		{"process subst read", "diff <(a) <(b)", "<(a)", true},
		{"process subst write", "diff >(a) >(b)", ">(a)", true},
		{"braced var", "echo ${HOME}", "${HOME}", true},
		{"glued subst", "echo a$(x)b", "a$(x)b", true},
		// good（不标）——与 bad 成对，同 Text 反结论
		{"single quoted subst literal", "echo '$(rm -rf /)'", "$(rm -rf /)", false},
		{"single quoted var literal", "echo '$HOME'", "$HOME", false},
		{"escaped dollar", `echo \$HOME`, "$HOME", false},
		{"escaped backtick", "echo \\`id\\`", "`id`", false},
		{"double quoted escaped subst", `echo "\$(id)"`, "$(id)", false},
		{"lone dollar", "echo a$ b", "a$", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := analyzeOne(t, tt.in)
			seg := a.Segments[0]
			var w Word
			for _, cand := range seg.Words {
				if cand.Text == tt.text {
					w = cand
				}
			}
			if w.Text != tt.text {
				t.Fatalf("Analyze(%q) word %q not found in %+v", tt.in, tt.text, seg.Words)
			}
			if w.Dynamic != tt.dyn {
				t.Errorf("Analyze(%q) word %q Dynamic = %v, want %v", tt.in, tt.text, w.Dynamic, tt.dyn)
			}
		})
	}
}

// TestAnalyzeDynamicTarget 钉住反向验证第 4 条：读侧重定向目标含动态也必须
// 可报——只看 op 会放行 `cat < $(rm -rf build)`。
func TestAnalyzeDynamicTarget(t *testing.T) {
	a := analyzeOne(t, "cat < $(cmd)")
	checkSegs(t, "cat < $(cmd)", a, []wantSeg{
		{op: "", words: []string{"cat"}, redirects: []wantRedirect{
			{op: "<", fd: -1, write: false, tText: "$(cmd)", tDyn: true},
		}},
	})
	if !a.HasDynamic() {
		t.Errorf("Analyze(%q).HasDynamic() = false, want true", "cat < $(cmd)")
	}
	// 写侧目标同理
	a = analyzeOne(t, "echo x > $(mktemp)")
	checkSegs(t, "echo x > $(mktemp)", a, []wantSeg{
		{op: "", words: []string{"echo", "x"}, redirects: []wantRedirect{
			{op: ">", fd: -1, write: true, tText: "$(mktemp)", tDyn: true},
		}},
	})
}

// TestAnalyzeSubstIsOneWord 钉住 bash 词法口径：$( ) / ` ` 内部空格不切词
// （`$(rm -rf /)` 是一个词），替换原文逐字保留。
func TestAnalyzeSubstIsOneWord(t *testing.T) {
	a := analyzeOne(t, "echo $(rm -rf /)")
	checkSegs(t, "echo $(rm -rf /)", a, []wantSeg{
		{op: "", words: []string{"echo", "$(rm -rf /)"}, dynamics: []bool{false, true}},
	})
	// 嵌套与引号内的括号不参与配对
	a = analyzeOne(t, `echo $(echo ')')`)
	checkSegs(t, `echo $(echo ')')`, a, []wantSeg{
		{op: "", words: []string{"echo", "$(echo ')')"}, dynamics: []bool{false, true}},
	})
	// 替换内部命令不递归分析（内部的 > 不是外层重定向）
	if a := analyzeOne(t, "echo $(a > b)"); a.HasRedirect() {
		t.Errorf("Analyze(%q).HasRedirect() = true, want false（> 在替换内部）", "echo $(a > b)")
	}
}

// TestAnalyzeSegments 钉住分段形态：按 ; && || | & 切段、Op 记录结束本段的
// 控制符、空段保留、QuoteArgv 拼装往返成立。
func TestAnalyzeSegments(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []wantSeg
	}{
		{"and", "a && b", []wantSeg{
			{op: "&&", words: []string{"a"}},
			{op: "", words: []string{"b"}},
		}},
		{"pipe", "a | b", []wantSeg{
			{op: "|", words: []string{"a"}},
			{op: "", words: []string{"b"}},
		}},
		{"or then semi", "a || b ; c", []wantSeg{
			{op: "||", words: []string{"a"}},
			{op: ";", words: []string{"b"}},
			{op: "", words: []string{"c"}},
		}},
		{"background", "sleep 5 &", []wantSeg{
			{op: "&", words: []string{"sleep", "5"}},
		}},
		{"per segment redirects", "a >f | b <g", []wantSeg{
			{op: "|", words: []string{"a"}, redirects: []wantRedirect{{op: ">", fd: -1, write: true, tText: "f"}}},
			{op: "", words: []string{"b"}, redirects: []wantRedirect{{op: "<", fd: -1, write: false, tText: "g"}}},
		}},
		{"empty segments kept", "a;;b", []wantSeg{
			{op: ";", words: []string{"a"}},
			{op: ";", words: nil},
			{op: "", words: []string{"b"}},
		}},
		{"quoted op not a separator", `echo 'a;b' && echo '|'`, []wantSeg{
			{op: "&&", words: []string{"echo", "a;b"}},
			{op: "", words: []string{"echo", "|"}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := analyzeOne(t, tt.in)
			checkSegs(t, tt.in, a, tt.want)
			// 往返判据：段的 Texts() 经 QuoteArgv 拼装后 Split 逐项还原
			for i, seg := range a.Segments {
				texts := seg.Texts()
				got, err := Split(QuoteArgv(texts...))
				if err != nil {
					t.Fatalf("Split(QuoteArgv(%q)) error: %v", texts, err)
				}
				if len(got) != len(texts) {
					t.Fatalf("roundtrip seg[%d] of %q: got %q, want %q", i, tt.in, got, texts)
				}
				for j := range got {
					if got[j] != texts[j] {
						t.Errorf("roundtrip seg[%d] of %q: got %q, want %q", i, tt.in, got, texts)
						break
					}
				}
			}
		})
	}
	// 纯空白输入没有段（与 Split 空输入口径一致）
	if a := analyzeOne(t, "  \t \n "); len(a.Segments) != 0 {
		t.Errorf("Analyze(whitespace) = %+v, want no segments", a.Segments)
	}
}

// TestAnalyzeErrors 钉住判不了就报错的纪律（不尽力解析）。
func TestAnalyzeErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"unclosed single quote", "echo 'unclosed", ErrUnclosedQuote},
		{"unclosed double quote", `echo "unclosed`, ErrUnclosedQuote},
		{"trailing backslash", `echo trailing\`, ErrTrailingEscape},
		{"unclosed dollar paren", "echo $(rm -rf /", ErrUnclosedSubstitution},
		{"unclosed backtick", "echo `whoami", ErrUnclosedSubstitution},
		{"unclosed process subst", "diff <(a", ErrUnclosedSubstitution},
		{"unclosed brace var", "echo ${HOME", ErrUnclosedSubstitution},
		{"missing target", "echo a >", ErrMissingRedirectTarget},
		{"missing target before control", "echo a > ; b", ErrMissingRedirectTarget},
		{"missing target before op", "echo a > > b", ErrMissingRedirectTarget},
		{"missing target before newline", "echo a >\nb", ErrMissingRedirectTarget},
		{"missing target at eol after fd", "echo a 2>", ErrMissingRedirectTarget},
		{"bad fd", "echo a 99999999999999999999>x", ErrBadRedirect},
		// 引号内的替换没闭合照样报错
		{"unclosed subst in double quotes", `echo "$(id"`, ErrUnclosedSubstitution},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Analyze(tt.in)
			if err == nil {
				t.Fatalf("Analyze(%q) = %+v, want error %v", tt.in, got, tt.want)
			}
			if err != tt.want {
				t.Errorf("Analyze(%q) error = %v, want %v", tt.in, err, tt.want)
			}
		})
	}
}

// TestAnalyzeHasHelpers 钉住问询面语义。
func TestAnalyzeHasHelpers(t *testing.T) {
	if a := analyzeOne(t, "git status"); a.HasRedirect() || a.HasDynamic() {
		t.Errorf("Analyze(git status) has-facts = (%v,%v), want (false,false)", a.HasRedirect(), a.HasDynamic())
	}
	if a := analyzeOne(t, "git status >/etc/passwd"); !a.HasRedirect() {
		t.Errorf("Analyze(git status >/etc/passwd).HasRedirect() = false, want true")
	}
	if a := analyzeOne(t, "echo $(id)"); !a.HasDynamic() {
		t.Errorf("Analyze(echo $(id)).HasDynamic() = false, want true")
	}
	// 只有重定向目标动态时 HasDynamic 也为真
	if a := analyzeOne(t, "cat < $(cmd)"); !a.HasDynamic() {
		t.Errorf("Analyze(cat < $(cmd)).HasDynamic() = false, want true")
	}
}

// TestAnalyzeSplitCompat 钉住兼容面：词文本与 Split 同口径的样例两边一致
// （重定向/替换形态除外——那是 Analyze 的新增事实，Split 口径不动）。
func TestAnalyzeSplitCompat(t *testing.T) {
	lines := []string{
		"sh -c 'echo hi'",
		"cmd '' arg",
		"a'b'c",
		"echo \"a b\" c",
		"echo 中文",
	}
	for _, line := range lines {
		sp, err := Split(line)
		if err != nil {
			t.Fatalf("Split(%q) error: %v", line, err)
		}
		a := analyzeOne(t, line)
		var texts []string
		for _, seg := range a.Segments {
			texts = append(texts, seg.Texts()...)
		}
		if !reflect.DeepEqual(texts, sp) {
			t.Errorf("Analyze(%q) texts = %q, Split = %q, want identical", line, texts, sp)
		}
	}
}

// TestAnalyzeComplexCommand 综合样例：一条真实形态的命令行，三类事实齐全。
func TestAnalyzeComplexCommand(t *testing.T) {
	line := "git commit -m \"fix $(id)\" >/tmp/log 2>&1 && echo done | tee >(cat)"
	a := analyzeOne(t, line)
	if len(a.Segments) != 3 {
		t.Fatalf("Analyze(%q) got %d segments, want 3", line, len(a.Segments))
	}
	s0 := a.Segments[0]
	if strings.Join(s0.Texts(), " ") != "git commit -m fix $(id)" {
		t.Errorf("seg0 texts = %q", s0.Texts())
	}
	if !s0.Words[3].Dynamic {
		t.Errorf("seg0 word[3] %q Dynamic = false, want true", s0.Words[3].Text)
	}
	if len(s0.Redirects) != 2 || !s0.Redirects[0].Write || s0.Redirects[1].Fd != 2 {
		t.Errorf("seg0 redirects = %+v, want 写侧两连（>/tmp/log、2>&1）", s0.Redirects)
	}
	if !a.Segments[2].Words[1].Dynamic {
		t.Errorf("seg2 word[1] Dynamic = false, want true（进程替换）")
	}
}
