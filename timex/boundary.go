package timex

import "time"

// 本文件提供时间段边界函数（BeginningOfX/EndOfX）。
//
// 语义约定：
//   - 全部为纯函数，输入 time.Time 返回新的 time.Time，时区保持不变；
//   - 均基于 time.Date 墙钟重建而非 Truncate（Truncate 按绝对时间对齐，
//     在非整小时偏移时区如 +05:40 会得到错误的墙钟边界）；
//   - EndOfX = 下一周期起点 - 1ns，即 [BeginningOfX, EndOfX] 恰好覆盖
//     整个周期且不与相邻周期重叠；
//   - 周起始日（weekStart）由调用方显式指定（国内习惯 time.Monday，
//     美式习惯 time.Sunday），包内不设全局状态；
//   - 季度按自然季度划分：1-3 月 / 4-6 月 / 7-9 月 / 10-12 月。

// BeginningOfMinute 返回 t 所在分钟的起点（秒与纳秒归零）。
func BeginningOfMinute(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, t.Hour(), t.Minute(), 0, 0, t.Location())
}

// BeginningOfHour 返回 t 所在小时的起点（分/秒/纳秒归零）。
func BeginningOfHour(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, t.Hour(), 0, 0, 0, t.Location())
}

// BeginningOfDay 返回 t 所在日的起点（00:00:00）。
func BeginningOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// BeginningOfWeek 返回 t 所在周的起点（00:00:00）。
// weekStart 指定一周起始日，如 time.Monday。
func BeginningOfWeek(t time.Time, weekStart time.Weekday) time.Time {
	day := BeginningOfDay(t)
	offset := (int(day.Weekday()) - int(weekStart) + 7) % 7
	return day.AddDate(0, 0, -offset)
}

// BeginningOfMonth 返回 t 所在月第一天（1 号）的起点。
func BeginningOfMonth(t time.Time) time.Time {
	y, m, _ := t.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, t.Location())
}

// BeginningOfQuarter 返回 t 所在自然季度首月 1 号的起点。
func BeginningOfQuarter(t time.Time) time.Time {
	y, m, _ := t.Date()
	qm := time.Month((int(m)-1)/3*3 + 1)
	return time.Date(y, qm, 1, 0, 0, 0, 0, t.Location())
}

// BeginningOfYear 返回 t 所在年 1 月 1 日的起点。
func BeginningOfYear(t time.Time) time.Time {
	y, _, _ := t.Date()
	return time.Date(y, time.January, 1, 0, 0, 0, 0, t.Location())
}

// EndOfMinute 返回 t 所在分钟的最后一纳秒。
func EndOfMinute(t time.Time) time.Time {
	return BeginningOfMinute(t).Add(time.Minute - time.Nanosecond)
}

// EndOfHour 返回 t 所在小时的最后一纳秒。
func EndOfHour(t time.Time) time.Time {
	return BeginningOfHour(t).Add(time.Hour - time.Nanosecond)
}

// EndOfDay 返回 t 所在日的最后一纳秒（23:59:59.999999999）。
// 跨夏令时的 23/25 小时日按墙钟日界计算，不按固定 24h 时长。
func EndOfDay(t time.Time) time.Time {
	return BeginningOfDay(t).AddDate(0, 0, 1).Add(-time.Nanosecond)
}

// EndOfWeek 返回 t 所在周（起始日为 weekStart）最后一日的最后一纳秒。
func EndOfWeek(t time.Time, weekStart time.Weekday) time.Time {
	return BeginningOfWeek(t, weekStart).AddDate(0, 0, 7).Add(-time.Nanosecond)
}

// EndOfMonth 返回 t 所在月最后一天的最后一纳秒。
// 计算基准取月初 1 号再走 AddDate 到下月 1 号，天然规避大小月与
// 闰月（2 月 28/29 日）的日期归一化陷阱。
func EndOfMonth(t time.Time) time.Time {
	return BeginningOfMonth(t).AddDate(0, 1, 0).Add(-time.Nanosecond)
}

// EndOfQuarter 返回 t 所在自然季度最后一日的最后一纳秒。
func EndOfQuarter(t time.Time) time.Time {
	return BeginningOfQuarter(t).AddDate(0, 3, 0).Add(-time.Nanosecond)
}

// EndOfYear 返回 t 所在年 12 月 31 日的最后一纳秒。
func EndOfYear(t time.Time) time.Time {
	return BeginningOfYear(t).AddDate(1, 0, 0).Add(-time.Nanosecond)
}
