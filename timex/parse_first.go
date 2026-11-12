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
//     消费形态一致；带时区的变体见 ParseFirstInLocation）。⚠️ 时区**缩写名**
//     layout（CST/MST…）在 UTC 口径下与 time.Parse 差整数小时（缩写解析
//     依赖 Location 的 zone 表，见 ParseFirstInLocation 的 #425 说明）——
//     这类 layout 请用 ParseFirstInLocation 传产线时区。
//
// 用法：
//
//	dueOn, ok := timex.ParseFirst(raw, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02")
func ParseFirst(s string, layouts ...string) (time.Time, bool) {
	// 单行委托（#377）：nil loc 回退 time.UTC 已内建于 ParseFirstInLocation
	// （nil = 显式回退默认值，仓规总则②）——空串校验与首命中循环不再各持
	// 一份，未来加边界规则只改一处。
	return ParseFirstInLocation(s, nil, layouts...)
}

// ParseFirstInLocation 同 ParseFirst，但 layout 不含时区信息时按 loc 解析
// 而非 UTC（#271）。
//
// 面向「服务端的无时区字段实际是服务器本地时区」的平台：Gitee 的
// milestone due_on 混布 RFC3339（带时区）与 "2006-01-02 15:04:05"
// （无时区、服务端按 CST/UTC+8 产出）——按 ParseFirst 的 UTC 口径解析
// 后者会静默偏 8 小时，「按 due 排序提醒」类消费就是 8 小时的误报窗口。
//
// 语义边界：
//
//   - 时区归属**逐 layout 独立判定**（time.ParseInLocation 语义）：layout
//     自带时区**偏移**（如 RFC3339 的 +08:00）时 loc 不参与，解析结果确定；
//     layout 不含时区信息时按 loc 解释墙钟。混合 layout 列表因此安全：
//     带偏移的条目照常精确，无时区的条目落 loc；
//   - ⚠️ **时区缩写名 layout（MST/CST/IST…）是已知陷阱**（#425）：缩写的
//     解析依赖 Location 的 zone 表——`time.Parse` 用 Local（如
//     TZ=Asia/Shanghai 时 CST 表项 → +0800），而本函数传 time.UTC 时按
//     UTC 表解释（无 CST 表项 → 按墙钟落 UTC）⇒ 同一输入差出整数小时，
//     且 CI（TZ=UTC）与开发机（Local=+8）行为不同、CI 恒绿生产偏移。
//     缩写名 layout 必须传入与产线一致的 loc（如 Asia/Shanghai），并自行
//     验证；本包不为此 panic（RFC3339 主流形不受影响，见 #425 拍板）；
//   - loc 为 nil 时回退 time.UTC（与 ParseFirst 同口径）——nil 是显式
//     回退默认值，不算非法（仓规总则②）；经 time.LoadLocation 取 loc
//     失败的调用方应自行处理错误而不是传 nil 装作无事发生；
//   - 其余边界（空输入/空 layout 列表/任一空 layout → (零值, false)、
//     首命中即返回）与 ParseFirst 逐条一致。
//
// ParseFirst 等价于 ParseFirstInLocation(s, time.UTC, layouts...)（无时区
// layout 两者结果完全一致；带时区缩写名的 layout 下两者**时刻不同**——
// 见上面的缩写名陷阱，#425 实测证伪了旧注释「时刻相同」的承诺）。
//
// 用法（Gitee due_on 三 layout，无时区条目按 CST 解析）：
//
//	cst, _ := time.LoadLocation("Asia/Shanghai")
//	dueOn, ok := timex.ParseFirstInLocation(raw, cst, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02")
func ParseFirstInLocation(s string, loc *time.Location, layouts ...string) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	if s == "" || len(layouts) == 0 {
		return time.Time{}, false
	}
	for _, layout := range layouts {
		if layout == "" {
			return time.Time{}, false
		}
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
