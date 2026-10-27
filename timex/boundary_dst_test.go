package timex

import (
	"testing"
	"time"
)

// dstZones 是本文件用到的两个 DST 边界时区。
//
//   - America/New_York：回拨（同一墙钟小时出现两次）；
//   - America/Santiago：前跳（某个墙钟午夜不存在）。
func dstZones(t *testing.T) (ny, scl *time.Location) {
	t.Helper()
	var err error
	if ny, err = time.LoadLocation("America/New_York"); err != nil {
		t.Skipf("缺少 tzdata（America/New_York）：%v", err)
	}
	if scl, err = time.LoadLocation("America/Santiago"); err != nil {
		t.Skipf("缺少 tzdata（America/Santiago）：%v", err)
	}
	return ny, scl
}

// TestBoundaries_DSTFallBack_BeginningBeforeT_EndAfterT 回归测试：夏令时
// **回拨**（同一墙钟小时出现两次）时，两条不变量必须成立：
//
//	BeginningOfX(t) <= t <= EndOfX(t)
//	BeginningOfX(t) 与 t 落在**同一个墙钟周期**内
//
// 缺陷成因：Go 的 time.Date 对歧义墙钟时间一律解析到较早的那一次 ⇒ 单纯
// 「按墙钟重建」在 t 落在较晚那次时把边界算早一个周期——实测
// EndOfHour(t) 落在 t **之前**（区间 [Begin, End] 连 t 都不含）。
func TestBoundaries_DSTFallBack_BeginningBeforeT_EndAfterT(t *testing.T) {
	ny, _ := dstZones(t)

	// 回拨小时的两次 1:30（EDT = 05:30 UTC、EST = 06:30 UTC）。
	first := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC).In(ny)
	second := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC).In(ny)
	if first.Equal(second) {
		t.Fatal("两次 1:30 应当是不同的瞬间")
	}

	for _, tc := range []struct {
		name string
		at   time.Time
	}{
		{"回拨前（第一次 1:30）", first},
		{"回拨后（第二次 1:30）", second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bh := BeginningOfHour(tc.at)
			eh := EndOfHour(tc.at)
			if bh.After(tc.at) {
				t.Errorf("BeginningOfHour(%v) = %v，晚于输入", tc.at, bh)
			}
			if eh.Before(tc.at) {
				t.Errorf("EndOfHour(%v) = %v，早于输入", tc.at, eh)
			}
			// 同一墙钟小时内。
			if bh.Hour() != tc.at.Hour() || bh.Day() != tc.at.Day() {
				t.Errorf("BeginningOfHour(%v) = %v，不在同一个墙钟小时内", tc.at, bh)
			}
			// 起点与 t 的距离不应超过一小时（此前会差 1.5 小时）。
			if d := tc.at.Sub(bh); d > time.Hour || d < 0 {
				t.Errorf("BeginningOfHour(%v) 距离输入 %v，超出一个小时", tc.at, d)
			}
		})
	}
}

// TestBoundaries_DSTFallBack_Minute 同型：回拨小时内的分钟边界。
func TestBoundaries_DSTFallBack_Minute(t *testing.T) {
	ny, _ := dstZones(t)
	second := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC).In(ny)

	bm := BeginningOfMinute(second)
	em := EndOfMinute(second)
	if bm.After(second) || em.Before(second) {
		t.Errorf("分钟边界越界：begin=%v end=%v at=%v", bm, em, second)
	}
	if d := second.Sub(bm); d > time.Minute || d < 0 {
		t.Errorf("BeginningOfMinute(%v) 距离输入 %v，超出一分钟", second, d)
	}
}

// TestBoundaries_DSTSpringForward_BeginningOfDaySameDate 回归测试：夏令时
// **前跳**（某个墙钟午夜不存在）时，BeginningOfDay 必须落在**同一个日期**。
//
// 缺陷成因：Go 的 time.Date 对不存在的墙钟时间归一到**前一天**的同一时刻
// （实测 time.Date(2026,9,6,0,0,0,0,America/Santiago) = 2026-09-05 23:00）
// ⇒ BeginningOfDay 落到前一天、EndOfDay 早一小时。
func TestBoundaries_DSTSpringForward_BeginningOfDaySameDate(t *testing.T) {
	_, scl := dstZones(t)

	at := time.Date(2026, 9, 6, 10, 0, 0, 0, scl)
	b := BeginningOfDay(at)
	e := EndOfDay(at)

	if b.Day() != at.Day() || b.Month() != at.Month() || b.Year() != at.Year() {
		t.Errorf("BeginningOfDay(%v) = %v，不在同一个日期", at, b)
	}
	if b.After(at) || e.Before(at) {
		t.Errorf("日边界越界：begin=%v end=%v at=%v", b, e, at)
	}
	// 该日只有 23 小时（午夜不存在），结束点必须仍然是当天的最后一刻。
	if e.Day() != at.Day() {
		t.Errorf("EndOfDay(%v) = %v，不在同一个日期", at, e)
	}
}

// TestBoundaries_NonDST_Unchanged 非 DST 时区的既有行为不变（回归护栏）。
func TestBoundaries_NonDST_Unchanged(t *testing.T) {
	loc := time.FixedZone("fixed", 8*3600)
	at := time.Date(2026, 5, 17, 13, 45, 30, 123, loc)

	if got := BeginningOfMinute(at); !got.Equal(time.Date(2026, 5, 17, 13, 45, 0, 0, loc)) {
		t.Errorf("BeginningOfMinute = %v", got)
	}
	if got := BeginningOfHour(at); !got.Equal(time.Date(2026, 5, 17, 13, 0, 0, 0, loc)) {
		t.Errorf("BeginningOfHour = %v", got)
	}
	if got := BeginningOfDay(at); !got.Equal(time.Date(2026, 5, 17, 0, 0, 0, 0, loc)) {
		t.Errorf("BeginningOfDay = %v", got)
	}
	if got := EndOfHour(at); !got.Equal(time.Date(2026, 5, 17, 13, 59, 59, 999999999, loc)) {
		t.Errorf("EndOfHour = %v", got)
	}
	if got := EndOfDay(at); !got.Equal(time.Date(2026, 5, 17, 23, 59, 59, 999999999, loc)) {
		t.Errorf("EndOfDay = %v", got)
	}
}

// TestBoundaries_Invariant_AllFunctions 所有边界函数在若干时刻上都满足
// BeginningOfX(t) <= t <= EndOfX(t)。
func TestBoundaries_Invariant_AllFunctions(t *testing.T) {
	ny, scl := dstZones(t)
	times := []time.Time{
		time.Date(2026, 3, 8, 2, 30, 0, 0, ny),  // 前跳日
		time.Date(2026, 11, 1, 1, 30, 0, 0, ny), // 回拨日
		time.Date(2026, 9, 6, 10, 0, 0, 0, scl), // 午夜不存在的日
		time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	for _, at := range times {
		checks := []struct {
			name  string
			begin time.Time
			end   time.Time
		}{
			{"Minute", BeginningOfMinute(at), EndOfMinute(at)},
			{"Hour", BeginningOfHour(at), EndOfHour(at)},
			{"Day", BeginningOfDay(at), EndOfDay(at)},
			{"Week", BeginningOfWeek(at, time.Monday), EndOfWeek(at, time.Monday)},
			{"Month", BeginningOfMonth(at), EndOfMonth(at)},
			{"Quarter", BeginningOfQuarter(at), EndOfQuarter(at)},
			{"Year", BeginningOfYear(at), EndOfYear(at)},
		}
		for _, c := range checks {
			if c.begin.After(at) {
				t.Errorf("%s: BeginningOf(%v) = %v 晚于输入", c.name, at, c.begin)
			}
			if c.end.Before(at) {
				t.Errorf("%s: EndOf(%v) = %v 早于输入", c.name, at, c.end)
			}
		}
	}
}
