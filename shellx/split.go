package shellx

import (
	"errors"
	"strings"
)

var (
	// ErrUnclosedQuote 表示命令行里有未闭合的引号（'abc 或 "abc）。
	// Split 不做尽力解析：尽力解析会把两段判成一段，前缀判据因此被绕过。
	ErrUnclosedQuote = errors.New("shellx: unclosed quote in command line")
	// ErrTrailingEscape 表示命令行以悬挂反斜杠结尾（abc\）。
	ErrTrailingEscape = errors.New("shellx: trailing escape in command line")
)

// Split 把一条命令行拆成 shell 词（argv），是 Quote/QuoteArgv 的逆运算：
//
//	Split(QuoteArgv(args...)) 必须逐项还原 args（含空参数、含引号/空格/
//	$ 等特殊字符——这条往返判据由用例钉住，Quote 的口径一改 Split 必须跟）。
//
// 口径（POSIX shell 词法，只拆词、不展开）：
//   - 单引号内一切原样（含反斜杠）——与 Quote 的单引号转义互逆；
//   - 双引号内反斜杠仅在后跟 " \ $ ` 或换行时是转义（POSIX：\<char> 出该
//     char，\<newline> 是行拼接、整体消失），其余情况（\x 非 escape 时）
//     反斜杠与字符一并字面保留；
//   - 未加引号的 ; | & 作为**独立 token** 返回，相邻同类合并成 && / ||——
//     分隔符不与词粘连、也不丢弃：是否按「多条子命令」处理是判据侧的
//     严格度，不由语法层决定（#243）；
//   - 未闭合引号、尾随转义 → 返回 error，不做尽力解析；
//   - 空参数保留：” 还原为空参数——丢弃它会让后续参数整体错位
//     （Quote("") = "”" 的逆）；
//   - 词内引号段与裸段粘连成一个词（a'b'c → abc，与 shell 一致）；
//   - 不做变量展开、命令替换、glob——与 Quote 的「只转义、不判策略」同一
//     立场：展开会让判定看到与执行时点不同的文本。
//
// 空白分隔符为空格/制表/换行/回车（CR 按空白处理：从配置/工单粘贴的 CRLF
// 命令行，行尾 \r 不粘进最后一个 token）。空串与纯空白输入返回空切片。
func Split(s string) ([]string, error) {
	var (
		tokens  []string
		cur     strings.Builder
		pending bool // 当前词已开始（裸字符或任一引号段即置位）——空参数保留的关键
	)
	flush := func() {
		if pending {
			tokens = append(tokens, cur.String())
			cur.Reset()
			pending = false
		}
	}

	for i := 0; i < len(s); {
		switch c := s[i]; c {
		case ' ', '\t', '\n', '\r':
			flush()
			i++
		case '\'': // 单引号内一切原样（含反斜杠）
			pending = true
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, ErrUnclosedQuote
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += 1 + j + 1
		case '"': // 双引号：反斜杠仅转义 " \ $ ` 与换行
			pending = true
			i++
			closed := false
			for i < len(s) {
				ch := s[i]
				if ch == '\\' && i+1 < len(s) {
					switch next := s[i+1]; next {
					case '"', '\\', '$', '`':
						cur.WriteByte(next)
					case '\n': // 行拼接：反斜杠+换行整体消失
					default: // \x 非 escape：反斜杠字面保留
						cur.WriteByte('\\')
						cur.WriteByte(next)
					}
					i += 2
					continue
				}
				if ch == '"' {
					i++
					closed = true
					break
				}
				cur.WriteByte(ch)
				i++
			}
			if !closed {
				return nil, ErrUnclosedQuote
			}
		case '\\': // 裸反斜杠：转义任意下一字符；行拼接同上
			if i+1 >= len(s) {
				return nil, ErrTrailingEscape
			}
			if s[i+1] != '\n' {
				cur.WriteByte(s[i+1])
				pending = true
			}
			i += 2
		case ';', '|', '&': // 独立 token，相邻同类合并
			flush()
			tok := string(c)
			if c != ';' && i+1 < len(s) && s[i+1] == c {
				tok += string(c) // && / ||
				i++
			}
			tokens = append(tokens, tok)
			i++
		default:
			cur.WriteByte(c)
			pending = true
			i++
		}
	}
	flush()
	return tokens, nil
}
