package shellx

import (
	"reflect"
	"testing"
)

// —— pkg#358 反向验证矩阵：重定向事实 / 引号内字面 / dynamic 标记 good/bad 成对钉住 ——

func TestAnalyzeRedirectFacts(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Facts
	}{
		// 写重定向：带空格与粘连两种形态都必须报出「写重定向 + 目标」
		{
			"write redirect spaced",
			"git status > /etc/passwd",
			Facts{Segments: []Segment{{
				Words:     []Word{{"git", false}, {"status", false}},
				Redirects: []Redirect{{Op: ">", Target: Word{"/etc/passwd", false}, Write: true}},
			}}},
		},
		{
			"write redirect glued",
			"git status >/etc/passwd",
			Facts{Segments: []Segment{{
				Words:     []Word{{"git", false}, {"status", false}},
				Redirects: []Redirect{{Op: ">", Target: Word{"/etc/passwd", false}, Write: true}},
			}}},
		},
		{
			"word then redirect glued",
			"echo a>b",
			Facts{Segments: []Segment{{
				Words:     []Word{{"echo", false}, {"a", false}},
				Redirects: []Redirect{{Op: ">", Target: Word{"b", false}, Write: true}},
			}}},
		},
		// fd 前缀：词首数字紧邻 < / > 是运算符的一部分（与 bash IO_NUMBER 同口径）
		{
			"fd write redirect",
			"git status 2>/dev/null",
			Facts{Segments: []Segment{{
				Words:     []Word{{"git", false}, {"status", false}},
				Redirects: []Redirect{{Op: "2>", Target: Word{"/dev/null", false}, Write: true}},
			}}},
		},
		{
			"fd digits alone are redirect",
			"echo 32>b",
			Facts{Segments: []Segment{{
				Words:     []Word{{"echo", false}},
				Redirects: []Redirect{{Op: "32>", Target: Word{"b", false}, Write: true}},
			}}},
		},
		{
			"fd digits glued to word are not",
			"echo a2>b",
			Facts{Segments: []Segment{{
				Words:     []Word{{"echo", false}, {"a2", false}},
				Redirects: []Redirect{{Op: ">", Target: Word{"b", false}, Write: true}},
			}}},
		},
		{
			"append redirect",
			"echo x >> /var/log/a",
			Facts{Segments: []Segment{{
				Words:     []Word{{"echo", false}, {"x", false}},
				Redirects: []Redirect{{Op: ">>", Target: Word{"/var/log/a", false}, Write: true}},
			}}},
		},
		{
			"both-stream redirect",
			"cmd &> out",
			Facts{Segments: []Segment{{
				Words:     []Word{{"cmd", false}},
				Redirects: []Redirect{{Op: "&>", Target: Word{"out", false}, Write: true}},
			}}},
		},
		{
			"both-stream redirect glued",
			"cmd &>>out",
			Facts{Segments: []Segment{{
				Words:     []Word{{"cmd", false}},
				Redirects: []Redirect{{Op: "&>>", Target: Word{"out", false}, Write: true}},
			}}},
		},
		{
			"bash gt-ampersand form",
			"cmd >& out",
			Facts{Segments: []Segment{{
				Words:     []Word{{"cmd", false}},
				Redirects: []Redirect{{Op: ">&", Target: Word{"out", false}, Write: true}},
			}}},
		},
		// 读侧
		{
			"read redirect",
			"cat < in.txt",
			Facts{Segments: []Segment{{
				Words:     []Word{{"cat", false}},
				Redirects: []Redirect{{Op: "<", Target: Word{"in.txt", false}, Write: false}},
			}}},
		},
		{
			"heredoc",
			"cat <<EOF",
			Facts{Segments: []Segment{{
				Words:     []Word{{"cat", false}},
				Redirects: []Redirect{{Op: "<<", Target: Word{"EOF", false}, Write: false}},
			}}},
		},
		{
			"heredoc tab strip",
			"cat <<-EOF",
			Facts{Segments: []Segment{{
				Words:     []Word{{"cat", false}},
				Redirects: []Redirect{{Op: "<<-", Target: Word{"EOF", false}, Write: false}},
			}}},
		},
		{
			"herestring",
			"cat <<< 'a b'",
			Facts{Segments: []Segment{{
				Words:     []Word{{"cat", false}},
				Redirects: []Redirect{{Op: "<<<", Target: Word{"a b", false}, Write: false}},
			}}},
		},
		// fd 复制 / 关闭：目标 "&N"/"&-"，按运算符方向归类（写侧保守）
		{
			"fd dup",
			"cmd 2>&1",
			Facts{Segments: []Segment{{
				Words:     []Word{{"cmd", false}},
				Redirects: []Redirect{{Op: "2>", Target: Word{"&1", false}, Write: true}},
			}}},
		},
		{
			"fd close",
			"cmd >&-",
			Facts{Segments: []Segment{{
				Words:     []Word{{"cmd", false}},
				Redirects: []Redirect{{Op: ">", Target: Word{"&-", false}, Write: true}},
			}}},
		},
		// 一个段可以只有重定向没有词（bash 的 "> out" 建空文件）
		{
			"redirect only",
			"> out",
			Facts{Segments: []Segment{{
				Redirects: []Redirect{{Op: ">", Target: Word{"out", false}, Write: true}},
			}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Analyze(tt.in)
			if err != nil {
				t.Fatalf("Analyze(%q) unexpected error: %v", tt.in, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Analyze(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

// TestAnalyzeQuotedAngleIsLiteral 钉住引号内/转义的 > < 是字面参数，
// **不得**报重定向（good/bad 成对：同输入裸形态是重定向）。
func TestAnalyzeQuotedAngleIsLiteral(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Word
	}{
		{"single quoted", "echo 'a>b'", []Word{{"echo", false}, {"a>b", false}}},
		{"double quoted", `echo "a>b"`, []Word{{"echo", false}, {"a>b", false}}},
		{"single quoted lt", "echo 'a<b'", []Word{{"echo", false}, {"a<b", false}}},
		{"escaped", `echo a\>b`, []Word{{"echo", false}, {"a>b", false}}},
		{"glued quoted", `echo x"a>b"y`, []Word{{"echo", false}, {"xa>by", false}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Analyze(tt.in)
			if err != nil {
				t.Fatalf("Analyze(%q) unexpected error: %v", tt.in, err)
			}
			if got.HasRedirect() {
				t.Errorf("Analyze(%q).HasRedirect() = true, want false（引号内 > 是字面参数）", tt.in)
			}
			if !reflect.DeepEqual(got.Segments[0].Words, tt.want) {
				t.Errorf("Analyze(%q) words = %+v, want %+v", tt.in, got.Segments[0].Words, tt.want)
			}
		})
	}
}

// TestAnalyzeDynamic 钉住动态词标记：命令替换/进程替换/变量展开标 dynamic，
// 单引号内一切字面不标。
func TestAnalyzeDynamic(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Word
	}{
		{"cmd subst", "echo $(rm -rf /)", []Word{{"echo", false}, {"$(rm -rf /)", true}}},
		{"backtick", "ls `whoami`", []Word{{"ls", false}, {"`whoami`", true}}},
		{"process subst read", "cat <(ls)", []Word{{"cat", false}, {"<(ls)", true}}},
		{"process subst write", "cat >(wc)", []Word{{"cat", false}, {">(wc)", true}}},
		{"var", "echo $HOME", []Word{{"echo", false}, {"$HOME", true}}},
		{"braced var", "echo ${HOME:-x}", []Word{{"echo", false}, {"${HOME:-x}", true}}},
		{"subst inside double quotes", `echo "$(rm -rf /)"`, []Word{{"echo", false}, {"$(rm -rf /)", true}}},
		{"var inside double quotes", `echo "$HOME"`, []Word{{"echo", false}, {"$HOME", true}}},
		// 反例：单引号内一切字面
		{"single quoted subst", "echo '$(rm -rf /)'", []Word{{"echo", false}, {"$(rm -rf /)", false}}},
		{"single quoted var", "echo '$HOME'", []Word{{"echo", false}, {"$HOME", false}}},
		{"escaped dollar", `echo \$HOME`, []Word{{"echo", false}, {"$HOME", false}}},
		{"bare dollar is literal", "echo a$ b", []Word{{"echo", false}, {"a$", false}, {"b", false}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Analyze(tt.in)
			if err != nil {
				t.Fatalf("Analyze(%q) unexpected error: %v", tt.in, err)
			}
			if !reflect.DeepEqual(got.Segments[0].Words, tt.want) {
				t.Errorf("Analyze(%q) words = %+v, want %+v", tt.in, got.Segments[0].Words, tt.want)
			}
		})
	}
}

// TestAnalyzeDynamicRedirectTarget 钉住读侧目标含动态也要能报——
// 只看 op 会放行 cat < $(rm -rf build)（bashguard 第六轮审计踩过同款）。
func TestAnalyzeDynamicRedirectTarget(t *testing.T) {
	got, err := Analyze("cat < $(rm -rf build)")
	if err != nil {
		t.Fatalf("Analyze unexpected error: %v", err)
	}
	r := got.Segments[0].Redirects
	want := []Redirect{{Op: "<", Target: Word{"$(rm -rf build)", true}, Write: false}}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("redirects = %+v, want %+v", r, want)
	}
	if got.HasWriteRedirect() {
		t.Error("HasWriteRedirect() = true, want false（读侧目标）")
	}

	got, err = Analyze("echo x > $OUT")
	if err != nil {
		t.Fatalf("Analyze unexpected error: %v", err)
	}
	r = got.Segments[0].Redirects
	want = []Redirect{{Op: ">", Target: Word{"$OUT", true}, Write: true}}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("redirects = %+v, want %+v", r, want)
	}
}

// TestAnalyzeSegments 钉住按 ; && || | |& & 切段（段内 Words 与 Redirects
// 分列，词序保留）。
func TestAnalyzeSegments(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Segment
	}{
		{
			"segment with redirect then next command",
			"git status > /etc/passwd; rm -rf /",
			[]Segment{
				{
					Sep:       ";",
					Words:     []Word{{"git", false}, {"status", false}},
					Redirects: []Redirect{{Op: ">", Target: Word{"/etc/passwd", false}, Write: true}},
				},
				{Words: []Word{{"rm", false}, {"-rf", false}, {"/", false}}},
			},
		},
		{
			"pipe",
			"a | b",
			[]Segment{{Sep: "|", Words: []Word{{"a", false}}}, {Words: []Word{{"b", false}}}},
		},
		{
			"glued and-or",
			"a&&b||c",
			[]Segment{
				{Sep: "&&", Words: []Word{{"a", false}}},
				{Sep: "||", Words: []Word{{"b", false}}},
				{Words: []Word{{"c", false}}},
			},
		},
		{
			"pipe stderr merge is one separator",
			"a |& b",
			[]Segment{{Sep: "|&", Words: []Word{{"a", false}}}, {Words: []Word{{"b", false}}}},
		},
		{
			"background trailing",
			"cmd &",
			[]Segment{{Sep: "&", Words: []Word{{"cmd", false}}}},
		},
		{
			"background then next",
			"a & b",
			[]Segment{{Sep: "&", Words: []Word{{"a", false}}}, {Words: []Word{{"b", false}}}},
		},
		{
			"empty interior segment kept for adjacent separators",
			"a;;b",
			[]Segment{
				{Sep: ";", Words: []Word{{"a", false}}},
				{Sep: ";"},
				{Words: []Word{{"b", false}}},
			},
		},
		{
			"leading separator keeps its empty segment",
			"; a",
			[]Segment{{Sep: ";"}, {Words: []Word{{"a", false}}}},
		},
		{
			"trailing separator no trailing empty segment",
			"a ;",
			[]Segment{{Sep: ";", Words: []Word{{"a", false}}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Analyze(tt.in)
			if err != nil {
				t.Fatalf("Analyze(%q) unexpected error: %v", tt.in, err)
			}
			if !reflect.DeepEqual(got.Segments, tt.want) {
				t.Errorf("Analyze(%q) segments = %+v, want %+v", tt.in, got.Segments, tt.want)
			}
		})
	}
}

// TestFactsPredicates 钉住 issue 反向验证的 good/bad 成对判据。
func TestFactsPredicates(t *testing.T) {
	bad := "git status > /etc/passwd"
	f, err := Analyze(bad)
	if err != nil {
		t.Fatal(err)
	}
	if !f.HasRedirect() || !f.HasWriteRedirect() {
		t.Errorf("%q: HasRedirect/HasWriteRedirect = %v/%v, want true/true", bad, f.HasRedirect(), f.HasWriteRedirect())
	}

	good := `echo "a>b"`
	f, err = Analyze(good)
	if err != nil {
		t.Fatal(err)
	}
	if f.HasRedirect() || f.HasWriteRedirect() {
		t.Errorf("%q: HasRedirect/HasWriteRedirect = %v/%v, want false/false（引号内字面）", good, f.HasRedirect(), f.HasWriteRedirect())
	}

	dyn := "ls `whoami`"
	f, err = Analyze(dyn)
	if err != nil {
		t.Fatal(err)
	}
	if !f.HasDynamic() {
		t.Errorf("%q: HasDynamic() = false, want true", dyn)
	}

	lit := "echo '$(rm -rf /)'"
	f, err = Analyze(lit)
	if err != nil {
		t.Fatal(err)
	}
	if f.HasDynamic() {
		t.Errorf("%q: HasDynamic() = true, want false（单引号字面）", lit)
	}
}

func TestAnalyzeErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"unclosed single quote", "echo 'unclosed", ErrUnclosedQuote},
		{"unclosed double quote", `echo "unclosed`, ErrUnclosedQuote},
		{"trailing escape", `echo trailing\`, ErrTrailingEscape},
		{"unclosed cmd subst", "echo $(rm", ErrUnclosedSubstitution},
		{"unclosed backtick", "echo `rm", ErrUnclosedSubstitution},
		{"unclosed braced var", "echo ${x", ErrUnclosedSubstitution},
		{"unclosed process subst", "echo <(ls", ErrUnclosedSubstitution},
		{"redirect no target", "cmd >", ErrMissingRedirectTarget},
		{"redirect target is separator", "cmd > ; x", ErrMissingRedirectTarget},
		{"redirect target is redirect", "cmd > > x", ErrMissingRedirectTarget},
		{"redirect target missing after space", "cmd >   ", ErrMissingRedirectTarget},
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

// TestAnalyzeArgvMatchesSplit 钉住词文本口径与 Split 一致（无重定向/命令
// 替换的输入）：Analyze 的段词序列 + 段尾分隔符 == Split 的 token 序列，
// 消费方从 Split 迁移到事实面不该看到词文本漂移。
func TestAnalyzeArgvMatchesSplit(t *testing.T) {
	ins := []string{
		"cmd -f /tmp/x",
		"sh -c 'echo hi'",
		`echo "a b" c`,
		"echo 中文",
		"cmd '' arg",
		`echo a\>b`,
		"echo 'a;b'",
	}
	for _, in := range ins {
		want, err := Split(in)
		if err != nil {
			t.Fatalf("Split(%q) error: %v", in, err)
		}
		f, err := Analyze(in)
		if err != nil {
			t.Fatalf("Analyze(%q) error: %v", in, err)
		}
		var got []string
		for _, seg := range f.Segments {
			got = append(got, seg.Argv()...)
			if seg.Sep != "" {
				got = append(got, seg.Sep)
			}
		}
		if len(got) == 0 {
			got = nil
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Analyze(%q) argv+sep = %q, Split = %q", in, got, want)
		}
	}
}
