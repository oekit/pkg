package timex

import "time"

// ParseFirst 按序尝试 layouts，**首个成功即返回**；全部失败返回零值与
// false（#197）。
//
// 面向「同一时间字段在多上游/多端点格式不一」的归一化场景——layout 列表
// 是该字段的服务端方言记录（如里程碑 due_on：GitCode 回显 RFC3339 与
// 日期-only 并存、Gitee 另有 "2006-01-02 15:04:05" wire 形），按序尝试、
// 首命中即返回（列表本身就是优先序），不该每个消费方重写一遍 for-layout
// 循环。
//
// 语义边界（与容错定位一致）：
//
//   - 空输入串返回 (零值, false)——不区分「空」与「坏」，调用方要区分请
//     先判空；
//   - layouts 为空，或**任一** layout 为空串，返回 (零值, false)——空
//     layout 是调用方的配置错误，fail-loud 到 ok=false，不静默跳过；
//   - 全部 layout 都解析失败返回 (零值, false)——全失败的调用方语义各异
//     （nil 指针 / 零值+ok / 错误包装），原语只承诺 (time.Time, bool)
//     一种，包装留给调用方（故不做 MustParseFirst）；
//   - 解析走 time.Parse 语义（layout 不含时区信息时结果为 UTC——与既有
//     消费形态一致；带时区的变体如 ParseFirstInLocation 留作后续）。
//
// 用法：
//
//	dueOn, ok := timex.ParseFirst(raw, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02")
func ParseFirst(s string, layouts ...string) (time.Time, bool) {
	if s == "" || len(layouts) == 0 {
		return time.Time{}, false
	}
	for _, layout := range layouts {
		if layout == "" {
			return time.Time{}, false
		}
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
