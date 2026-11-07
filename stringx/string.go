package stringx

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/oekit/pkg/slicex"
)

var (
	// ErrInvalidStartPosition indicates the start position is invalid.
	ErrInvalidStartPosition = errors.New("start position is invalid")
	// ErrInvalidStopPosition indicates the stop position is invalid.
	ErrInvalidStopPosition = errors.New("stop position is invalid")
	// ErrInvalidRange indicates the substring range is inverted (start > stop).
	ErrInvalidRange = errors.New("substring range is inverted")
)

// Contains 检查 str 是否在 list 中。
//
// 语义单源在 slicex.Contains[string]（泛型版），本函数保留仅为字符串场景
// 的可读性——实现是一行转发，不是第二份副本（#242：两份逐字同构的循环体
// 在改口径时只会改一处，另一处静默保持旧行为）。
func Contains(list []string, str string) bool {
	return slicex.Contains(list, str)
}

// Filter 返回 s 中满足 filter 条件的字符（保留命中）。
//
// ⚠️ 语义变更（#173，破坏性）：v0.32 及以前本函数是「剔除命中」（命中即删），
// 与同库 slicex.Filter（保留命中）同名反方向——谓词写反零报错，只是结果里
// 多/少了字符（实测事故：净化函数按「保留命中」的肌肉记忆写谓词，控制字符
// 被留在回显里）。v0.33.0 起与 slicex.Filter / 主流 filter 语义对齐（JS
// Array.filter、Java Stream.filter、Rust Iterator::filter 皆保留命中）。
//
// 迁移（按意图二选一）：
//   - 原「剔除命中」（filter(x)=true 的字符被删）：改用 DeleteFunc；
//   - 原谓词写的是「保留条件」却以为在删：改用 Filter/KeepFunc（结果反转）。
//
// 方向要在调用点自解释时用 KeepFunc（保留命中）/ DeleteFunc（剔除命中）对偶入口。
func Filter(s string, filter func(r rune) bool) string {
	return KeepFunc(s, filter)
}

// KeepFunc 返回 s 中满足 keep 条件的字符（保留命中；方向自解释，命名对齐
// slices.KeepFunc 的口径）。与 Filter 同语义——Filter 与 slicex.Filter
// 对齐后即「保留命中」（#173）。
func KeepFunc(s string, keep func(r rune) bool) string {
	var n int
	chars := []rune(s)
	for i, x := range chars {
		if keep(x) {
			if n < i {
				chars[n] = x
			}
			n++
		}
	}
	return string(chars[:n])
}

// DeleteFunc 从 s 中删除满足 del 条件的字符（剔除命中；方向自解释，命名对齐
// slices.DeleteFunc 的口径）。
//
// 这是 Filter 在 v0.32 及以前的行为（#173 语义对齐后迁移至此）：
//
//	// 剔除控制字符（净化回显）
//	clean := stringx.DeleteFunc(s, unicode.IsControl)
func DeleteFunc(s string, del func(r rune) bool) string {
	var n int
	chars := []rune(s)
	for i, x := range chars {
		if n < i {
			chars[n] = x
		}
		if !del(x) {
			n++
		}
	}
	return string(chars[:n])
}

// FirstN 返回 s 的前 n 个字符，可选添加省略号。
func FirstN(s string, n int, ellipsis ...string) string {
	// n<0 对齐 LastN/TruncRunes 的「非法上限=全截断」口径（#183）：此前
	// 循环永不命中截断点 ⇒ 静默返回整串——截断护栏整段失效，且与 Trunc
	// 家族其它函数行为相反（自家 LastN 的 doc 已自认「那是实现怪癖不是
	// 语义」）。修复而非行为变更。
	if n < 0 {
		ret := ""
		for _, each := range ellipsis {
			ret += each
		}
		return ret
	}
	var i int
	for j := range s {
		if i == n {
			ret := s[:j]
			for _, each := range ellipsis {
				ret += each
			}
			return ret
		}
		i++
	}
	return s
}

// LastN 返回 s 的最后 n 个字符（保尾截断：丢前缀、留后缀——文件名限长保
// 扩展名、路径限长保末段这类「尾部比头部更有信息量」的场景），可选在头部
// 添加省略号。与 FirstN 互为镜像：FirstN 保头、省略号拼尾；LastN 保尾、
// 省略号拼头。未发生截断时原样返回（不拼省略号）。
//
// 边界口径：n <= 0 一律按「保留 0 个」处理（返回空串+省略号）。与 FirstN
// 的 n<0 现状（其循环永不命中截断点 ⇒ 原样返回整串）有意不同——那是实现
// 怪癖不是语义，本函数按 TruncRunes 的「非法上限=全截断」口径走，不 panic。
func LastN(s string, n int, ellipsis ...string) string {
	if n <= 0 {
		ret := ""
		for _, each := range ellipsis {
			ret += each
		}
		return ret
	}
	// 从尾部回退 n 个 rune 找保尾起点：无需 []rune 全量分配；
	// 提前走完字符串（n >= rune 数）⇒ 无需截断。
	i := len(s)
	for k := 0; k < n && i > 0; k++ {
		_, size := utf8.DecodeLastRuneInString(s[:i])
		i -= size
	}
	if i == 0 {
		return s
	}
	ret := ""
	for _, each := range ellipsis {
		ret += each
	}
	return ret + s[i:]
}

// HasEmpty 检查 args 中是否存在空字符串。
func HasEmpty(args ...string) bool {
	for _, arg := range args {
		if len(arg) == 0 {
			return true
		}
	}
	return false
}

// Join 将多个字符串用 sep 连接，忽略空字符串。
func Join(sep byte, elem ...string) string {
	var size int
	for _, e := range elem {
		size += len(e)
	}
	if size == 0 {
		return ""
	}

	buf := make([]byte, 0, size+len(elem)-1)
	for _, e := range elem {
		if len(e) == 0 {
			continue
		}
		if len(buf) > 0 {
			buf = append(buf, sep)
		}
		buf = append(buf, e...)
	}
	return string(buf)
}

// NotEmpty 检查 args 中所有字符串是否都不为空。
func NotEmpty(args ...string) bool {
	return !HasEmpty(args...)
}

// Remove 从 strings 中移除指定的 strs。
func Remove(list []string, strs ...string) []string {
	out := append([]string(nil), list...)
	for _, str := range strs {
		var n int
		for _, v := range out {
			if v != str {
				out[n] = v
				n++
			}
		}
		out = out[:n]
	}
	return out
}

// Reverse 反转字符串。
func Reverse(s string) string {
	runes := []rune(s)
	for from, to := 0, len(runes)-1; from < to; from, to = from+1, to-1 {
		runes[from], runes[to] = runes[to], runes[from]
	}
	return string(runes)
}

// Substr 返回 [start, stop) 范围内的子字符串，支持 UTF-8。
func Substr(str string, start, stop int) (string, error) {
	rs := []rune(str)
	length := len(rs)

	if start < 0 || start > length {
		return "", ErrInvalidStartPosition
	}
	if stop < 0 || stop > length {
		return "", ErrInvalidStopPosition
	}
	if start > stop {
		return "", ErrInvalidRange
	}
	return string(rs[start:stop]), nil
}

// TakeOne 返回非空的字符串，如果 valid 非空则返回 valid，否则返回 or。
func TakeOne(valid, or string) string {
	if len(valid) > 0 {
		return valid
	}
	return or
}

// TakeWithPriority 返回第一个非空的函数返回值。
func TakeWithPriority(fns ...func() string) string {
	for _, fn := range fns {
		val := fn()
		if len(val) > 0 {
			return val
		}
	}
	return ""
}

// ToCamelCase 将字符串首字母转为小写。
//
// 按 rune 处理（多字节首字符不被截断）。非法 UTF-8 字节不 panic：range 会把
// 非法字节解成 U+FFFD，其**实际**宽度是 1 字节，而 utf8.RuneLen(U+FFFD) 是 3
// ⇒ 此前用 RuneLen 切片会越界（slice bounds out of range [3:1]）。
func ToCamelCase(s string) string {
	for i, v := range s {
		// 用实际解码宽度切片：合法 UTF-8 时与 utf8.RuneLen(v) 相同，
		// 非法字节时是 1（range 消费掉的字节数）。
		_, size := utf8.DecodeRuneInString(s[i:])
		return string(unicode.ToLower(v)) + s[i+size:]
	}
	return ""
}

// Union 合并两个字符串切片并去重。
func Union(first, second []string) []string {
	// 保序实现（#187）：先 first 后 second 扫重——此前从 map 迭代构造结果，
	// 每次调用顺序不同（Go map 遍历随机化），而同库 slicex.Unique 明确「保持
	// 原始顺序」——两族口径相反。合并结果进 UI 列表/审计日志/签名串/缓存 key
	// 时顺序抖动 = diff 噪声、快照比对 flaky、缓存命中率玄学下降。成本一致
	// （同是 O(n+m) + 一次 map 查重）。
	seen := make(map[string]struct{}, len(first)+len(second))
	merged := make([]string, 0, len(first)+len(second))
	for _, each := range first {
		if _, ok := seen[each]; !ok {
			seen[each] = struct{}{}
			merged = append(merged, each)
		}
	}
	for _, each := range second {
		if _, ok := seen[each]; !ok {
			seen[each] = struct{}{}
			merged = append(merged, each)
		}
	}
	return merged
}

// JoinWith 将字符串切片用指定分隔符连接。
func JoinWith(elem []string, sep string) string {
	var size int
	for _, e := range elem {
		size += len(e)
	}
	if size == 0 {
		return ""
	}

	buf := make([]byte, 0, size+len(elem)-1)
	for _, e := range elem {
		if len(e) == 0 {
			continue
		}
		if len(buf) > 0 {
			buf = append(buf, sep...)
		}
		buf = append(buf, e...)
	}
	return string(buf)
}

// SplitWith 将字符串按指定分隔符列表分割后返回字段切片。
func SplitWith(str string, sep []string) []string {
	for _, v := range sep {
		str = strings.ReplaceAll(str, v, " ")
	}
	return strings.Fields(str)
}

// SplitByComma 将字符串按逗号分割。
func SplitByComma(str string) []string {
	return strings.Split(str, ",")
}

// JoinByComma 将字符串切片用逗号连接。
func JoinByComma(str []string) string {
	return strings.Join(str, ",")
}
