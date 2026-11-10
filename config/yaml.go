package config

// yaml 配置子集解析器（零第三方依赖）。支持的形态与边界见包文档——原则是
// 「不支持的结构显式报错，不做静默猜测」：配置装配错一半比 fail-fast 危险
// 得多。

import (
	"fmt"
	"strconv"
	"strings"
)

// parseYAML 把 yaml 文本解析为通用值：map[string]any / []any / string /
// bool / int64 / float64 / nil（空文档返回 nil）。语法错误逐行定位。
func parseYAML(src string) (any, error) {
	lines, err := scanYAMLLines(src)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, nil
	}
	p := &yamlParser{lines: lines}
	v, err := p.parseNode(lines[0].indent)
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		return nil, fmt.Errorf("config yaml: 第 %d 行无法解析（缩进或结构不符合子集）: %q", ln.num, ln.text)
	}
	return v, nil
}

// yamlLine 一条有内容的行：缩进列数 + 注释剥离后的文本 + 1 基行号（报错定位）。
type yamlLine struct {
	indent int
	text   string
	num    int
}

// scanYAMLLines 切行、剥注释、校验缩进与文档边界。空行与纯注释行被丢弃。
func scanYAMLLines(src string) ([]yamlLine, error) {
	raw := strings.Split(src, "\n")
	var out []yamlLine
	docStarted := false
	for i, rl := range raw {
		rl = strings.TrimRight(rl, "\r")
		indent := 0
		for indent < len(rl) && rl[indent] == ' ' {
			indent++
		}
		if indent < len(rl) && rl[indent] == '\t' {
			return nil, fmt.Errorf("config yaml: 第 %d 行使用 tab 缩进（仅允许空格）", i+1)
		}
		text := strings.TrimSpace(stripComment(strings.TrimRight(rl, " ")))
		if text == "" {
			continue
		}
		if text == "---" {
			// 文件头部单个分隔符容忍；出现第二段即多文档流，报错。
			if len(out) == 0 && !docStarted {
				docStarted = true
				continue
			}
			return nil, fmt.Errorf("config yaml: 第 %d 行遇到文档分隔符 ---（不支持多文档流）", i+1)
		}
		if text == "..." {
			return nil, fmt.Errorf("config yaml: 第 %d 行遇到文档结束符 ...（不支持多文档流）", i+1)
		}
		out = append(out, yamlLine{indent: indent, text: text, num: i + 1})
	}
	return out, nil
}

// stripComment 引号感知的行尾注释剥离：# 出现在引号外且行首或前置空白才算
// 注释起点（foo#bar 是含 # 的标量）。
func stripComment(s string) string {
	var b strings.Builder
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inSingle:
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					b.WriteString("''") // '' 是引号本身的转义，原样留给标量解析
					i++
					continue
				}
				inSingle = false
			}
			b.WriteByte(c)
		case inDouble:
			if c == '\\' && i+1 < len(s) {
				b.WriteByte(c)
				b.WriteByte(s[i+1])
				i++
				continue
			}
			if c == '"' {
				inDouble = false
			}
			b.WriteByte(c)
		case c == '\'':
			inSingle = true
			b.WriteByte(c)
		case c == '"':
			inDouble = true
			b.WriteByte(c)
		case c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return b.String()
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

type yamlParser struct {
	lines []yamlLine
	pos   int
}

func (p *yamlParser) cur() (yamlLine, bool) {
	if p.pos < len(p.lines) {
		return p.lines[p.pos], true
	}
	return yamlLine{}, false
}

// isSeqItem 判断行文本是否为块序列项（"-" 独占或 "- " 开头）。
func isSeqItem(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ")
}

func (p *yamlParser) parseNode(indent int) (any, error) {
	ln, ok := p.cur()
	if !ok {
		return nil, nil
	}
	if isSeqItem(ln.text) {
		return p.parseSequence(indent)
	}
	return p.parseMapping(indent)
}

// parseMapping 解析块映射。与父键同列的序列（key: 后紧跟同列 "- "）由
// 空值分支委托给 parseSequence——这是 yaml 允许的序列缩进形态。
func (p *yamlParser) parseMapping(indent int) (any, error) {
	m := map[string]any{}
	for {
		ln, ok := p.cur()
		if !ok || ln.indent < indent || isSeqItem(ln.text) {
			return m, nil
		}
		if ln.indent > indent {
			return nil, fmt.Errorf("config yaml: 第 %d 行缩进异常（期望 %d 列）: %q", ln.num, indent, ln.text)
		}
		key, rest, err := splitKey(ln.text, ln.num)
		if err != nil {
			return nil, err
		}
		if _, dup := m[key]; dup {
			return nil, fmt.Errorf("config yaml: 第 %d 行重复键 %q", ln.num, key)
		}
		p.pos++
		var val any
		if rest == "" {
			next, ok := p.cur()
			switch {
			case ok && next.indent > indent:
				val, err = p.parseNode(next.indent)
			case ok && next.indent == indent && isSeqItem(next.text):
				val, err = p.parseSequence(indent)
			default:
				val = nil // key: 空值且无更深内容 → null
			}
		} else {
			val, err = parseValue(rest, ln.num)
		}
		if err != nil {
			return nil, err
		}
		m[key] = val
	}
}

// parseSequence 解析块序列。映射项（"- key: value"）用合成行技巧归一：
// 把首键改写到 dash+2 列，后续键按块映射继续解析。
func (p *yamlParser) parseSequence(indent int) (any, error) {
	s := []any{}
	for {
		ln, ok := p.cur()
		if !ok || ln.indent < indent {
			return s, nil
		}
		if ln.indent > indent {
			return nil, fmt.Errorf("config yaml: 第 %d 行缩进异常（期望 %d 列）: %q", ln.num, indent, ln.text)
		}
		if !isSeqItem(ln.text) {
			return s, nil
		}
		rest := strings.TrimSpace(strings.TrimPrefix(ln.text, "-"))
		p.pos++
		var (
			item any
			err  error
		)
		switch {
		case rest == "":
			next, ok := p.cur()
			if ok && next.indent > indent {
				item, err = p.parseNode(next.indent)
			} else {
				item = nil
			}
		case isSeqItem(rest):
			// 内联嵌套序列：- - a。改写当前行为子序列项后继续。
			p.pos--
			p.lines[p.pos] = yamlLine{indent: indent + 2, text: rest, num: ln.num}
			item, err = p.parseSequence(indent + 2)
		default:
			if _, _, kerr := splitKey(rest, ln.num); kerr == nil {
				// 映射项：首键改写到 dash+2 列，同项后续键与之对齐。
				p.pos--
				p.lines[p.pos] = yamlLine{indent: indent + 2, text: rest, num: ln.num}
				item, err = p.parseMapping(indent + 2)
			} else {
				item, err = parseValue(rest, ln.num)
			}
		}
		if err != nil {
			return nil, err
		}
		s = append(s, item)
	}
}

// splitKey 拆出「键: 值」。键支持引号形态；裸键以「: 空白或行尾」为分隔
// （值里含冒号的场景引号包起来）。不是键值行时返回错误（parseSequence 用
// 该错误区分「映射项」与「标量项」）。
func splitKey(text string, num int) (key, rest string, err error) {
	if text[0] == '"' || text[0] == '\'' {
		key, rest, err = parseQuoted(text, num)
		if err != nil {
			return "", "", err
		}
		rest = strings.TrimLeft(rest, " ")
		if !strings.HasPrefix(rest, ":") {
			return "", "", fmt.Errorf("config yaml: 第 %d 行引号键后缺少冒号: %q", num, text)
		}
		return key, strings.TrimSpace(rest[1:]), nil
	}
	for i := 0; i < len(text); i++ {
		if text[i] == ':' && (i+1 == len(text) || text[i+1] == ' ' || text[i+1] == '\t') {
			return strings.TrimSpace(text[:i]), strings.TrimSpace(text[i+1:]), nil
		}
	}
	return "", "", fmt.Errorf("config yaml: 第 %d 行不是「键: 值」形态: %q", num, text)
}

// parseQuoted 解析行首引号标量，返回（值, 余下文本）。单引号 ” 转义自身；
// 双引号支持 \n \t \r \\ \" \0 \uXXXX。
func parseQuoted(text string, num int) (string, string, error) {
	q := text[0]
	var b strings.Builder
	for i := 1; i < len(text); i++ {
		c := text[i]
		if q == '\'' {
			if c == '\'' {
				if i+1 < len(text) && text[i+1] == '\'' {
					b.WriteByte('\'')
					i++
					continue
				}
				return b.String(), text[i+1:], nil
			}
			b.WriteByte(c)
			continue
		}
		if c == '\\' {
			if i+1 >= len(text) {
				break
			}
			i++
			switch text[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '0':
				b.WriteByte(0)
			case '"', '\\':
				b.WriteByte(text[i])
			case 'u':
				if i+4 >= len(text) {
					return "", "", fmt.Errorf("config yaml: 第 %d 行 \\u 转义不完整: %q", num, text)
				}
				r, perr := strconv.ParseUint(text[i+1:i+5], 16, 32)
				if perr != nil {
					return "", "", fmt.Errorf("config yaml: 第 %d 行非法 \\u 转义: %q", num, text)
				}
				b.WriteRune(rune(r))
				i += 4
			default:
				return "", "", fmt.Errorf("config yaml: 第 %d 行不支持的转义 \\%c", num, text[i])
			}
			continue
		}
		if c == '"' {
			return b.String(), text[i+1:], nil
		}
		b.WriteByte(c)
	}
	return "", "", fmt.Errorf("config yaml: 第 %d 行引号未闭合: %q", num, text)
}

// parseValue 解析单行标量值；子集不支持的结构显式报错。
func parseValue(text string, num int) (any, error) {
	switch text[0] {
	case '"', '\'':
		s, rest, err := parseQuoted(text, num)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(rest) != "" {
			return nil, fmt.Errorf("config yaml: 第 %d 行引号标量后有多余内容: %q", num, text)
		}
		return s, nil
	case '[', '{':
		return nil, fmt.Errorf("config yaml: 第 %d 行 flow 风格（%c…）不支持，请改用块风格", num, text[0])
	case '&':
		return nil, fmt.Errorf("config yaml: 第 %d 行锚点（&）不支持", num)
	case '*':
		return nil, fmt.Errorf("config yaml: 第 %d 行别名（*）不支持", num)
	case '!':
		return nil, fmt.Errorf("config yaml: 第 %d 行显式标签（!）不支持", num)
	case '|', '>':
		return nil, fmt.Errorf("config yaml: 第 %d 行块标量（%c）不支持，长文本建议放环境变量", num, text[0])
	}
	return inferScalar(text), nil
}

// inferScalar 标量类型推断：null/~ → nil；true/false（三档大小写）→ bool；
// 十进制整数 → int64；浮点 → float64；其余按字符串。仅当首字符形如数字时
// 才尝试数值解析——避免把 "inf"/"NaN" 等误判成浮点。
func inferScalar(text string) any {
	switch text {
	case "null", "Null", "NULL", "~":
		return nil
	case "true", "True", "TRUE":
		return true
	case "false", "False", "FALSE":
		return false
	}
	if c := text[0]; c == '+' || c == '-' || c == '.' || (c >= '0' && c <= '9') {
		if strings.Contains(text, "_") {
			// ParseFloat 按 Go 字面量规则会吃掉下划线，但 YAML 1.2 core schema
			// 不认 1_000——按文档边界保持字符串，不做 Go 特有扩展。
			return text
		}
		if n, err := strconv.ParseInt(text, 10, 64); err == nil {
			return n
		}
		if f, err := strconv.ParseFloat(text, 64); err == nil {
			return f
		}
	}
	return text
}
