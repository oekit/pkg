package jsonrepair

import (
	"encoding/json"
	"fmt"
	"strings"
)

// UnmarshalOption 是 UnmarshalWithOpts 的选项类型
// （AGENTS「XxxOption 禁止裸 Option」规约，多包 co-import 不冲突）。
type UnmarshalOption func(*unmarshalOpts)

type unmarshalOpts struct {
	allowPartial bool
}

// WithAllowPartialArray 宽松模式：尽力闭合截断的数组/对象——补悬空字符串
// 引号（含 EOF 悬挂转义）、丢 EOF 悬空逗号、悬空冒号补 null、按栈序闭合
// 未闭括号。截断前缀能闭合出合法 JSON 即解析成功：「宁可少显示，不能显示
// 一半」的取舍由调用方做出。纯垃圾（无法闭合出合法 JSON）仍然报错，
// 不编造数据。
func WithAllowPartialArray() UnmarshalOption {
	return func(o *unmarshalOpts) { o.allowPartial = true }
}

// UnmarshalLenient 宽容解析：在 ParseLenient 同款修复链之外，尽力闭合截断
// 的数组/对象。partial=true 表示原文不完整、结果来自闭合后的截断前缀
// （内容可能缺失，如闭合的半截字符串/补位的 null）；false 表示完整解析。
// 纯垃圾（登录过期提示、HTML 错误页等）仍返回 err。
//
// 与 ParseLenient 的分工：需要 Schema 归一走 ParseLenient；需要感知截断
// （界面区分「解析失败」与「数据不全」）走本函数。
func UnmarshalLenient(text string, v any) (partial bool, err error) {
	return unmarshal(text, v, unmarshalOpts{allowPartial: true})
}

// UnmarshalWithOpts 带选项的宽容解析。无选项时与修复链保守语义一致
// （截断报错，不闭合——结果要么完整要么失败），传 WithAllowPartialArray
// 开启宽松模式。
func UnmarshalWithOpts(text string, v any, opts ...UnmarshalOption) error {
	var o unmarshalOpts
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	_, err := unmarshal(text, v, o)
	return err
}

// unmarshal 的保守面直接复用既有 Unmarshal 三级链（默认语义零漂移，
// 错误信息同源：严格/宽容双路包装）；allowPartial 时对截断候选追加
// closePartial 收尾。schema 归一面走 ParseLenient（Normalize 与截断
// 闭合正交，此处不重复承载）。
func unmarshal(text string, v any, o unmarshalOpts) (bool, error) {
	err := Unmarshal(text, v)
	if err == nil {
		return false, nil
	}
	if !o.allowPartial {
		return false, err
	}
	// 宽松收尾：仅当 EOF 处确有截断迹象才闭合（闭合一个均衡文本只会掩盖
	// 中段损坏，违背「修复器的错误成功比失败更贵」纪律）。
	// 候选三件与抽取面对称：ExtractJSON（首个 {/[ 起）、ExtractObject、ExtractArray
	// ——散文里先出现 '{' 时 ExtractJSON 会挑中散文那个 '{'，数组候选补位
	// （issue #37）。
	for _, c := range [3]string{
		ExtractJSON(text),
		ExtractObject(stripCodeFence(text)),
		ExtractArray(stripCodeFence(text)),
	} {
		if c == "" {
			continue
		}
		closed, partial := closePartial(c)
		if !partial {
			continue
		}
		// 宽松闭合只对数组/对象（issue 语义）：顶层散串不救——防止把
		// 任意悬空引号文本「修」成一个字符串值。
		if tc := strings.TrimLeft(c, " \t\n\r"); tc == "" || (tc[0] != '{' && tc[0] != '[') {
			continue
		}
		if json.Unmarshal([]byte(closed), v) == nil {
			return true, nil
		}
		if fixed := Repair(closed); fixed != closed && json.Unmarshal([]byte(fixed), v) == nil {
			return true, nil
		}
	}
	return false, fmt.Errorf("jsonrepair: 无法解析为 JSON（宽松模式无法闭合出合法 JSON）")
}

// closePartial 尽力闭合截断的 JSON 前缀。partial=true 表示原文在 EOF 处
// 不完整（字符串未闭合 / 悬挂转义 / 悬空逗号 / 悬空冒号 / 括号未闭合），
// 返回值是闭合后的文本；闭合后仍非法由调用方决定去留。
//
// 与 Repair 的差异：Repair 的 closeUnclosed 只补括号，且对「引号未闭合」
// 场景会把闭括号写进字符串里产出非法 JSON——截断在中途字符串时
// （`"HostName":"trunc` 结尾，真机固件会话过期/重置的高频形态）两者皆无解，
// 本函数是宽松模式专属的完整闭合。截断在键位字符串中段（{"Host 结尾）
// 闭引号后补 :null；截断在字面量中段（tru/fal/nul）不在闭合能力内，仍报错。
func closePartial(s string) (string, bool) {
	var stack []byte
	inStr, esc := false, false
	keyPos := false       // 当前未闭合字符串是否键位（{"Host 截断形态）
	expectColon := false  // 键位字符串已闭合但冒号未出现（{"a":"b","c" 截断形态）
	lastStruct := byte(0) // 字符串外最后一个非空白字节（尾逗号/尾冒号判定）
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
				expectColon = keyPos // 键位字符串在此闭合：其后必须出现冒号
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
			// 引号开口位置决定 EOF 闭合语义：开在 '{' 后是键位
			// （闭引号后还缺冒号），开在 ':'/'[' 后是值位。
			//
			// 开在 ',' 后**要分上下文**：逗号既可能是对象的键值分隔符，
			// 也可能是数组元素的分隔符。只看 lastStruct 会把数组里的元素串
			// 误判成键位 ⇒ 闭合时补 ":null" ⇒ 产出 ["a","b":null] 这种非法
			// JSON ⇒ 宽松解析整体失败（实测：["a","b 与 {"a":["x","y 都
			// 解析不出来）。
			//
			// 判据是**开口时所处的括号上下文**：栈顶是 '{' 才是对象内
			// （键位），栈顶是 '[' 是数组内（值位）。
			keyPos = lastStruct == '{' ||
				(lastStruct == ',' && len(stack) > 0 && stack[len(stack)-1] == '{')
		case '{', '[':
			stack = append(stack, c)
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case ':':
			expectColon = false // 冒号如期出现
		}
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			lastStruct = c
		}
	}

	// inStr：字符串未闭合（其后的括号闭合才落在字符串外）；
	// esc：EOF 悬挂 `\`，转义不完整；expectColon：键位串已闭合、冒号缺失；
	// lastStruct ','/':'：值悬空。
	partial := inStr || esc || expectColon || len(stack) > 0 ||
		(!inStr && (lastStruct == ',' || lastStruct == ':'))
	if !partial {
		return s, false
	}

	var b strings.Builder
	b.Grow(len(s) + len(stack) + 8)
	if inStr {
		if esc {
			s = s[:len(s)-1] // EOF 悬挂 `\`：转义不完整，丢反斜杠（与 Repair 非法转义同口径）
		}
		b.WriteString(s)
		b.WriteByte('"')
		if keyPos {
			b.WriteString(":null") // 键位截断（{"Host 结尾）：闭引号后还缺冒号与值
		}
	} else {
		trimmed := strings.TrimRight(s, " \t\n\r")
		switch {
		case expectColon:
			b.WriteString(trimmed)
			b.WriteString(":null") // 键位串已闭合、冒号缺失（{"a":"b","c" 结尾）
		case lastStruct == ',':
			b.WriteString(trimmed[:len(trimmed)-1]) // 丢 EOF 悬空逗号
		case lastStruct == ':':
			b.WriteString(trimmed)
			b.WriteString("null") // 悬空冒号补 null（语义最中性的占位）
		default:
			b.WriteString(trimmed)
		}
	}
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			b.WriteByte('}')
		} else {
			b.WriteByte(']')
		}
	}
	return b.String(), true
}
