package cron

import (
	"testing"
	"time"
)

// 本文件覆盖 Next 在夏令时切换日的推进（[#214] 及同源的「前跳」变体）。
//
// 两类墙钟异常，Go time.Date 对它们的行为都是**把时刻挪到别处**：
//
//	回拨（同一墙钟出现两次）⇒ 一律解析到**较早**那次；
//	前跳（某个墙钟不存在）  ⇒ 归一到**前一天**同一时刻附近。
//
// 原先的推进写法是「Add(1h/1m/1d) 后按墙钟重建」，两条都会中招：回拨时
// 绝对时间倒退 ⇒ Next 返回早于入参的时刻；前跳时重建把日期留在前一天 ⇒
// 日期循环原地打转、Next 死循环。

func mustSpec(t *testing.T, spec, loc string) *SpecSchedule {
	t.Helper()
	s, err := Parse(spec)
	if err != nil {
		t.Fatalf("Parse(%q) 报错：%v", spec, err)
	}
	ss, ok := s.(*SpecSchedule)
	if !ok {
		t.Fatalf("Parse(%q) 应返回 *SpecSchedule，实得 %T", spec, s)
	}
	l, err := time.LoadLocation(loc)
	if err != nil {
		t.Skipf("时区 %s 不可用：%v", loc, err)
	}
	ss.Location = l
	return ss
}

// TestNext_DSTFallBack_ReturnsLaterOccurrence 回归 #214 的核心断言。
//
// America/New_York 2026-11-01：02:00 EDT 回拨到 01:00 EST，墙钟 01:00–01:59
// 出现两次。从第一段的 01:59:00 EDT 出发，`0 0 * * * *`（每小时整点）的下一次
// 触发是**第二段的 01:00:00 EST**，绝对时间只前进 1 分钟。
//
// 缺陷形态：按墙钟重建把它归一到 01:00:00 **EDT**（早 59 分钟）⇒ Next 返回
// 过去时刻，调度器的 `for { next := Next(prev) }` 立即热循环且永不前进。
func TestNext_DSTFallBack_ReturnsLaterOccurrence(t *testing.T) {
	sched := mustSpec(t, "0 0 * * * *", "America/New_York")

	// 2026-11-01 01:59:00 -0400 (EDT)
	firstPass := time.Date(2026, 11, 1, 1, 59, 0, 0, sched.Location)
	if name, _ := firstPass.Zone(); name != "EDT" {
		t.Fatalf("夹具前提不成立：01:59 应落在回拨前一段（EDT），实得 %s", name)
	}

	got := sched.Next(firstPass)

	// 期望：第二段的 01:00:00 -0500 (EST)，即绝对时间 +1 分钟
	want := firstPass.Add(time.Minute)
	if !got.Equal(want) {
		t.Fatalf("Next(%v) = %v（unix %d），want %v（unix %d）——差 %v",
			firstPass, got, got.Unix(), want, want.Unix(), got.Sub(want))
	}
	if name, _ := got.Zone(); name != "EST" {
		t.Fatalf("期望落在第二段（EST），实得 %s——按墙钟重建会归一到较早那次（EDT）", name)
	}
	if !got.After(firstPass) {
		t.Fatalf("Next 必须严格晚于入参：%v !> %v", got, firstPass)
	}
}

// TestNext_DSTFallBack_HourFieldCrossing 小时字段受限时，回拨窗口内的推进
// 也会踩到同一个坑：从第一段的 02:00 EDT 推「下一个 03:00」时，绝对时间先
// 走到第二段的 01:00 EST，按墙钟重建却被归一回 01:00 **EDT** ⇒ 绝对时间倒退，
// 循环再次推进到 01:00 EST、再被重建回 01:00 EDT ⇒ **Next 内部死循环**
// （比返回错值更糟：调用方拿不到任何返回）。
func TestNext_DSTFallBack_HourFieldCrossing(t *testing.T) {
	sched := mustSpec(t, "0 0 3 * * *", "America/New_York") // 每天 03:00

	prev := time.Date(2026, 11, 1, 1, 30, 0, 0, sched.Location)
	if name, _ := prev.Zone(); name != "EDT" {
		t.Fatalf("夹具前提不成立：01:30 应在回拨前一段（EDT），实得 %s", name)
	}

	done := make(chan time.Time, 1)
	go func() { done <- sched.Next(prev) }()

	var got time.Time
	select {
	case got = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Next 在回拨窗口内死循环（3s 未返回）")
	}

	want := time.Date(2026, 11, 1, 3, 0, 0, 0, sched.Location)
	if !got.Equal(want) {
		t.Fatalf("Next(%v) = %v，want %v", prev, got, want)
	}
	if name, _ := got.Zone(); name != "EST" {
		t.Fatalf("03:00 应落在 EST，实得 %s", name)
	}
}

// TestNext_DSTFallBack_NeverStalls 回拨窗口内连续求值必须**严格单调前进**，
// 并能在有限步内走出重复小时（缺陷形态是恒等：Next(x) == x）。
func TestNext_DSTFallBack_NeverStalls(t *testing.T) {
	sched := mustSpec(t, "0 0 * * * *", "America/New_York")
	transition := time.Date(2026, 11, 1, 2, 0, 0, 0, sched.Location).Unix() // 回拨时刻

	cur := time.Date(2026, 11, 1, 1, 30, 0, 0, sched.Location)
	seen := map[int64]bool{cur.Unix(): true}
	for i := 0; i < 50; i++ {
		next := sched.Next(cur)
		if next.IsZero() {
			t.Fatalf("第 %d 次迭代返回零值", i)
		}
		if !next.After(cur) {
			t.Fatalf("第 %d 次迭代未前进：Next(%v) = %v（回拨窗口内活锁）", i, cur, next)
		}
		if seen[next.Unix()] {
			t.Fatalf("第 %d 次迭代回到已触发过的时刻 %v——调度器会重复执行", i, next)
		}
		seen[next.Unix()] = true
		cur = next
		if cur.Unix() > transition+3600 {
			return // 已走出回拨小时
		}
	}
	t.Fatalf("50 次迭代仍未走出回拨窗口，最后停在 %v（transition=%d）", cur, transition)
}

// TestNext_DSTSpringForward_MidnightGap 前跳变体：America/Santiago 2026-09-06
// 的 00:00 不存在（直接跳到 01:00）。
//
// 缺陷形态比回拨更烈：`time.Date(2026,9,6,0,0,0,0,santiago)` 返回
// **09-05 23:00**（实测），而 `AddDate(0,0,1)` 从 09-05 00:00 出发也落到
// 09-05 23:00 ⇒ 日期循环原地打转、Next **永不返回**（不是返回错值，是死循环）。
//
// 只在**日期字段受限**时才会进日期循环——`0 0 0 * * *` 的 dayMatches 恒真，
// 根本走不到那里，所以用「只在 9 月 6 日」这种受限写法触发。
func TestNext_DSTSpringForward_MidnightGap(t *testing.T) {
	sched := mustSpec(t, "0 0 0 6 9 *", "America/Santiago") // 每年 9/6 00:00

	prev := time.Date(2026, 9, 5, 0, 0, 0, 0, sched.Location)

	done := make(chan time.Time, 1)
	go func() { done <- sched.Next(prev) }()

	var got time.Time
	select {
	case got = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Next 在前跳日死循环（3s 未返回）——日期循环把 09-06 归一回 09-05 后原地打转")
	}

	// 该日 00:00 不存在 ⇒ 当年这一次不触发，顺延到表达式下一次命中（次年
	// 9/6 00:00 存在）。关键字面：**必须终止**，且不得返回过去时刻。
	// 顺带钉住现状：00:00 不存在的日子**不**补触发到 01:00——「按墙钟语义，
	// 该时刻当天不存在」比「顺延到首个存在的整点」更不容易让人意外。若将来
	// 改成顺延语义，本条会红，那时是**有意的行为变更**，请连带改 doc.go。
	want := time.Date(2027, 9, 6, 0, 0, 0, 0, sched.Location)
	if !got.Equal(want) {
		t.Fatalf("Next(%v) = %v，want %v", prev, got, want)
	}
}

// TestNext_DSTSpringForward_DailySkipsGapDay 日常形态下的前跳：`0 0 0 * * *`
// 在 00:00 不存在的日子**跳过当次**、次日正常。这条钉的是「跳过而非死循环
// 也不是连跳两天」。
func TestNext_DSTSpringForward_DailySkipsGapDay(t *testing.T) {
	sched := mustSpec(t, "0 0 0 * * *", "America/Santiago")
	prev := time.Date(2026, 9, 5, 0, 0, 0, 0, sched.Location)

	done := make(chan time.Time, 1)
	go func() { done <- sched.Next(prev) }()

	var got time.Time
	select {
	case got = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Next 在前跳日死循环")
	}
	if got.IsZero() {
		t.Fatal("日常表达式不应返回零值")
	}
	want := time.Date(2026, 9, 7, 0, 0, 0, 0, sched.Location)
	if !got.Equal(want) {
		t.Fatalf("Next(%v) = %v，want %v（09-06 的 00:00 不存在 ⇒ 跳过当次）", prev, got, want)
	}
}

// TestNext_MonotonicAcrossTransitions 抽样多条表达式 × 两个时区，在切换日
// 前后各推进 500 次：必须严格单调、非零，并在合理步数内跨过切换点。
func TestNext_MonotonicAcrossTransitions(t *testing.T) {
	cases := []struct {
		spec, loc string
		start     time.Time
	}{
		{"0 0 * * * *", "America/New_York", time.Date(2026, 10, 31, 22, 0, 0, 0, time.UTC)},
		{"0 */15 * * * *", "America/New_York", time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)},
		{"0 0 0 * * *", "America/New_York", time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)},
		{"0 0 0 * * *", "America/Santiago", time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)},
		{"0 0 * * * *", "America/Santiago", time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)},
		{"0 30 2 * * *", "America/New_York", time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		t.Run(c.spec+"@"+c.loc, func(t *testing.T) {
			sched := mustSpec(t, c.spec, c.loc)
			cur := c.start.In(sched.Location)
			for i := 0; i < 500; i++ {
				next := sched.Next(cur)
				if next.IsZero() {
					t.Fatalf("第 %d 次迭代返回零值（起点 %v）", i, cur)
				}
				if !next.After(cur) {
					t.Fatalf("第 %d 次迭代未前进：Next(%v) = %v", i, cur, next)
				}
				cur = next
			}
		})
	}
}

// TestFirstExistingOnDay 直接钉住 DST 辅助件的语义：目标日的最早存在时刻。
//
// 这是前跳缺陷的**根**：Go 的 time.Date 对不存在的凌晨返回**前一天 23:00**
// （实测），任何「按墙钟归一到当日 0 点」的写法都会把日期留在前一天。
func TestFirstExistingOnDay(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("时区不可用：%v", err)
	}
	st, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Skipf("时区不可用：%v", err)
	}

	// 普通日：就是 00:00
	if got := firstExistingOnDay(2026, 6, 15, ny); got.Hour() != 0 || got.Day() != 15 {
		t.Fatalf("普通日应返回当日 00:00，实得 %v", got)
	}

	// 前跳日（00:00 不存在）：必须落在当日，且是该日最早存在的整分
	got := firstExistingOnDay(2026, 9, 6, st)
	if got.Day() != 6 || got.Month() != time.September {
		t.Fatalf("前跳日应仍落在 09-06，实得 %v（time.Date 会归一到前一天）", got)
	}
	if got.Hour() != 1 || got.Minute() != 0 {
		t.Fatalf("前跳日最早存在的时刻应是 01:00，实得 %v", got)
	}
}

// TestNextMonthStart 月推进必须落到「下一个月 1 日」，且跨年前进正常。
func TestNextMonthStart(t *testing.T) {
	ny := time.UTC
	cases := []struct {
		from time.Time
		want time.Time
	}{
		{time.Date(2026, 1, 31, 23, 59, 0, 0, ny), time.Date(2026, 2, 1, 0, 0, 0, 0, ny)},
		{time.Date(2026, 12, 15, 0, 0, 0, 0, ny), time.Date(2027, 1, 1, 0, 0, 0, 0, ny)},
		{time.Date(2026, 2, 1, 0, 0, 0, 0, ny), time.Date(2026, 3, 1, 0, 0, 0, 0, ny)},
	}
	for _, c := range cases {
		if got := nextMonthStart(c.from); !got.Equal(c.want) {
			t.Fatalf("nextMonthStart(%v) = %v，want %v", c.from, got, c.want)
		}
	}
}
