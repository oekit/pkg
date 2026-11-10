package shellx

import (
	"errors"
	"strconv"
	"strings"
)

// 词法事实面的哨兵错误。判定纪律与 [ErrUnclosedQuote] 同口径：判不了就报错、
// 不尽力解析——尽力解析会把「没有重定向」的假事实交给判据，比不给事实更坏。
var (
	// ErrMissingRedirectTarget 表示重定向运算符后面没有目标词
	// （`echo a >`、`echo a > ;`）——与 bash 的语法错误同款，判据侧按拒绝处理。
	ErrMissingRedirectTarget = errors.New("shellx: redirect operator without target")
	// ErrBadRedirect 表示重定向的 fd 数字不是合法整数（位数过长等）。
	// 判不了就报错：宁可让判据拒绝，也不产出「fd 没看清」的半截事实。
	ErrBadRedirect = errors.New("shellx: bad file descriptor in redirect")
	// ErrUnclosedSubstitution 表示替换单元没有闭合（`$(rm -rf /`、
	// `` `whoami ``、`<(cmd`、`${VAR`）。替换内容是执行时点的代码，闭合
	// 之前连「一个词在哪结束」都判不出来。
	ErrUnclosedSubstitution = errors.New("shellx: unclosed substitution in command line")
)

// Word 是一个 shell 词的词法事实。
type Word struct {
	// Text 是词文本：词自身的引号按 [Split] 同口径剥离粘连
	// （Split(QuoteArgv(texts...)) 往返判据对 Text 成立）；命令替换/进程
	// 替换/变量替换的**原文**（含 $() / 反引号定界符）逐字保留——那是执行
	// 时点会被替换的代码，剥壳即失真。
	Text string
	// Dynamic 标记「执行时点文本可被替换」：词内含裸露或双引号内的 $( )、
	// 反引号、<( )、>( )、$VAR 时置位；单引号内一切字面（含 '$(rm -rf /)'）
	// 不算。注意 Text 相同的两个词 Dynamic 可以不同（$(rm -rf /) 裸露与
	// '$(rm -rf /)' 的 Text 相同、判据结论相反）——安全判定必须看本字段，
	// 不能只看 Text。
	Dynamic bool
}

// Redirect 是一条重定向的词法事实（词法层即可判定，不需要 AST）。
type Redirect struct {
	// Op 是运算符形态（不含 fd 数字）：读侧 "<"、"<<"、"<<-"、"<<<"、"<&"；
	// 写侧 ">"、">>"、">|"、">&"、"&>"、"&>>"。显式 fd 见 Fd。
	Op string
	// Fd 是紧邻运算符的显式 fd 数字（`2>` ⇒ 2，数字必须与运算符粘连且
	// 全为裸数字——`2 >x` 的 2 是普通参数，与 bash 的 IO_NUMBER 同口径）；
	// 未显式为 -1。
	Fd int
	// Write 区分读写两侧：写侧（> 族，含 fd 复制 >&）为 true；读侧（< 族，
	// 含 here-doc/here-string）为 false。「有没有写重定向」看它——
	// `git status > /etc/passwd` 与 `git status >/etc/passwd` 都是写侧。
	Write bool
	// Target 是目标词（文件、here-string 内容、fd 号、"-"）。目标是否字面
	// 看 Target.Dynamic：`cat < $(rm -rf build)` 的**读侧**目标含动态同样
	// 危险，只看 op 会放行它。
	Target Word
}

// Segment 是按控制符切出的一段命令——前缀判据「逐段取最严」的单位。
type Segment struct {
	// Op 是结束本段的控制符：""（行尾）、";"、"&&"、"||"、"|"、"&"。
	Op string
	// Words 是段内普通参数（argv 语义）：**不含**重定向运算符与目标词。
	// 把目标并回 argv 正是「git status > /etc/passwd」绕过前缀判据的根因：
	// 规则只看 argv 前缀，重定向以普通 token 身份排在其后就整类放行了写文件。
	Words []Word
	// Redirects 是段内重定向，按出现顺序。
	Redirects []Redirect
}

// Texts 返回段内普通参数的文本序列（前缀判据比对、QuoteArgv 拼装用）。
// 对它成立的往返判据：Split(QuoteArgv(seg.Texts()...)) == seg.Texts()。
func (s *Segment) Texts() []string {
	out := make([]string, len(s.Words))
	for i, w := range s.Words {
		out[i] = w.Text
	}
	return out
}

// Analysis 是一条命令行的词法事实面（tokenize 结果）：词、重定向、分段
// 三类事实一次给全，安全判定面不用再各自扫字符串。
type Analysis struct {
	// Segments 按未引号的 `;` `&&` `||` `|` `&` 机械切分：控制符不与词
	// 粘连、空段保留（首/尾控制符与空段都是事实的一部分，不清理）；
	// 纯空白输入没有段。
	Segments []Segment
}

// HasRedirect 报告命令行里有没有重定向（任一段、任一侧）。
func (a *Analysis) HasRedirect() bool {
	for _, seg := range a.Segments {
		if len(seg.Redirects) > 0 {
			return true
		}
	}
	return false
}

// HasDynamic 报告命令行里有没有动态词——普通参数或重定向目标里任一词含
// 替换单元即置位（`echo $(rm -rf /)` 与 `cat < $(cmd)` 都命中）。
func (a *Analysis) HasDynamic() bool {
	for _, seg := range a.Segments {
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

// Analyze 把一条命令行拆成词法事实——[Split] 的加强面：在拆词之上区分
// 「词」与「重定向运算符+目标」、给动态词打标记、按控制符分段。给安全
// 判定面（前缀规则、白名单）当事实源：
//
//	a, _ := shellx.Analyze("git status > /etc/passwd")
//	a.Segments[0].Texts()        // ["git", "status"] —— argv 不含目标
//	a.Segments[0].Redirects[0]   // {Op: ">", Write: true, Target: "/etc/passwd"}
//
// 口径（POSIX shell 词法，只切词、不展开、不判策略）：
//   - Split 的引号/转义/空参数/控制符口径全部沿用；ErrUnclosedQuote、
//     ErrTrailingEscape 语义不动；
//   - 未引号的 `<` `>` 是重定向运算符（`<(`/`>(` 除外——进程替换是词的
//     构成部分）：`<` `<<` `<<-` `<<<` `<&` `>` `>>` `>|` `>&` `&>` `&>>`
//     十一种形态都识别；紧邻的裸数字串是显式 fd（`2>` ⇒ Fd 2）；
//   - 重定向目标是一个词（可引号、可替换）；运算符后没有目标词报
//     ErrMissingRedirectTarget，不猜；
//   - 动态词标记：词内含裸露或双引号内的 $( )、反引号、<( )、>( )、
//     $VAR 即 Dynamic（单引号内一切字面不算）；替换单元按括号/引号配对
//     整体吞掉（bash 词法：`$(rm -rf /)` 是**一个**词，内部空格不切词），
//     闭合不了报 ErrUnclosedSubstitution，不做尽力解析；
//   - 替换单元的内部命令不递归分析：`echo $(a > b)` 报的是「一个动态词」，
//     内部的 `>` 属于被替换的那段代码——要判它就对替换内容再调一次
//     Analyze（判据自己决定递归深度）。
//
// 与 Split 的分工：Split 是 Quote/QuoteArgv 的逆运算（纯拆词，`>` 是普通
// 词字符）；Analyze 是给判据的事实面（`>` 是运算符）——两者对 `a>b` 的
// 切分不同是刻意的，往返判据只约束 Split。边界（如实声明）：不处理
// here-doc 正文（多行正文按普通词处理，判多行命令请按行切开）、不处理
// `$'...'` ANSI-C 引用（按字面 `$` 处理）、不展开 `~` 与 glob。
func Analyze(s string) (*Analysis, error) {
	a := &analyzer{s: s}
	res := &Analysis{}
	var seg Segment
	segTouched := false // 当前段有词/重定向
	var pending *wordScan

	flush := func() {
		// started=false 的「词」（如仅由 \+换行的行拼接构成）不落位——
		// 空词只在显式引号（'' / ""）时保留，与 Split 的空参数口径一致。
		if pending != nil && pending.started {
			seg.Words = append(seg.Words, pending.Word)
		}
		pending = nil
	}
	endSeg := func(op string) {
		flush()
		seg.Op = op
		res.Segments = append(res.Segments, seg)
		seg = Segment{}
		segTouched = false
	}

	for a.i < len(a.s) {
		c := a.s[a.i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
			a.i++
		case c == ';':
			endSeg(";")
			a.i++
		case c == '|':
			endSeg(a.controlPair('|'))
		case c == '&':
			if strings.HasPrefix(a.s[a.i:], "&&") {
				endSeg("&&")
				a.i += 2
				continue
			}
			if strings.HasPrefix(a.s[a.i:], "&>>") || strings.HasPrefix(a.s[a.i:], "&>") {
				// &> / &>>：写侧双流重定向。前面的数字**不是** fd 前缀
				// （bash 口径：2&>f 的 2 是普通参数）。
				op := "&>"
				n := 2
				if a.s[a.i+1] == '>' && a.i+2 < len(a.s) && a.s[a.i+2] == '>' {
					op = "&>>"
					n = 3
				}
				a.i += n
				flush()
				if err := a.redirect(&seg, op, -1, true); err != nil {
					return nil, err
				}
				segTouched = true
				continue
			}
			endSeg("&")
			a.i++
		case c == '<' || c == '>':
			if a.i+1 < len(a.s) && a.s[a.i+1] == '(' {
				// 进程替换 <( ) / >( )：词的构成部分，走词扫描
				w, err := a.scanWord()
				if err != nil {
					return nil, err
				}
				pending = &w
				segTouched = true
				continue
			}
			fd := -1
			if pending != nil {
				if pending.digits {
					v, err := strconv.Atoi(pending.Text)
					if err != nil {
						return nil, ErrBadRedirect
					}
					fd = v
				} else {
					flush()
				}
				pending = nil
			}
			op, n := a.redirectOp()
			a.i += n
			if err := a.redirect(&seg, op, fd, c == '>'); err != nil {
				return nil, err
			}
			segTouched = true
		default:
			w, err := a.scanWord()
			if err != nil {
				return nil, err
			}
			pending = &w
			segTouched = true
		}
	}
	flush()
	if segTouched {
		seg.Op = ""
		res.Segments = append(res.Segments, seg)
	}
	return res, nil
}

// redirect 扫描重定向目标并登记一条重定向事实。目标缺失报
// ErrMissingRedirectTarget——不猜、不把后面的控制符/运算符当目标。
func (a *analyzer) redirect(seg *Segment, op string, fd int, write bool) error {
	// 目标前只跳空格/制表/回车：换行不能当目标前的空白——bash 里
	// `>\nfoo` 是语法错误（NEWLINE 是语句分隔符），判据按拒绝处理。
	for a.i < len(a.s) && (a.s[a.i] == ' ' || a.s[a.i] == '\t' || a.s[a.i] == '\r') {
		a.i++
	}
	if a.i >= len(a.s) {
		return ErrMissingRedirectTarget
	}
	switch a.s[a.i] {
	case '\n', ';', '|', '&', '<', '>':
		return ErrMissingRedirectTarget
	}
	w, err := a.scanWord()
	if err != nil {
		return err
	}
	if !w.started {
		return ErrMissingRedirectTarget
	}
	seg.Redirects = append(seg.Redirects, Redirect{
		Op:     op,
		Fd:     fd,
		Write:  write,
		Target: w.Word,
	})
	return nil
}

// redirectOp 归一当前位置的重定向运算符形态，返回形态与消耗字节数。
// 前置条件：a.i 指向裸 `<` 或 `>`（`<(`/`>(` 已在调用方排除）。
func (a *analyzer) redirectOp() (op string, n int) {
	c := a.s[a.i]
	if c == '>' {
		switch {
		case strings.HasPrefix(a.s[a.i:], ">>"):
			return ">>", 2
		case strings.HasPrefix(a.s[a.i:], ">|"):
			return ">|", 2
		case strings.HasPrefix(a.s[a.i:], ">&"):
			return ">&", 2
		default:
			return ">", 1
		}
	}
	switch {
	case strings.HasPrefix(a.s[a.i:], "<<<"):
		return "<<<", 3
	case strings.HasPrefix(a.s[a.i:], "<<-"):
		return "<<-", 3
	case strings.HasPrefix(a.s[a.i:], "<<"):
		return "<<", 2
	case strings.HasPrefix(a.s[a.i:], "<&"):
		return "<&", 2
	default:
		return "<", 1
	}
}

// controlPair 消费一个同类双字符控制符（&& / ||）或单字符控制符，
// 返回控制符文本。前置条件：a.i 指向 `|`。
func (a *analyzer) controlPair(c byte) string {
	if a.i+1 < len(a.s) && a.s[a.i+1] == c {
		a.i += 2
		return string([]byte{c, c})
	}
	a.i++
	return string(c)
}

// wordScan 是 scanWord 的产出：词文本/动态标记/是否全是裸数字（fd 候选）
// 以及 started（确实扫到了词——空引号 `”` 也是词，但词首就是结束符不是）。
type wordScan struct {
	Word
	digits  bool
	started bool
}

// analyzer 是 Analyze 的扫描器：s 是命令行原文，i 是当前扫描位置。
type analyzer struct {
	s string
	i int
}

// scanWord 从当前位置扫描一个 shell 词：引号段按 Split 口径剥离粘连，
// 替换单元原文逐字并入并置 Dynamic。词在裸上下文遇到空白、`;` `|` `&`
// 或重定向运算符（`<`/`>` 且不构成 `<(`/`>(`）时结束——结束符留给主循环
// 处理，不消耗。started=false 表示当前位置不是词的开头。
func (a *analyzer) scanWord() (res wordScan, err error) {
	res.digits = true
	var sb strings.Builder
	for a.i < len(a.s) {
		c := a.s[a.i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ';' || c == '|' || c == '&':
			res.Text = sb.String()
			return res, nil
		case c == '<' || c == '>':
			if a.i+1 < len(a.s) && a.s[a.i+1] == '(' {
				v, err := a.substParen()
				if err != nil {
					return res, err
				}
				sb.WriteString(v)
				res.Dynamic = true
				res.digits = false
				res.started = true
				continue
			}
			res.Text = sb.String()
			return res, nil
		case c == '\'':
			// 单引号内一切原样（含反斜杠、$、反引号）——不置 Dynamic
			a.i++
			j := strings.IndexByte(a.s[a.i:], '\'')
			if j < 0 {
				return res, ErrUnclosedQuote
			}
			sb.WriteString(a.s[a.i : a.i+j])
			a.i += j + 1
			res.digits = false
			res.started = true
		case c == '"':
			a.i++
			if err := a.scanDoubleQuote(&sb, &res.Dynamic); err != nil {
				return res, err
			}
			res.digits = false
			res.started = true
		case c == '\\':
			// 裸反斜杠：转义任意下一字符；\+换行是行拼接（整体消失）
			if a.i+1 >= len(a.s) {
				return res, ErrTrailingEscape
			}
			if a.s[a.i+1] != '\n' {
				sb.WriteByte(a.s[a.i+1])
				res.started = true
				res.digits = false
			}
			a.i += 2
		case c == '`':
			v, err := a.substBacktick()
			if err != nil {
				return res, err
			}
			sb.WriteString(v)
			res.Dynamic = true
			res.digits = false
			res.started = true
		case c == '$':
			next := byte(0)
			if a.i+1 < len(a.s) {
				next = a.s[a.i+1]
			}
			switch {
			case next == '(':
				v, err := a.substParen()
				if err != nil {
					return res, err
				}
				sb.WriteString(v)
				res.Dynamic = true
				res.digits = false
				res.started = true
			case next == '{':
				v, err := a.substBrace()
				if err != nil {
					return res, err
				}
				sb.WriteString(v)
				res.Dynamic = true
				res.digits = false
				res.started = true
			case isDollarTrigger(next):
				// $VAR / $? / $$ ……：替换触发但不是成对定界，无需吞段
				sb.WriteByte('$')
				a.i++
				res.Dynamic = true
				res.digits = false
				res.started = true
			default:
				// 孤立 $（后跟空白/斜杠/引号/行尾）：字面量
				sb.WriteByte('$')
				a.i++
				res.digits = false
				res.started = true
			}
		default:
			sb.WriteByte(c)
			a.i++
			res.started = true
			if c < '0' || c > '9' {
				res.digits = false
			}
		}
	}
	res.Text = sb.String()
	return res, nil
}

// scanDoubleQuote 扫描双引号段（不含开闭引号）并把内容并入 sb：
// 反斜杠转义表与 [Split] 同口径（仅 " \ $ ` 与换行）；未转义的 $、反引号
// 构成替换触发——替换单元原文并入并在 *dynamic 置位（转义掉的 \$、\` 是
// 字面量，不置位）。未闭合报 ErrUnclosedQuote。
func (a *analyzer) scanDoubleQuote(sb *strings.Builder, dynamic *bool) error {
	for a.i < len(a.s) {
		c := a.s[a.i]
		switch {
		case c == '\\' && a.i+1 < len(a.s):
			switch next := a.s[a.i+1]; next {
			case '"', '\\', '$', '`':
				sb.WriteByte(next) // 转义后的字符是字面量，不触发替换
			case '\n': // 行拼接：反斜杠+换行整体消失
			default:
				sb.WriteByte('\\')
				sb.WriteByte(next)
			}
			a.i += 2
		case c == '"':
			a.i++
			return nil
		case c == '$':
			next := byte(0)
			if a.i+1 < len(a.s) {
				next = a.s[a.i+1]
			}
			switch {
			case next == '(':
				v, err := a.substParen()
				if err != nil {
					return err
				}
				sb.WriteString(v)
				*dynamic = true
			case next == '{':
				v, err := a.substBrace()
				if err != nil {
					return err
				}
				sb.WriteString(v)
				*dynamic = true
			case isDollarTrigger(next):
				sb.WriteByte('$')
				a.i++
				*dynamic = true
			default:
				sb.WriteByte('$')
				a.i++
			}
		case c == '`':
			v, err := a.substBacktick()
			if err != nil {
				return err
			}
			sb.WriteString(v)
			*dynamic = true
		default:
			sb.WriteByte(c)
			a.i++
		}
	}
	return ErrUnclosedQuote
}

// isDollarTrigger 判 `$` 后面的字符是否构成变量/参数替换触发。
// 不在触发集里的（空白、/、=、引号、行尾……）是字面 `$`。
func isDollarTrigger(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '_':
		return true
	}
	return strings.IndexByte("?#@$*!-", c) >= 0
}

// substParen 吞掉一个括号配对的替换单元（$( ) / <( ) / >( )）并返回原文
// （含定界符）。括号配对按 bash 词法：引号内的括号不计数，反斜杠转义跳过。
// 任何闭合失败（含内部引号未闭合）都以 ErrUnclosedSubstitution 上抛——
// 对调用方而言「替换单元没闭合」就是唯一需要知道的失败形态。
func (a *analyzer) substParen() (string, error) {
	start := a.i
	a.i += 2 // 跳过 $( / <( / >(
	depth := 1
	for a.i < len(a.s) {
		c := a.s[a.i]
		switch c {
		case '\\':
			if a.i+1 >= len(a.s) {
				return "", ErrUnclosedSubstitution
			}
			a.i += 2
		case '\'', '"', '`':
			if err := a.skipQuote(c); err != nil {
				return "", ErrUnclosedSubstitution
			}
		case '(':
			depth++
			a.i++
		case ')':
			depth--
			a.i++
			if depth == 0 {
				return a.s[start:a.i], nil
			}
		default:
			a.i++
		}
	}
	return "", ErrUnclosedSubstitution
}

// substBrace 吞掉 ${...}（按 ${ 配对 }，内部 $()/反引号/引号整段跳过）。
// 闭合失败一律 ErrUnclosedSubstitution（同 substParen）。
func (a *analyzer) substBrace() (string, error) {
	start := a.i
	a.i += 2 // 跳过 ${
	depth := 1
	for a.i < len(a.s) {
		c := a.s[a.i]
		switch c {
		case '\\':
			if a.i+1 >= len(a.s) {
				return "", ErrUnclosedSubstitution
			}
			a.i += 2
		case '\'', '"', '`':
			if err := a.skipQuote(c); err != nil {
				return "", ErrUnclosedSubstitution
			}
		case '$':
			if strings.HasPrefix(a.s[a.i:], "${") {
				depth++
				a.i += 2
				continue
			}
			if strings.HasPrefix(a.s[a.i:], "$(") {
				if _, err := a.substParen(); err != nil {
					return "", err
				}
				continue
			}
			a.i++
		case '}':
			depth--
			a.i++
			if depth == 0 {
				return a.s[start:a.i], nil
			}
		default:
			a.i++
		}
	}
	return "", ErrUnclosedSubstitution
}

// substBacktick 吞掉 `...` 并返回原文（含反引号）。反斜杠转义跳过；
// 闭合不了报 ErrUnclosedSubstitution。
func (a *analyzer) substBacktick() (string, error) {
	start := a.i
	a.i++ // 跳过开引号
	for a.i < len(a.s) {
		switch a.s[a.i] {
		case '\\':
			if a.i+1 >= len(a.s) {
				return "", ErrUnclosedSubstitution
			}
			a.i += 2
		case '`':
			a.i++
			return a.s[start:a.i], nil
		default:
			a.i++
		}
	}
	return "", ErrUnclosedSubstitution
}

// skipQuote 只跳过一个引号段（替换单元配对用），不提取文本。
// c 是引号字符：单引号/双引号/反引号。
func (a *analyzer) skipQuote(c byte) error {
	switch c {
	case '\'':
		j := strings.IndexByte(a.s[a.i+1:], '\'')
		if j < 0 {
			return ErrUnclosedQuote
		}
		a.i += 1 + j + 1
		return nil
	case '`':
		_, err := a.substBacktick()
		return err
	}
	a.i++ // 开引号（双引号）
	for a.i < len(a.s) {
		switch {
		case a.s[a.i] == '\\' && a.i+1 < len(a.s):
			a.i += 2
		case a.s[a.i] == '"':
			a.i++
			return nil
		default:
			a.i++
		}
	}
	return ErrUnclosedQuote
}
