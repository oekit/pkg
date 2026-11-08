package timex

import (
	"testing"
	"time"
)

// withLocal 临时替换 time.Local（Date2Stamp 固定用本地时区解析），测试结束
// 恢复。本包测试串行执行（无 t.Parallel），替换是安全的。
func withLocal(t *testing.T, loc *time.Location) {
	t.Helper()
	old := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = old })
}

// TestDate2Stamp_DSTSpringForward_NoDayShift 回归（[#261]）：DST 前跳日的
// 00:00:00 这个墙钟时刻**不存在**（America/Santiago 2026-09-06 从 00:00
// 直接跳到 01:00），Go 的 ParseInLocation 把结果归一到**前一天** 23:00——
// 时间戳偏移一整天且解析不报错。GetCurrentDayStartTime 走的正是这条路径：
// 前跳日当天「今天 00:00」拿到的是昨天 ⇒ 日切统计、日限额、日报窗口全部
// 偏移一整天。同一个坑在 boundary.go 的 dayStart 已修过一次，本组用例
// 钉住 Date2Stamp 对齐同一口径：**回读的墙钟日期必须与输入同一天**。
func TestDate2Stamp_DSTSpringForward_NoDayShift(t *testing.T) {
	_, scl := dstZones(t)
	withLocal(t, scl)

	const layout = "2006-01-02 15:04:05"
	for _, tc := range []struct {
		name string
		in   string
	}{
		{"前跳缺口内 00:00:00", "2026-09-06 00:00:00"},
		{"前跳缺口内 00:30:00", "2026-09-06 00:30:00"},
		{"前跳日缺口后 01:00:00", "2026-09-06 01:00:00"},
		{"前跳日正常时刻 12:00:00", "2026-09-06 12:00:00"},
		{"前跳日前一天（正常日）", "2026-09-05 00:00:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stamp := Date2Stamp(tc.in, layout)
			if stamp == 0 {
				t.Fatal("Date2Stamp returned 0")
			}
			back := StampInt2Date(stamp, layout)
			if back[:10] != tc.in[:10] {
				t.Fatalf("Date2Stamp(%q) = %d，回读 %q：日期被归一挪走（前一天）", tc.in, stamp, back)
			}
		})
	}

	// #261 复现样本的精确值：缺口时刻锚定「目标日首个存在瞬间」——
	// 2026-09-06 00:00:00 应落到当天 01:00（=1788667200），而不是
	// 归一到 2026-09-05 23:00（=1788663600，缺陷值）。
	if got := Date2Stamp("2026-09-06 00:00:00", layout); got != time.Date(2026, 9, 6, 1, 0, 0, 0, scl).Unix() {
		t.Fatalf("Date2Stamp(2026-09-06 00:00:00) = %d, want 当天首个存在瞬间 %d",
			got, time.Date(2026, 9, 6, 1, 0, 0, 0, scl).Unix())
	}
	// 缺口内的 00:30 补回时刻偏移：落到当天 01:30（缺口之后）。
	if got := Date2Stamp("2026-09-06 00:30:00", layout); got != time.Date(2026, 9, 6, 1, 0, 0, 0, scl).Add(30*time.Minute).Unix() {
		t.Fatalf("Date2Stamp(2026-09-06 00:30:00) = %d, want 缺口顺延后的 01:30 %d",
			got, time.Date(2026, 9, 6, 1, 0, 0, 0, scl).Add(30*time.Minute).Unix())
	}
	// 正常日的既有行为零变化。
	if got, want := Date2Stamp("2026-09-05 00:00:00", layout), time.Date(2026, 9, 5, 0, 0, 0, 0, scl).Unix(); got != want {
		t.Fatalf("Date2Stamp(2026-09-05 00:00:00) = %d, want %d（正常日行为不得变化）", got, want)
	}
}

// TestDate2Stamp_DSTFallBackAndGap_SameDayUnchanged 回拨与前跳（缺口不在
// 午夜的时区，如 America/New_York 02:00→03:00）不触发日期修正——Go 对
// 回拨取较早一次、对缺口取偏移前一侧，日期都不会被挪走，既有约定保持。
func TestDate2Stamp_DSTFallBackAndGap_SameDayUnchanged(t *testing.T) {
	ny, _ := dstZones(t)
	withLocal(t, ny)

	const layout = "2006-01-02 15:04:05"
	for _, tc := range []struct {
		name string
		in   string
	}{
		{"回拨重复墙钟第一次", "2026-11-01 01:30:00"},
		{"回拨日正常时刻", "2026-11-01 15:00:00"},
		{"前跳缺口内 02:30（同日归一）", "2026-03-08 02:30:00"},
		{"前跳日正常时刻", "2026-03-08 12:00:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stamp := Date2Stamp(tc.in, layout)
			if stamp == 0 {
				t.Fatal("Date2Stamp returned 0")
			}
			back := StampInt2Date(stamp, layout)
			if back[:10] != tc.in[:10] {
				t.Fatalf("Date2Stamp(%q) = %d，回读 %q：日期被挪走", tc.in, stamp, back)
			}
		})
	}
}

// TestGetCurrentDayStartTime_MatchesBeginningOfDay 钉住 issue 点名的下游
// （[#261]）：GetCurrentDayStartTime 经 Date2Stamp 走字符串绕一圈，必须与
// 同包 dayStart 口径的 BeginningOfDay(time.Now()) 完全一致——包括 DST 边
// 界日。此前 Santiago 前跳日当天两者会差出一整天。
func TestGetCurrentDayStartTime_MatchesBeginningOfDay(t *testing.T) {
	_, scl := dstZones(t)
	withLocal(t, scl)

	if got, want := GetCurrentDayStartTime(), BeginningOfDay(time.Now()).Unix(); got != want {
		t.Fatalf("GetCurrentDayStartTime() = %d, want BeginningOfDay(now) = %d（两条路径必须同口径）", got, want)
	}
}

// TestDate2Stamp_ZoneInLayout_NoLocalAmbiguity layout 含时区元素时，时刻由
// 输入串里的偏移决定，不存在本地墙钟歧义——修正跳过，按解析结果返回。
func TestDate2Stamp_ZoneInLayout_NoLocalAmbiguity(t *testing.T) {
	_, scl := dstZones(t)
	withLocal(t, scl)

	const layout = "2006-01-02 15:04:05Z07:00"
	got := Date2Stamp("2026-09-06 00:00:00-03:00", layout)
	if want := time.Date(2026, 9, 6, 3, 0, 0, 0, time.UTC).Unix(); got != want {
		t.Fatalf("Date2Stamp with zone layout = %d, want %d（偏移在串里，不该被本地修正改写）", got, want)
	}
}
