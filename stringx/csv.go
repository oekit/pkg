package stringx

import "strings"

// SplitCSVNonEmpty 按逗号拆分字符串（RFC 4180 引号语义），逐项去掉首尾
// 空白并丢弃空项。
//
// 与 SplitByComma 的区别：SplitByComma 就是 strings.Split(s, ",")——不认
// 引号、不 trim、不过滤空项、空串返回 [""]（1 个空元素），名字叫「按逗号
// 拆」但实际把「拆完顺手清干净」这层留给每个消费方各写一遍（典型样板：
// strings.Split → 逐项 TrimSpace → 跳过空项）。本原语把那层样板一次收进去。
//
// 与 SplitWith 的区别（不要拿 SplitWith 顶替）：SplitWith 走 strings.Fields，
// 按**空白**切分，值里含空格时（如 glob 列表 "my glob"）会被静默拆成两项，
// 改变语义。CSV 值的空白是有意义的（"a b" 是一个值，不是两个），所以这条
// 面需要按逗号切的专门原语，而不是拿现有函数拼。
//
// 引号口径（[#259]，RFC 4180）：
//
//   - 字段以双引号开头即按引号字段解析：引号内的逗号**不切分**（
//     `"a,b",c` → ["a,b" "c"]——此前被静默切成 ["\"a" "b\"" "c"]，
//     含逗号的地址 / JSON 片段 / SQL 条件等产品名一个字段拆成两段且不报错）；
//   - 引号内的 `""` 是字面引号（`"a""b"` → `a"b`）；
//   - 引号在值外**剥离**（`"x"` → `x`——此前第三种不一致状态：不解释、
//     也不剥离，原样保留）；
//   - 引号内的空白是值的一部分，不 trim 内部；两端引号外的空白仍按
//     「逐项去首尾空白」处理（`"a, b" , c` → ["a, b" "c"]）；
//   - 未闭合引号宽松兜底：余下全部并入当前字段，不报错（本函数族
//     「拆分不做尽力校验」的既有立场）；
//   - 闭合引号后到下一逗号之间的残余字符（如 `"a"x` 的 x）宽松并入
//     当前字段，与 RFC 对畸形输入「未定义」的行为不同，这里选择不丢字。
//
// 口径（写死在本函数契约里）：
//
//   - 逐项去掉**首尾**空白（strings.TrimSpace，含 Unicode 空白）；
//   - 去掉空白后为空的项**丢弃**（"a,,b" → ["a","b"]，引号空字段 ""
//     同样丢弃）；
//   - **空串返回 nil**（不是空切片）——调用方普遍用 != nil / len()>0 区分
//     「没配」与「配了空」，返回空切片会把两者混同；
//   - 全是空白或空项的输入（""、"  "、",,"）同样返回 nil。
//
// 用法:
//
//	stringx.SplitCSVNonEmpty(`"a,b",c`)  // ["a,b" "c"]
//	stringx.SplitCSVNonEmpty(`"x"`)      // ["x"]
//	stringx.SplitCSVNonEmpty("a, b,,c")  // ["a" "b" "c"]
//	stringx.SplitCSVNonEmpty("")         // nil
//	stringx.SplitCSVNonEmpty("  ,  ")    // nil
func SplitCSVNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	parts := splitCSVFields(s)
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

// splitCSVFields 按逗号切分一行，带 RFC 4180 引号状态机：**字段开头**的
// 双引号开启引号字段（内含逗号不切分、"" 是字面引号）；字段中部的裸引号
// 按字面保留（RFC 对未加引号字段里的引号是「未定义」，这里选择不解释）。
// 只切分，不 trim、不丢空项——那两层留给 SplitCSVNonEmpty（单一职责，
// SplitByComma 若要引号语义也走这里）。
func splitCSVFields(s string) []string {
	var fields []string
	var b strings.Builder
	flush := func() {
		fields = append(fields, b.String())
		b.Reset()
	}
	atFieldStart := true
	i := 0
	for i < len(s) {
		switch c := s[i]; {
		case c == ',':
			flush()
			atFieldStart = true
			i++
		case c == '"' && atFieldStart:
			// 引号字段：吞到闭合引号，"" 解成字面引号。
			i++
			closed := false
			for i < len(s) {
				if s[i] == '"' {
					if i+1 < len(s) && s[i+1] == '"' {
						b.WriteByte('"')
						i += 2
						continue
					}
					i++ // 闭合引号
					closed = true
					break
				}
				b.WriteByte(s[i])
				i++
			}
			atFieldStart = false
			if !closed {
				// 未闭合引号：余下全部并入当前字段（i 已到串尾）。
				break
			}
			// 闭合引号后的残余（如 `"a"x` 的 x）宽松并入，到下一逗号为止。
			for i < len(s) && s[i] != ',' {
				b.WriteByte(s[i])
				i++
			}
		default:
			b.WriteByte(c)
			atFieldStart = false
			i++
		}
	}
	flush()
	return fields
}
