// normalize.go —— 识别前的数字形态归一化（issue #297）。
//
// # 为什么需要一层归一化
//
// 原识别只认 **ASCII 连续数字**：`１３８…`（全角）、`138​12345678`（零宽插入）、
// `138 1234 5678` / `138-1234-5678` / `138.1234.5678`（分组分隔）全部漏检，
// 而漏检的形态会整串原样外发、`blocked=false`——宿主以为已保护，实际什么都没有。
// 根因不是正则写错，是缺一层「先归一化、再判定」。
//
// # 归一化口径（可预期、可反向验证）
//
//   - **全角 → 半角**：U+FF01..U+FF5E 整体折半（数字、字母与 `+ - .` 都覆盖），
//     U+3000 表意空格折成普通空格；
//   - **不可见字符删除**：U+200B..U+200D、U+2060、U+FEFF、U+00AD、U+180E——
//     它们没有语义，留着只会让「同一串数字」在字节层面有无穷多种写法；
//   - **分组分隔符折叠**：空格 / `-` / `.` 仅在**数字与数字之间**删除，而且只对
//     「整段数字 + 分隔符的连续串」在**可识别长度**（手机号 11、身份证 18；
//     带 `86` / `0086` 前缀时 13 / 15）时折叠。这条限制是**误检面评估后的口径**：
//     无条件删分隔符会把 `版本 1 13812345678` 拼成 12 位长串、并用「前邻数字」
//     把本来能认出的手机号拒掉；按长度整段折叠既覆盖分组写法，又不改变
//     既有的空格/符号边界语义。
//
// 未折叠的分隔符保持原样（偏移不变），因此「12 位不打」「13 位毫秒时间戳不打」
// 「19 位数字串」「随机 18 位数字」等防误命中用例在归一化后仍绿。
//
// # 偏移映射
//
// 归一化文本上的匹配必须能映射回**原串字节偏移**：处置按原串切片，span 覆盖
// 原串里对应的整段（含内部分隔符），打码不会只盖住「归一化后的一半」。
// normalizeForDetect 返回 origIdx：归一化文本每个字节在原串中的起始偏移，
// 末位补原串长度，使 [start,end) 两端都能直接查表。
//
// # 仍然不覆盖（文档同步声明，不假装已判定）
//
// 阿拉伯-印度数字（٠١٢…）等非全角数字形态、字母/汉字夹在数字之间、
// 数字间夹非分隔类符号（如 `+`）——这些不在归一化口径内，仍按未命中处理。
package pii

import (
	"strings"
	"unicode/utf8"
)

type normRune struct {
	r      rune
	start  int  // 原串字节偏移
	size   int  // 原串字节宽度
	folded bool // 是否被折半成 ASCII（映射只需记起点）
}

// normalizeForDetect 返回归一化文本与「归一化字节 → 原串字节」偏移表。
//
// 无需归一化时原样返回 s 且偏移表为 nil（调用方按恒等映射处理），避免热路径
// 为普通 ASCII 文本付出分配代价。
func normalizeForDetect(s string) (norm string, origIdx []int) {
	if !needsNormalization(s) {
		return s, nil
	}

	folded := make([]normRune, 0, len(s))
	for i, r := range s {
		if isInvisible(r) {
			continue
		}
		size := utf8.RuneLen(r)
		if f := foldWidth(r); f != r {
			folded = append(folded, normRune{r: f, start: i, size: size, folded: true})
			continue
		}
		folded = append(folded, normRune{r: r, start: i, size: size})
	}

	// 标记「数字 + 分隔符」整段中可折叠的分隔符：段必须以数字开头和结尾
	// （不吞掉前导/尾随分隔符，否则会改变既有边界语义）。
	drop := make([]bool, len(folded))
	for i := 0; i < len(folded); {
		if !isDigitRune(folded[i].r) {
			i++
			continue
		}
		j := i
		for j < len(folded) && (isDigitRune(folded[j].r) || isGroupSep(folded[j].r)) {
			j++
		}
		if j > i && isGroupSep(folded[j-1].r) {
			for j > i && isGroupSep(folded[j-1].r) {
				j-- // 段尾是分隔符：退到最后一个数字
			}
		}
		if joinableDigitRun(folded[i:j]) {
			for k := i; k < j; k++ {
				if isGroupSep(folded[k].r) {
					drop[k] = true
				}
			}
		}
		i = j
	}

	var b strings.Builder
	b.Grow(len(s))
	idx := make([]int, 0, len(s)+1)
	for i, nr := range folded {
		if drop[i] {
			continue
		}
		if nr.folded {
			b.WriteRune(nr.r)
			idx = append(idx, nr.start)
			continue
		}
		// 未折半的 rune 逐字节原样拷贝，偏移按字节记录。
		for k := 0; k < nr.size; k++ {
			idx = append(idx, nr.start+k)
		}
		b.WriteString(s[nr.start : nr.start+nr.size])
	}
	idx = append(idx, len(s))
	norm = b.String()
	if norm == s {
		return s, nil // 没有任何实际改动：退回恒等映射
	}
	return norm, idx
}

// normalizeDigits 归一化一段已命中的原文（处置用：打码与摘要都按同一口径）。
func normalizeDigits(v string) string {
	norm, _ := normalizeForDetect(v)
	return norm
}

// needsNormalization 快速判是否需要归一化（避免普通 ASCII 文本走分配路径）。
func needsNormalization(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= utf8.RuneSelf {
			return true // 非 ASCII：全角/零宽/表意空格都可能在此
		}
		if !isSepByte(c) {
			continue
		}
		// 只有「数字之间的分隔符」才可能被折叠。
		j := i - 1
		for j >= 0 && isSepByte(s[j]) {
			j--
		}
		if j < 0 || !isDigitByte(s[j]) {
			continue
		}
		k := i + 1
		for k < len(s) && isSepByte(s[k]) {
			k++
		}
		if k < len(s) && isDigitByte(s[k]) {
			return true
		}
	}
	return false
}

// joinableDigitRun 该「数字 + 分隔符」连续段是否属于可识别长度。
//
// 手机号 11 位；身份证 18 位；带 86 / 0086 前缀时分别 13 / 15 位。长度不匹配的
// 段不折叠分隔符——宁可让分组写法漏检，也不把两个相邻数字 token 拼成一个假号。
func joinableDigitRun(run []normRune) bool {
	var digits strings.Builder
	for _, nr := range run {
		if isDigitRune(nr.r) {
			digits.WriteRune(nr.r)
		}
	}
	d := digits.String()
	switch len(d) {
	case 11, 18:
		return true
	case 13:
		return strings.HasPrefix(d, "86")
	case 15:
		return strings.HasPrefix(d, "0086")
	}
	return false
}

// foldWidth 全角 → 半角（Unicode 全角 ASCII 区整体偏移 0xFEE0；表意空格折普通空格）。
func foldWidth(r rune) rune {
	switch {
	case r >= '\uFF01' && r <= '\uFF5E':
		return r - 0xFEE0
	case r == '\u3000':
		return ' '
	}
	return r
}

// isInvisible 零宽与格式类不可见字符（无语义，删除不改变可见文本）。
func isInvisible(r rune) bool {
	switch r {
	case '\u200B', '\u200C', '\u200D', '\u2060', '\uFEFF', '\u00AD', '\u180E':
		return true
	}
	return false
}

func isGroupSep(r rune) bool {
	return r == ' ' || r == '-' || r == '.'
}

func isSepByte(c byte) bool {
	return c == ' ' || c == '-' || c == '.'
}

func isDigitByte(c byte) bool { return c >= '0' && c <= '9' }

func isDigitRune(r rune) bool { return r >= '0' && r <= '9' }
