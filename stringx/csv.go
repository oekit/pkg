package stringx

import "strings"

// SplitCSVNonEmpty 按逗号拆分字符串，逐项去掉首尾空白并丢弃空项。
//
// 与 SplitByComma 的区别：SplitByComma 就是 strings.Split(s, ",")——不 trim、
// 不过滤空项、空串返回 [""]（1 个空元素），名字叫「按逗号拆」但实际把
// 「拆完顺手清干净」这层留给每个消费方各写一遍（典型样板：strings.Split
// → 逐项 TrimSpace → 跳过空项）。本原语把那层样板一次收进去。
//
// 与 SplitWith 的区别（不要拿 SplitWith 顶替）：SplitWith 走 strings.Fields，
// 按**空白**切分，值里含空格时（如 glob 列表 "my glob"）会被静默拆成两项，
// 改变语义。CSV 值的空白是有意义的（"a b" 是一个值，不是两个），所以这条
// 面需要按逗号切的专门原语，而不是拿现有函数拼。
//
// 口径（写死在本函数契约里）：
//
//   - 逐项去掉**首尾**空白（strings.TrimSpace，含 Unicode 空白）；
//   - 去掉空白后为空的项**丢弃**（"a,,b" → ["a","b"]）；
//   - **空串返回 nil**（不是空切片）——调用方普遍用 != nil / len()>0 区分
//     「没配」与「配了空」，返回空切片会把两者混同；
//   - 全是空白或空项的输入（""、"  "、",,"）同样返回 nil。
//
// 用法:
//
//	stringx.SplitCSVNonEmpty("a, b,,c")  // ["a" "b" "c"]
//	stringx.SplitCSVNonEmpty("a,,b")     // ["a" "b"]
//	stringx.SplitCSVNonEmpty("")         // nil
//	stringx.SplitCSVNonEmpty("  ,  ")     // nil
func SplitCSVNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
