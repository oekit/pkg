package shellx

import (
	"errors"
	"strings"
)

var (
	// ErrUnclosedSubstitution 表示命令行里有未闭合的命令替换/进程替换/变量
	// 形态（$(、反引号、<( )/>( )、${ }）。判不了就报错，不尽力解析——
	// 与 ErrUnclosedQuote 同口径（pkg#358）。
	ErrUnclosedSubstitution = errors.New("shellx: unclosed substitution in command line")
	// ErrMissingRedirectTarget 表示重定向运算符后面没有目标词，或目标形态
	// 判不了（如读侧 <& 后跟非 fd 目标）。
	ErrMissingRedirectTarget = errors.New("shellx: missing redirect target in command line")
)

// Word 是一个 shell 词及其词法事实。
type Word struct {
	// Text 是词文本：引号剥除、转义还原，与 Split 的词同口径。差别只有一处：
	// 命令替换/进程替换（$( )、反引号、<( )/>( )）整体算**一个**词、替换体按
	// 原文保留（含其中的引号与转义符）——Split 没有替换体概念、会在体内的
	// 空白处拆开（pkg#358）。
	Text string
	// Dynamic 标记词内含「运行时会被 shell 展开」的成分：命令替换 $( )、
	// 反引号、进程替换 <( )/>( )、变量展开 $VAR/${ }/$?/$$ 等。
	// 口径：
	//   - 单引号内一切字面——'$(rm -rf /)' 不标（反验证据）；
	//   - 双引号内 shell 仍展开 $( )、反引号与 $VAR，故同样标；
	//   - 转义掉的 \$ 不标；$ 后不构成展开形态的（如 "a$ b"）按字面、不标；
	//   - <( )/>( ) 只在未加引号时是进程替换（引号内 < ( 是字面）。
	Dynamic bool
}

// Redirect 是一条重定向的词法事实：重定向运算符与目标同普通词**分开**
// （pkg#358 的核心——Split 里它们是普通 argv 元素，前缀判据整类绕过）。
type Redirect struct {
	// Op 是运算符原文，含 fd 前缀与特殊形态："<"、"<<"、"<<-"、"<<<"、
	// ">"、">>"、"2>"、"&>"、"&>>"、">&"（bash 的 >&word = &>word 双流形态）。
	Op string
	// Target 是目标词（Word.Text 为目标文本、Dynamic 为字面性——「目标是否
	// 字面」= !Target.Dynamic）。fd 复制/关闭形态（2>&1、>&-）目标为
	// "&1"/"&-"，恒为字面；读侧目标含动态同样报出——只看 op 会放行
	// cat < $(rm -rf build)（bashguard 第六轮审计同款坑）。
	Target Word
	// Write 按运算符方向归类：> 族（含 <>、>& 双流、fd 复制/关闭）为写侧
	// ——保守口径，2>&1 不写文件但归写侧（要细分看 Target.Text 是否 &N）；
	// < 族为读侧。
	Write bool
}

// Segment 是一条命令段（按 ; && || | |& & 切分）。
type Segment struct {
	// Sep 是段尾分隔符原文（";"、"&&"、"||"、"|"、"|&"、"&"），末段为 ""。
	// |& 是 bash 的 2>&1 | 合并形态、算**一个**分隔符；相邻分隔符各自收束
	// 一个空段（a;;b → [a ;][ ;][b]），行尾分隔符不产生尾随空段。
	Sep string
	// Words 是段内普通词（命令名与参数，词序保留），重定向目标不在其中。
	// Words 与 Redirects 分列 ⇒ 两者的相对位置不保留（echo a > f b 与
	// echo a b > f 同形）——「词」与「重定向运算符+目标」分开正是本结构的
	// 目的；要保词序用 Words，要保重定向事实用 Redirects。
	Words []Word
	// Redirects 是段内重定向列表。
	Redirects []Redirect
}

// Facts 是一条命令行的词法事实面：词、重定向、段三分，供前缀/白名单判据
// 消费（「先分词、再按 argv 前缀比规则」的判据此前只能拿到 Split 的纯词法
// 拆词，重定向/命令替换与普通参数不可分，一条只读规则放行任意文件写与任意
// 命令执行——pkg#358）。
type Facts struct {
	// Segments 按 ; && || | |& & 切分的命令段（下游「逐段取最严」）。
	Segments []Segment
}

// HasRedirect 报告是否有任何重定向（读侧也算）。
func (f Facts) HasRedirect() bool {
	for _, seg := range f.Segments {
		if len(seg.Redirects) > 0 {
			return true
		}
	}
	return false
}

// HasWriteRedirect 报告是否有写侧重定向（> 族，含 fd 复制/关闭——保守口径，
// 细分看 Redirect.Target.Text）。
func (f Facts) HasWriteRedirect() bool {
	for _, seg := range f.Segments {
		for _, r := range seg.Redirects {
			if r.Write {
				return true
			}
		}
	}
	return false
}

// HasDynamic 报告是否有任何动态词（普通词或重定向目标含展开成分）。
func (f Facts) HasDynamic() bool {
	for _, seg := range f.Segments {
		for _, w := range seg.Words {
			if w.Dynamic {
				return true
			}
		}
		for _, r := range seg.Redirects {
			if r.Target.Dynamic {
				return true
			}
		}
	}
	return false
}

// Argv 返回段内普通词的 argv（词序，重定向目标不在内）——前缀判据的输入；
// QuoteArgv(s.Argv()...) 拼装即该段的词序列渲染（不含重定向）。
func (s Segment) Argv() []string {
	if len(s.Words) == 0 {
		return nil
	}
	out := make([]string, len(s.Words))
	for i, w := range s.Words {
		out[i] = w.Text
	}
	return out
}

// Analyze 把一条命令行解析成词法事实（词 / 重定向运算符+目标 / 段）。
//
// 口径（POSIX shell 词法，只解析、不展开）：
//   - 词文本与 Split 同口径（引号段粘连、单引号原样、双引号转义规则一致）；
//     命令替换/进程替换整体算一个词、替换体按原文保留（见 Word.Text）；
//   - 未加引号的 < > 是重定向运算符，与目标分开记录：>、>>、<、<<、<<-、
//     <<<、<>、N>、N<、&>、&>> 都覆盖；词首数字紧邻 < / > 是 fd 前缀
//     （echo 32>b 是 fd 32 重定向，echo a2>b 的 a2 是词——与 bash 的
//     IO_NUMBER 同口径）；> < 粘在词尾同样切开（echo a>b → 词 a + 重定向）；
//   - 引号内与转义的 > < 是字面参数，**不得**报重定向（echo "a>b"）；
//   - 2>&1、>&- 等 fd 复制/关闭形态：目标 "&N"/"&-"，按运算符方向归类；
//     >&word 非 fd 形态是 bash 的 &> 等价（Op 记 "&"，归写侧）；
//   - 动态词标记见 Word.Dynamic；
//   - 按 ; && || | |& & 切段，见 Segment.Sep；
//   - 判不了就报错（ErrUnclosedQuote、ErrTrailingEscape、
//     ErrUnclosedSubstitution、ErrMissingRedirectTarget），不做尽力解析。
//
// 边界（如实声明，不伪装全知）：
//   - heredoc（<<、<<-）体按普通词继续解析（多行输入）：体内的 > 与展开
//     会按普通事实报出（保守侧——多报不少报），delimiter 目标记为 Target；
//   - 不处理注释（#）——与 Split 同口径；
//   - 替换体的边界扫描按括号/引号/反引号配对，极端嵌套（双引号内嵌套命令
//     替换再含双引号）不保证边界精度——动态标记不受影响（$ ( 已标）。
func Analyze(s string) (Facts, error) {
	a := &analyzer{s: s}
	var (
		facts Facts
		cur   Segment
	)
	flush := func(sep string) {
		cur.Sep = sep
		facts.Segments = append(facts.Segments, cur)
		cur = Segment{}
	}
	for a.i < len(a.s) {
		c := a.s[a.i]
		switch {
		case isSpace(c):
			a.i++
		case c == ';':
			flush(";")
			a.i++
		case c == '|':
			switch {
			case strings.HasPrefix(a.s[a.i:], "||"):
				flush("||")
				a.i += 2
			case strings.HasPrefix(a.s[a.i:], "|&"): // bash 的 2>&1 | 合并形态
				flush("|&")
				a.i += 2
			default:
				flush("|")
				a.i++
			}
		case c == '&':
			switch {
			case strings.HasPrefix(a.s[a.i:], "&&"):
				flush("&&")
				a.i += 2
			case strings.HasPrefix(a.s[a.i:], "&>"): // &> / &>> 双流重定向
				r, err := a.scanRedirect()
				if err != nil {
					return Facts{}, err
				}
				cur.Redirects = append(cur.Redirects, r)
			default:
				flush("&")
				a.i++
			}
		case c == '<' || c == '>':
			if strings.HasPrefix(a.s[a.i:], "<(") || strings.HasPrefix(a.s[a.i:], ">(") {
				// 进程替换是词成分，不是重定向
				w, err := a.scanWord()
				if err != nil {
					return Facts{}, err
				}
				cur.Words = append(cur.Words, w)
				continue
			}
			r, err := a.scanRedirect()
			if err != nil {
				return Facts{}, err
			}
			cur.Redirects = append(cur.Redirects, r)
		case isDigit(c):
			j := a.i
			for j < len(a.s) && isDigit(a.s[j]) {
				j++
			}
			if j < len(a.s) && (a.s[j] == '<' || a.s[j] == '>') {
				r, err := a.scanRedirect()
				if err != nil {
					return Facts{}, err
				}
				cur.Redirects = append(cur.Redirects, r)
				continue
			}
			w, err := a.scanWord()
			if err != nil {
				return Facts{}, err
			}
			cur.Words = append(cur.Words, w)
		default:
			w, err := a.scanWord()
			if err != nil {
				return Facts{}, err
			}
			cur.Words = append(cur.Words, w)
		}
	}
	if len(cur.Words) > 0 || len(cur.Redirects) > 0 {
		flush("") // 行尾分隔符不产生尾随空段
	}
	return facts, nil
}

// analyzer 是 Analyze 的扫描游标。
type analyzer struct {
	s string
	i int
}

// scanRedirect 扫描 a.i 处的重定向运算符与目标。调用点保证 a.i 处是
// 运算符起点（词首数字、<、> 或 &>）。
func (a *analyzer) scanRedirect() (Redirect, error) {
	start := a.i
	amp := false
	if a.s[a.i] == '&' { // &> / &>>
		amp = true
		a.i++
	}
	for a.i < len(a.s) && isDigit(a.s[a.i]) {
		a.i++ // fd 前缀（2>、10>>）并入运算符原文
	}
	var core string
	switch {
	case strings.HasPrefix(a.s[a.i:], "<<<"):
		a.i += 3
		core = "<<<"
	case strings.HasPrefix(a.s[a.i:], "<<"):
		a.i += 2
		core = "<<"
		if a.i < len(a.s) && a.s[a.i] == '-' {
			a.i++ // <<-：heredoc 去行首 tab
			core = "<<-"
		}
	case strings.HasPrefix(a.s[a.i:], ">>"):
		a.i += 2
		core = ">>"
	case strings.HasPrefix(a.s[a.i:], "<>"):
		a.i += 2
		core = "<>"
	case a.i < len(a.s) && (a.s[a.i] == '<' || a.s[a.i] == '>'):
		core = string(a.s[a.i])
		a.i++
	default:
		return Redirect{}, ErrMissingRedirectTarget
	}
	op := a.s[start:a.i]
	write := strings.Contains(core, ">")

	// 运算符紧贴 & 的形态：fd 复制/关闭（>&1、<&-）或 bash 的 >&word
	// 双流等价。heredoc/herestring（<< 族）的 & 是目标的一部分，不走此径。
	if !amp && (core == "<" || core == ">" || core == ">>" || core == "<>") &&
		a.i < len(a.s) && a.s[a.i] == '&' {
		j := a.i + 1
		if j < len(a.s) && a.s[j] == '-' && (j+1 >= len(a.s) || !isWordChar(a.s[j+1])) {
			a.i = j + 1
			return Redirect{Op: op, Target: Word{Text: "&-"}, Write: write}, nil
		}
		k := j
		for k < len(a.s) && isDigit(a.s[k]) {
			k++
		}
		if k > j && (k >= len(a.s) || !isWordChar(a.s[k])) {
			a.i = k
			return Redirect{Op: op, Target: Word{Text: "&" + a.s[j:k]}, Write: write}, nil
		}
		if core == ">" || core == ">>" {
			op += "&" // bash：>&word = &>word，双流归写侧
			a.i = j
		} else {
			return Redirect{}, ErrMissingRedirectTarget // 读侧 <& 非 fd 目标判不了
		}
	}

	// 目标是紧随其后（允许空白分隔）的一个词。
	for a.i < len(a.s) && isSpace(a.s[a.i]) {
		a.i++
	}
	if a.i >= len(a.s) || isSepByte(a.s[a.i]) || a.s[a.i] == '<' || a.s[a.i] == '>' {
		return Redirect{}, ErrMissingRedirectTarget
	}
	w, err := a.scanWord()
	if err != nil {
		return Redirect{}, err
	}
	return Redirect{Op: op, Target: w, Write: write}, nil
}

// scanWord 扫描 a.i 处起的一个词（调用点保证 a.i 处是词起点），在空白、
// 分隔符与未被引号/转义/替换保护的 < > 处收束。
func (a *analyzer) scanWord() (Word, error) {
	var (
		b   strings.Builder
		dyn bool
	)
	for a.i < len(a.s) {
		c := a.s[a.i]
		switch {
		case isSpace(c) || isSepByte(c):
			return Word{Text: b.String(), Dynamic: dyn}, nil
		case c == '<' || c == '>':
			if strings.HasPrefix(a.s[a.i:], "<(") || strings.HasPrefix(a.s[a.i:], ">(") {
				end, err := a.scanParen(a.i + 1)
				if err != nil {
					return Word{}, err
				}
				b.WriteString(a.s[a.i:end]) // 替换体按原文保留
				a.i = end
				dyn = true
				continue
			}
			return Word{Text: b.String(), Dynamic: dyn}, nil
		case c == '\'': // 单引号内一切字面
			j := strings.IndexByte(a.s[a.i+1:], '\'')
			if j < 0 {
				return Word{}, ErrUnclosedQuote
			}
			b.WriteString(a.s[a.i+1 : a.i+1+j])
			a.i += j + 2
		case c == '"': // 双引号：转义规则与 Split 一致；展开标 dynamic
			a.i++
			closed := false
			for a.i < len(a.s) && !closed {
				switch ch := a.s[a.i]; {
				case ch == '\\':
					if a.i+1 >= len(a.s) {
						return Word{}, ErrTrailingEscape
					}
					switch next := a.s[a.i+1]; next {
					case '"', '\\', '$', '`':
						b.WriteByte(next)
					case '\n': // 行拼接：反斜杠+换行整体消失
					default:
						b.WriteByte('\\')
						b.WriteByte(next)
					}
					a.i += 2
				case ch == '"':
					a.i++
					closed = true
				case ch == '$' || ch == '`':
					end, d, err := a.scanExpansion(a.i)
					if err != nil {
						return Word{}, err
					}
					b.WriteString(a.s[a.i:end])
					a.i = end
					dyn = dyn || d
				default:
					b.WriteByte(ch)
					a.i++
				}
			}
			if !closed {
				return Word{}, ErrUnclosedQuote
			}
		case c == '\\': // 裸反斜杠：转义任意下一字符；行拼接同上
			if a.i+1 >= len(a.s) {
				return Word{}, ErrTrailingEscape
			}
			if a.s[a.i+1] != '\n' {
				b.WriteByte(a.s[a.i+1])
			}
			a.i += 2
		case c == '$' || c == '`':
			end, d, err := a.scanExpansion(a.i)
			if err != nil {
				return Word{}, err
			}
			b.WriteString(a.s[a.i:end])
			a.i = end
			dyn = dyn || d
		default:
			b.WriteByte(c)
			a.i++
		}
	}
	return Word{Text: b.String(), Dynamic: dyn}, nil
}

// scanExpansion 处理 p 处的 $ 或反引号开头的展开成分，返回其后下标与
// dynamic 标记。$ 后不构成展开形态的按字面（end=p+1、d=false）。
func (a *analyzer) scanExpansion(p int) (int, bool, error) {
	s := a.s
	if s[p] == '`' {
		end, err := a.scanBacktick(p)
		return end, true, err
	}
	var next byte
	if p+1 < len(s) {
		next = s[p+1]
	}
	switch {
	case next == '(':
		end, err := a.scanParen(p + 1)
		return end, true, err
	case next == '{':
		end, err := a.scanBrace(p + 1)
		return end, true, err
	case isNameStart(next):
		j := p + 1
		for j < len(s) && isNameChar(s[j]) {
			j++
		}
		return j, true, nil
	case isDigit(next) || strings.IndexByte("?*@$!#-", next) >= 0:
		return p + 2, true, nil // 特殊参数 $? $$ $! $@ $* $# $- 与位置参数
	default:
		return p + 1, false, nil // 字面 $
	}
}

// scanBacktick 从 p 处的反引号扫到配对反引号（反斜杠转义不闭合），返回
// 闭合符之后的下标。
func (a *analyzer) scanBacktick(p int) (int, error) {
	for i := p + 1; i < len(a.s); {
		switch a.s[i] {
		case '\\':
			i += 2
		case '`':
			return i + 1, nil
		default:
			i++
		}
	}
	return 0, ErrUnclosedSubstitution
}

// scanParen 从 p 处的 ( 扫到配对 )，返回 ) 之后的下标。引号段与反引号段
// 内的括号不计深（内层引号扫描保证边界）。
func (a *analyzer) scanParen(p int) (int, error) {
	depth := 0
	for i := p; i < len(a.s); {
		switch c := a.s[i]; c {
		case '\\':
			i += 2
		case '\'':
			j := strings.IndexByte(a.s[i+1:], '\'')
			if j < 0 {
				return 0, ErrUnclosedQuote
			}
			i += j + 2
		case '"':
			end, err := a.skipDoubleQuoted(i)
			if err != nil {
				return 0, err
			}
			i = end
		case '`':
			end, err := a.scanBacktick(i)
			if err != nil {
				return 0, err
			}
			i = end
		case '(':
			depth++
			i++
		case ')':
			depth--
			i++
			if depth == 0 {
				return i, nil
			}
		default:
			i++
		}
	}
	return 0, ErrUnclosedSubstitution
}

// scanBrace 从 p 处的 { 扫到配对 }，返回 } 之后的下标。
func (a *analyzer) scanBrace(p int) (int, error) {
	depth := 0
	for i := p; i < len(a.s); {
		switch c := a.s[i]; c {
		case '\\':
			i += 2
		case '\'':
			j := strings.IndexByte(a.s[i+1:], '\'')
			if j < 0 {
				return 0, ErrUnclosedQuote
			}
			i += j + 2
		case '"':
			end, err := a.skipDoubleQuoted(i)
			if err != nil {
				return 0, err
			}
			i = end
		case '`':
			end, err := a.scanBacktick(i)
			if err != nil {
				return 0, err
			}
			i = end
		case '{':
			depth++
			i++
		case '}':
			depth--
			i++
			if depth == 0 {
				return i, nil
			}
		default:
			i++
		}
	}
	return 0, ErrUnclosedSubstitution
}

// skipDoubleQuoted 跳过 p 处起的双引号段（含开引号），返回闭引号之后的
// 下标；未闭合报 ErrUnclosedQuote。
func (a *analyzer) skipDoubleQuoted(p int) (int, error) {
	for i := p + 1; i < len(a.s); {
		switch a.s[i] {
		case '\\':
			i += 2
		case '"':
			return i + 1, nil
		default:
			i++
		}
	}
	return 0, ErrUnclosedQuote
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNameChar(c byte) bool { return isNameStart(c) || isDigit(c) }

// isWordChar 判词字符（含 UTF-8 续字节——元字符全是 ASCII，≥0x80 的都算词
// 成分），用于 fd 复制目标的边界判定。
func isWordChar(c byte) bool { return isNameChar(c) || c >= 0x80 }

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func isSepByte(c byte) bool { return c == ';' || c == '|' || c == '&' }
