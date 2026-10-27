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
//
// # 夏令时（两条不变量）
//
// 本文件的所有边界都遵守两条不变量（无论时区是否处于 DST 切换窗口）：
//
//	BeginningOfX(t) <= t <= EndOfX(t)
//	BeginningOfX(t) 与 t 落在**同一个墙钟周期**内
//
// 达成这两条需要处理两类墙钟歧义：
//
//  1. **回拨**（同一墙钟时刻出现两次，如 America/New_York 的 2026-11-01 的
//     01:xx 出现两次）：Go 的 time.Date 对歧义墙钟时间**一律解析到较早的那
//     一次** ⇒ 单纯「按墙钟重建」在 t 落在较晚那次时会把边界算早一个周期
//     （EndOfHour 曾因此落在 t **之前**）。见 wallHourStart。
//  2. **前跳**（某个墙钟午夜不存在，如 America/Santiago 的 2026-09-06 的
//     00:00 直接跳到 01:00）：Go 的 time.Date 把不存在的墙钟时间归一到
//     **前一天**的同一时刻 ⇒ BeginningOfDay 曾落到**前一天**。见 dayStart。

// wallHourStart 返回 t 所在**墙钟小时**的起点。
//
// 夏令时回拨时同一墙钟小时会出现两次。Go 的 time.Date 对歧义墙钟时间一律
// 解析到较早的那一次 ⇒ 直接按墙钟重建，t 落在较晚那次时起点会被算早一个
// 周期。
//
// 判据：起点 + 一个周期后若仍是**同一墙钟小时** ⇒ 该小时重复了；此时若 t 已
// 落在后一次，则起点取后一次。
func wallHourStart(t time.Time) time.Time {
	y, m, d := t.Date()
	b := time.Date(y, m, d, t.Hour(), 0, 0, 0, t.Location())
	next := b.Add(time.Hour)
	if next.Hour() == b.Hour() && next.Day() == b.Day() &&
		next.Month() == b.Month() && next.Year() == b.Year() {
		if !t.Before(next) {
			b = next
		}
	}
	return b
}

// wallMinuteStart 返回 t 所在**墙钟分钟**的起点。
//
// 分钟落在某个小时内部，重复小时（回拨）时取 t 实际所在的那一次 ⇒ 先定小时
// 起点，再按墙钟分钟偏移。
func wallMinuteStart(t time.Time) time.Time {
	h := wallHourStart(t)
	return h.Add(time.Duration(t.Minute()-h.Minute()) * time.Minute)
}

// dayStart 返回墙钟日期 (y,m,d) 在 loc 中的**首个存在瞬间**。
//
// 夏令时前跳会让某个墙钟午夜不存在（00:00 直接跳到 01:00）。Go 的 time.Date
// 对不存在的墙钟时间会归一到**前一天**的同一时刻（实测
// time.Date(2026,9,6,0,0,0,0,America/Santiago) = 2026-09-05 23:00）⇒ 按墙钟
// 重建的 BeginningOfDay 会落到**前一天**。
//
// 修法：在当天 00:00-23:00 的墙钟小时里找**第一个存在的**那个。
//
// d 可以超出当月天数（如「下个月 1 号」写�� d=1、m+1），内部先归一。
func dayStart(y int, m time.Month, d int, loc *time.Location) time.Time {
	// 先把 (y,m,d) 归一到真实日期——用 UTC 正午构造，避免边界歧义。
	norm := time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
	ny, nm, nd := norm.Date()
	for h := 0; h < 24; h++ {
		c := time.Date(ny, nm, nd, h, 0, 0, 0, loc)
		if cy, cm, cd := c.Date(); cy == ny && cm == nm && cd == nd {
			return c
		}
	}
	// 理论上不可达：一整天里不可能所有墙钟小时都不存在。
	return time.Date(ny, nm, nd, 0, 0, 0, 0, loc)
}

// BeginningOfMinute 返回 t 所在分钟的起点（秒与纳秒归零）。
func BeginningOfMinute(t time.Time) time.Time {
	return wallMinuteStart(t)
}

// BeginningOfHour 返回 t 所在小时的起点（分/秒/纳秒归零）。
func BeginningOfHour(t time.Time) time.Time {
	return wallHourStart(t)
}

// BeginningOfDay 返回 t 所在日的起点（00:00:00）。
//
// 若该日的墙钟午夜不存在（DST 前跳），返回该日**首个存在瞬间**（通常 01:00）。
func BeginningOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return dayStart(y, m, d, t.Location())
}

// BeginningOfWeek 返回 t 所在周的起点（00:00:00）。
// weekStart 指定一周起始日，如 time.Monday。
func BeginningOfWeek(t time.Time, weekStart time.Weekday) time.Time {
	day := BeginningOfDay(t)
	offset := (int(day.Weekday()) - int(weekStart) + 7) % 7
	return dayStart(day.Year(), day.Month(), day.Day()-offset, t.Location())
}

// BeginningOfMonth 返回 t 所在月第一天（1 号）的起点。
func BeginningOfMonth(t time.Time) time.Time {
	y, m, _ := t.Date()
	return dayStart(y, m, 1, t.Location())
}

// BeginningOfQuarter 返回 t 所在自然季度首月 1 号的起点。
func BeginningOfQuarter(t time.Time) time.Time {
	y, m, _ := t.Date()
	qm := time.Month((int(m)-1)/3*3 + 1)
	return dayStart(y, qm, 1, t.Location())
}

// BeginningOfYear 返回 t 所在年 1 月 1 日的起点。
func BeginningOfYear(t time.Time) time.Time {
	y, _, _ := t.Date()
	return dayStart(y, time.January, 1, t.Location())
}

// EndOfMinute 返回 t 所在分钟的最后一纳秒。
func EndOfMinute(t time.Time) time.Time {
	return wallMinuteStart(t).Add(time.Minute - time.Nanosecond)
}

// EndOfHour 返回 t 所在小时的最后一纳秒。
func EndOfHour(t time.Time) time.Time {
	return wallHourStart(t).Add(time.Hour - time.Nanosecond)
}

// EndOfDay 返回 t 所在日的最后一纳秒（23:59:59.999999999）。
// 跨夏令时的 23/25 小时日按**墙钟日界**计算，不按固定 24h 时长。
func EndOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return dayStart(y, m, d+1, t.Location()).Add(-time.Nanosecond)
}

// EndOfWeek 返回 t 所在周（起始日为 weekStart）最后一日的最后一纳秒。
func EndOfWeek(t time.Time, weekStart time.Weekday) time.Time {
	b := BeginningOfWeek(t, weekStart)
	return dayStart(b.Year(), b.Month(), b.Day()+7, t.Location()).Add(-time.Nanosecond)
}

// EndOfMonth 返回 t 所在月最后一天的最后一纳秒。
// 计算基准取月初 1 号再走到下月 1 号，天然规避大小月与
// 闰月（2 月 28/29 日）的日期归一化陷阱。
func EndOfMonth(t time.Time) time.Time {
	y, m, _ := t.Date()
	return dayStart(y, m+1, 1, t.Location()).Add(-time.Nanosecond)
}

// EndOfQuarter 返回 t 所在自然季度最后一日的最后一纳秒。
func EndOfQuarter(t time.Time) time.Time {
	y, m, _ := t.Date()
	qm := time.Month((int(m)-1)/3*3 + 1)
	return dayStart(y, qm+3, 1, t.Location()).Add(-time.Nanosecond)
}

// EndOfYear 返回 t 所在年 12 月 31 日的最后一纳秒。
func EndOfYear(t time.Time) time.Time {
	y, _, _ := t.Date()
	return dayStart(y+1, time.January, 1, t.Location()).Add(-time.Nanosecond)
}
