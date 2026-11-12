// Package jsonrepair LLM 输出宽容 JSON 修复骨架：栅栏剥离 → 散文抽对象/抽数组 →
// 语法级修复 → 标量归一。报告/领域 schema 由调用方持有；本包只做格式宽容。
package jsonrepair

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ExtractObject 提取文本中第一个平衡的 JSON 对象（字符串字面量感知；
// 未闭合则返回剩余文本由 Repair 补全括号）。无对象返回 ""。
func ExtractObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	// 未闭合（截断）：剪掉最后一个 '}' 之后的垃圾，剩余缺口交 Repair 补全。
	tail := s[start:]
	if i := strings.LastIndex(tail, "}"); i >= 0 {
		tail = tail[:i+1]
	}
	return tail
}

// ExtractArray 提取文本中第一个平衡的 JSON 数组（字符串字面量感知；
// 未闭合则返回剩余文本由 Repair 补全括号）。无数组返回 ""。
//
// 与 ExtractObject 对称的一对面（issue #37）：对象面有抽取件、数组面此前没有——
// 数组文本只能被 ExtractObject 抽成「数组里的第一个对象」，对数组语义是错的
// （ExtractObject(`[{"a":1}]`) = `{"a":1}`）。调用方要抽数组只能自己手搓
// IndexAny + 深度扫描。
//
// 括号栈而非单一深度计数：数组可嵌套数组（`[[1,2],3]`）也可含对象字段数组
// （`[{"b":[2]}]`），只数 '[' 会把内层 ']' 当作数组收尾、切出半截。
func ExtractArray(s string) string {
	start := strings.IndexByte(s, '[')
	if start < 0 {
		return ""
	}
	// 栈存期望的闭合符：'[' → ']'，'{' → '}'。
	var stack []byte
	inStr, esc := false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '[':
			stack = append(stack, ']')
		case '{':
			stack = append(stack, '}')
		case ']', '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					return s[start : i+1]
				}
			}
		}
	}
	// 未闭合（截断）：剪掉最后一个 ']' 之后的垃圾，剩余缺口交 Repair 补全。
	tail := s[start:]
	if i := strings.LastIndexByte(tail, ']'); i >= 0 {
		tail = tail[:i+1]
	}
	return tail
}

// Repair 轻量 JSON 修复：全角结构标点、非法转义（反斜杠转义保留字面量）、
// 连续尾逗号、缺值补 null、未闭合括号。只处理语法级损坏；语义归一化由
// Normalize / 调用方承担。
//
// 单遍字符串感知扫描（第六轮审计重构）：此前尾逗号/非法转义走全文正则、不辨
// 字符串边界——字符串里合法的 `", }"` / `"\x"` 序列会被当损坏静默改写（修复器
// 的错误成功比失败更贵）。现在各类修复共用一个 inStr/esc 状态机，与
// scanOpenBrackets/ExtractObject 同一纪律：字符串外才动结构位，字符串内只修
// 转义。
//
// 输出契约（#262/#263/#268/#321 收敛）：单次调用即收敛到合法 JSON——
//   - 连续逗号一次清空（此前每遍只删一个，`{"a":1,,}` 要修两轮才合法）；
//   - 非法转义不再丢反斜杠改写值（`\x41` 曾被改成 `x41`），反斜杠转义为
//     `\\` 忠实保留「反斜杠 + 后续字符」的字面量，与 `\uXXXX` 残缺形态同口径；
//   - `:` 后缺值（`{"a":` / `{"a":}` / `{"a": ,}`）补 null——此前只补括号，
//     `{"a":}` 是非法 JSON 的稳定不动点，修复链路无出路；
//   - 括号种类错配改写为栈顶期待的那个（`{"a":1]` → `{"a":1}`），无主
//     闭合符（栈空）丢弃——此前闭合符出栈不看种类，`{"a":1]` 的扫描栈被
//     错误清空、closeUnclosed 判已平衡不补全，非法 JSON 原样返回（镜像
//     形态 `[{"a":1}` 却能收敛，#321）。
func Repair(s string) string {
	var b bytes.Buffer
	b.Grow(len(s))
	inStr, esc := false, false
	// truncAt 尾逗号候选段起点（连续逗号段首个逗号 + 其间空白）：确认紧跟
	// '}'/']' 才回退删除。commaRun 标记正在扫连续逗号段——段内后续逗号直接
	// 丢弃（一次清空，#262），truncAt 因此不需要被同段第二个逗号覆盖。
	truncAt := -1
	commaRun := false
	// needValue 标记 ':' 之后还没见到值（#268）：遇到 ','/'}'/']'/EOF 时补 null。
	// 只在对象上下文成立（栈顶 '{'）——数组里的裸 ':' 是纯垃圾，不为其编造 null。
	needValue := false
	// stack 是字符串外的开括号栈（needValue 的上下文闸门；闭合补全仍由
	// closeUnclosed 按全文扫描做，两处口径一致）。
	var stack []byte
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if inStr {
			if esc {
				esc = false
				if r == 'u' {
					// \uXXXX：4 位十六进制才合法，原样保留；残缺形态把反斜杠
					// 转义为 `\\`，字面量忠实保留（`\uZZZZ` 输出后仍可读）。
					if hasHex4(s[i+size:]) {
						b.WriteString(s[i-1 : i+size+4])
						i += size + 4
						continue
					}
					b.WriteString(`\\u`)
					i += size
					continue
				}
				if validEscape(r) {
					b.WriteByte('\\')
					b.WriteRune(r)
					i += size
					continue
				}
				// 非法转义（\x \q 等）：不丢反斜杠——丢掉会把值改写成另一个
				// （\x41 → x41），且输出合法 JSON 无人能发现（#263）。转义为
				// `\\` 保留原文语义。
				b.WriteString(`\\`)
				b.WriteRune(r)
				i += size
				continue
			}
			if r == '\\' {
				esc = true // 反斜杠的写入延到下一 rune 判定合法性
				i += size
				continue
			}
			if r == '"' {
				inStr = false
			}
			b.WriteRune(r)
			i += size
			continue
		}
		switch r {
		case '"':
			inStr = true
			truncAt = -1 // 值开始，逗号不再是尾逗号
			commaRun = false
			needValue = false
			b.WriteRune(r)
		case '{', '[':
			truncAt = -1
			commaRun = false
			needValue = false // 开括号本身是值（"a":{）——缺值标记到此为止
			stack = append(stack, byte(r))
			b.WriteRune(r)
		case '，': // 全角结构标点（字符串外）→ ASCII；逗号语义同 ASCII
			fallthrough
		case ',':
			if commaRun {
				i += size
				continue // 连续逗号段：后续逗号直接丢弃（一次清空）
			}
			if needValue { // "a": ,——缺值先补 null，逗号仍是分隔符
				b.WriteString("null")
				needValue = false
			}
			truncAt = b.Len()
			commaRun = true
			b.WriteByte(',')
		case '：', ':': // 全角结构标点（字符串外）→ ASCII；冒号语义同 ASCII
			truncAt = -1
			commaRun = false
			needValue = len(stack) > 0 && stack[len(stack)-1] == '{'
			b.WriteByte(':')
		case '}', ']':
			if commaRun {
				b.Truncate(truncAt) // 尾逗号段（含其间空白）删除
				commaRun = false
			}
			if needValue { // "a":}——补值再闭合，落点必须是合法 JSON（#268）
				b.WriteString("null")
				needValue = false
			}
			truncAt = -1
			if len(stack) == 0 {
				// 无主闭合符（栈空，#321）：丢弃——收敛契约（#268）要求单次
				// 输出即合法 JSON，残留闭合符写回去就是稳定不动点；无主闭合符
				// 没有任何「保留后合法」的可能性，丢弃是唯一收敛解。
				break
			}
			// 栈顶种类核对（#321）：'{' 等 '}'、'[' 等 ']'——错配闭合符改写
			// 为栈顶期待的那个（`{"a":1]` → `{"a":1}`）。不核对会让扫描栈被
			// 错误出栈清空、closeUnclosed 判已平衡不补全，非法 JSON 原样返回。
			want := closerFor(stack[len(stack)-1])
			stack = stack[:len(stack)-1]
			b.WriteByte(want)
		default:
			switch r {
			case ' ', '\t', '\n', '\r':
				// 空白不重置尾逗号候选（", }" 的空格段），也不清缺值标记
				// （"a": 1 的空格段）
			default:
				truncAt = -1
				commaRun = false
				needValue = false
			}
			b.WriteRune(r)
		}
		i += size
	}
	if commaRun {
		b.Truncate(truncAt) // EOF 悬空逗号：与尾逗号同判，删（`[1,2,` → `[1,2]`）
	}
	if esc {
		b.WriteString(`\\`) // EOF 悬挂 `\`：转义不完整，按字面量保留（#263 同口径）
	}
	if needValue { // "a":<EOF>——补值再闭合（#268）
		b.WriteString("null")
	}
	repaired := b.String()
	return closeUnclosed(repaired, scanOpenBrackets(repaired))
}

// hasHex4 判断 s 的前 4 个字节是否全为十六进制数字（\uXXXX 合法性判定）。
func hasHex4(s string) bool {
	if len(s) < 4 {
		return false
	}
	for i := range 4 {
		c := s[i]
		isHex := '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
		if !isHex {
			return false
		}
	}
	return true
}

// validEscape JSON 合法转义字符（\" \\ \/ \b \f \n \r \t \uXXXX）。
func validEscape(r rune) bool {
	switch r {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
		return true
	}
	return false
}

// closerFor 返回开括号期待的闭合符（'{' → '}'，'[' → ']'）。#321：括号
// 种类错配（'{' 被 ']' 闭合）此前让扫描栈被错误出栈清空、closeUnclosed 判
// 「已平衡」不补全，非法 JSON 原样返回——Repair 主扫描（错配改写/无主丢弃）
// 与 scanOpenBrackets（错配不出栈）两处扫描同步按种类核对。
func closerFor(open byte) byte {
	if open == '{' {
		return '}'
	}
	return ']'
}

// scanOpenBrackets 括号栈扫描（字符串字面量感知），返回未闭合的开括号序列。
// 错配闭合符不出栈（#321）：'{' 等 '}'、'[' 等 ']'，乱入的另一种闭合符不是
// 任何开括号的收尾——与 Repair 主扫描的改写口径一致，两处扫描不得各判各的。
func scanOpenBrackets(s string) []byte {
	var stack []byte
	inStr, esc := false, false
	for _, c := range s {
		switch {
		case esc:
			esc = false
		case c == '\\' && inStr:
			esc = true
		case c == '"':
			inStr = !inStr
		case !inStr && (c == '{' || c == '['):
			stack = append(stack, byte(c))
		case !inStr && (c == '}' || c == ']'):
			if len(stack) > 0 && byte(c) == closerFor(stack[len(stack)-1]) {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return stack
}

// closeUnclosed 按栈序补全未闭合括号（后开先闭）。
func closeUnclosed(s string, stack []byte) string {
	if len(stack) == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + len(stack))
	b.WriteString(s)
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			b.WriteByte('}')
		} else {
			b.WriteByte(']')
		}
	}
	return b.String()
}

// fixFullWidth 的全角标点修复已并入 Repair 的单遍扫描（字符串外 ，： → ASCII；
// 字符串内原样保留）。

// Schema 标量归一化作用范围（领域键集合由调用方提供）。
type Schema struct {
	// StringKeys 期望为字符串的键：bool/数字 → 文本；对象/数组 → 扁平化文本。
	StringKeys map[string]bool
	// ListKeys 期望为数组的键：标量 → 单元素数组（或空数组）；元素内标量 → 文本。
	ListKeys map[string]bool
	// OnMap 每个 map 在其子节点归一完成后的领域钩子（可 nil）。可增删改键。
	OnMap func(m map[string]any)
}

// Normalize 把字符串位的 bool/数字统一转成文本（宽容留给格式，拦截留给白名单）。
// raw 非法 JSON 时返回 error。
func Normalize(raw json.RawMessage, schema *Schema) (string, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", err
	}
	if schema == nil {
		schema = &Schema{}
	}
	walkNormalize(m, schema)
	out, err := json.Marshal(m)
	if err != nil {
		//nolint:nilerr // 宽容修复链永不失败：重序列化异常时回退原文，设计语义
		return string(raw), nil
	}
	return string(out), nil
}

func walkNormalize(m map[string]any, schema *Schema) {
	for k, v := range m {
		switch {
		case schema.StringKeys[k]:
			if s, ok := coerceString(v); ok {
				m[k] = s
			} else if flat, ok := flattenObject(v); ok {
				m[k] = flat
			} else if flat, ok := flattenList(v); ok {
				m[k] = flat
			}
		case schema.ListKeys[k]:
			if arr, isArr := v.([]any); isArr {
				for i, item := range arr {
					if s, ok := coerceString(item); ok {
						arr[i] = s
					}
				}
			} else if s, ok := coerceString(v); ok {
				// 标量 → 单元素数组（schema 声明 array 而模型给单值的高频形态）
				if s != "" {
					m[k] = []any{s}
				} else {
					m[k] = []any{}
				}
			}
		}
		// 子节点先归一，OnMap 才能看到已规范化的嵌套结构。
		if sub, ok := m[k].(map[string]any); ok {
			walkNormalize(sub, schema)
		}
		if arr, ok := m[k].([]any); ok {
			for _, item := range arr {
				if sub, ok := item.(map[string]any); ok {
					walkNormalize(sub, schema)
				}
			}
		}
	}
	if schema.OnMap != nil {
		schema.OnMap(m)
	}
}

// ParseLenient 栅栏剥离 → 直接解析 → Repair 重试 → ExtractObject+Repair 重试
// → ExtractArray+Repair 重试。schema 非 nil 时归一只服务修复链：每步先裸解析，
// 失败才 Normalize 重试（#301）——完全合法的输入必须走精确路径，先归一会经
// map[string]any 往返把数字全变 float64，>2^53 的整数（雪花 ID/订单号常态
// 量级）在结构体目标上被静默改值。
//
// 抽取兜底对象面/数组面对称（issue #37）：此前只有 ExtractObject 一件，多段数组
// 与散文含 '{' 的数组文本整段作废。顺序固定为「对象面先、数组面后」——目标类型
// 决定哪个形状能解析成功（[]T 只吃数组、map 只吃对象），而 any/json.RawMessage
// 这类通吃形状的目标必须保住对象面既有行为（`{"items":[1,2]}` 仍抽成整个对象，
// 不能被内层数组抢走）。
func ParseLenient(raw string, v any, schema *Schema) error {
	candidate := stripCodeFence(raw)
	if candidate == "" {
		return fmt.Errorf("jsonrepair: 空输入")
	}
	tryParse := func(c string) bool {
		// 裸解析优先（#301）：合法输入零开销精确返回——Normalize 的
		// Unmarshal→map[string]any→Marshal 往返会把数字全变 float64，先归一
		// 等于剥夺精确解析的机会。归一只服务修复链：裸解析失败（畸形或类型
		// 不合，如数字位的 bool 要靠 StringKeys 强转文本）才走 schema 归一。
		if json.Unmarshal([]byte(c), v) == nil {
			return true
		}
		if schema != nil {
			normalized, nerr := Normalize(json.RawMessage(c), schema)
			if nerr == nil && json.Unmarshal([]byte(normalized), v) == nil {
				return true
			}
		}
		return false
	}
	if tryParse(candidate) {
		return nil
	}
	if repaired := Repair(candidate); repaired != candidate && tryParse(repaired) {
		return nil
	}
	if extracted := ExtractObject(candidate); extracted != "" {
		if tryParse(extracted) {
			return nil
		}
		if tryParse(Repair(extracted)) {
			return nil
		}
	}
	if extracted := ExtractArray(candidate); extracted != "" {
		if tryParse(extracted) {
			return nil
		}
		if tryParse(Repair(extracted)) {
			return nil
		}
	}
	return fmt.Errorf("jsonrepair: 无法解析为 JSON")
}

// coerceString bool/数字 → 文本；字符串原样；其余（nil/对象/数组）不处理。
func coerceString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case bool:
		return fmt.Sprintf("%t", t), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	}
	return "", false
}

// CoerceString coerceString 的导出面：调用方在 Schema.OnMap 域内钩子里做
// 自定义归一（枚举收敛/布尔翻转等）时复用同一标量 coerce 口径，避免再造平行实现。
func CoerceString(v any) (string, bool) {
	return coerceString(v)
}

// flattenObject 对象形态 → 按 key 字典序取标量值拼为「v1；v2」文本。全空对象返回 false。
func flattenObject(v any) (string, bool) {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return "", false
	}
	keys := slices.Sorted(maps.Keys(m))
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if s, ok := coerceString(m[k]); ok && strings.TrimSpace(s) != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "；"), true
}

// flattenList 数组形态 → 「1. x；2. y」序号拼接。结构化条目取 title/detail。
func flattenList(v any) (string, bool) {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return "", false
	}
	parts := make([]string, 0, len(arr))
	for _, item := range arr {
		var s string
		if m, isMap := item.(map[string]any); isMap {
			title, _ := coerceString(m["title"])
			detail, _ := coerceString(m["detail"])
			switch {
			case title != "" && detail != "":
				s = title + "：" + detail
			case title != "":
				s = title
			case detail != "":
				s = detail
			default:
				s, _ = flattenObject(m)
			}
		} else {
			s, _ = coerceString(item)
		}
		if strings.TrimSpace(s) != "" {
			parts = append(parts, fmt.Sprintf("%d. %s", len(parts)+1, s))
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "；"), true
}
