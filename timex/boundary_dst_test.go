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

// —— #418：非整小时 DST 位移时区（30 分钟回拨）——

// TestBoundaries_DSTFallBack_HalfHourShift 封堵 #418：Lord Howe/Macquarie
// 回拨 30 分钟（02:00(+11) → 01:30(+10:30)），墙钟「01」小时跨两个 offset、
// 「02」点的墙钟序列错位。旧 EndOfHour/EndOfMinute 按「起点 + 固定周期」
// 计算，实测 EndOfHour 与下一小时的 BeginningOfHour 重叠近 30 分钟。
// 修复后 EndOfX = 下一周期起点 −1ns，三条不变量在此类时区同样成立。
func TestBoundaries_DSTFallBack_HalfHourShift(t *testing.T) {
	loc, err := time.LoadLocation("Australia/Lord_Howe")
	if err != nil {
		t.Skipf("tzdata 缺 Lord_Howe: %v", err)
	}
	// 2026-04-05 Lord Howe 回拨日：02:00(+11) → 01:30(+10:30)。
	// 扫回拨窗口前后每一分钟（含第一次 01:00-01:29(+11) 与第二次
	// 01:30-01:59(+10:30) 两个「01」出现）。
	base := time.Date(2026, 4, 5, 0, 0, 0, 0, loc)
	for i := 0; i < 3*60; i++ {
		at := base.Add(time.Duration(i) * time.Minute)

		if eoh := EndOfHour(at); eoh.Before(at) {
			t.Fatalf("EndOfHour(%v) = %v 早于输入", at, eoh)
		} else if next := BeginningOfHour(at.Add(time.Hour)); !eoh.Before(next) && wallHourStart(at) != wallHourStart(next) {
			// EndOfHour 不得越过下一墙钟小时的起点（同属自己周期内的
			// 重复小时不算越界——Begin/End 落同一墙钟小时）。
			t.Fatalf("★ EndOfHour(%v) = %v 越入下一墙钟小时(起点 %v)——#418 重叠回归", at, eoh, next)
		}

		if eom := EndOfMinute(at); eom.Before(at) {
			t.Fatalf("EndOfMinute(%v) = %v 早于输入", at, eom)
		} else if next := BeginningOfMinute(at.Add(time.Minute)); !eom.Before(next) && wallMinuteStart(at) != wallMinuteStart(next) {
			t.Fatalf("★ EndOfMinute(%v) = %v 越入下一墙钟分钟——#418 同族", at, eom)
		}
	}

	// 原始探针场景回归：第二次「01」出现（01:45 +10:30）的 EndOfHour 必须
	// 停在自己周期内（旧实现落到 02:29:59.999）。
	second := time.Date(2026, 4, 5, 1, 45, 0, 0, loc)
	if eoh := EndOfHour(second); !eoh.Before(time.Date(2026, 4, 5, 2, 0, 0, 0, loc)) {
		t.Fatalf("★ EndOfHour(01:45 回拨日) = %v 落进 02 点——重叠未修复", eoh)
	}

	// Macquarie 同型（同为 30 分钟回拨）。
	mq, err := time.LoadLocation("Australia/Macquarie")
	if err != nil {
		return // tzdata 缺则跳过，Lord Howe 已覆盖判据
	}
	mqBase := time.Date(2026, 4, 4, 0, 0, 0, 0, mq)
	for i := 0; i < 3*60; i++ {
		at := mqBase.Add(time.Duration(i) * time.Minute)
		if eoh := EndOfHour(at); eoh.Before(at) {
			t.Fatalf("Macquarie: EndOfHour(%v) = %v 早于输入", at, eoh)
		}
	}
}
