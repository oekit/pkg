package timex

import (
	"testing"
	"time"
)

var cst = time.FixedZone("CST", 8*60*60)

// wantTime 便于在表中书写期望时刻（固定时区语义）。
func wantTime(y int, m time.Month, d, hh, mm, ss, ns int, loc *time.Location) time.Time {
	return time.Date(y, m, d, hh, mm, ss, ns, loc)
}

func TestBeginningOfMinute(t *testing.T) {
	loc := cst
	in := time.Date(2026, 10, 16, 15, 4, 5, 123, loc)
	want := wantTime(2026, 10, 16, 15, 4, 0, 0, loc)
	if got := BeginningOfMinute(in); !got.Equal(want) {
		t.Errorf("BeginningOfMinute() = %v, want %v", got, want)
	}
}

func TestBeginningOfHour(t *testing.T) {
	loc := cst
	in := time.Date(2026, 10, 16, 15, 4, 5, 123, loc)
	want := wantTime(2026, 10, 16, 15, 0, 0, 0, loc)
	if got := BeginningOfHour(in); !got.Equal(want) {
		t.Errorf("BeginningOfHour() = %v, want %v", got, want)
	}
}

func TestBeginningOfDay(t *testing.T) {
	loc := cst
	in := time.Date(2026, 10, 16, 15, 4, 5, 123, loc)
	want := wantTime(2026, 10, 16, 0, 0, 0, 0, loc)
	if got := BeginningOfDay(in); !got.Equal(want) {
		t.Errorf("BeginningOfDay() = %v, want %v", got, want)
	}
}

// TestBeginningOfWeek 覆盖 2026-10-12（周一）～10-18（周日）整周，
// 验证周一起始与周日起始两种约定。
func TestBeginningOfWeek(t *testing.T) {
	loc := cst
	tests := []struct {
		name      string
		in        time.Time
		weekStart time.Weekday
		want      time.Time
	}{
		{"周一-周一起始", wantTime(2026, 10, 12, 10, 0, 0, 0, loc), time.Monday, wantTime(2026, 10, 12, 0, 0, 0, 0, loc)},
		{"周五-周一起始", wantTime(2026, 10, 16, 10, 0, 0, 0, loc), time.Monday, wantTime(2026, 10, 12, 0, 0, 0, 0, loc)},
		{"周六-周一起始", wantTime(2026, 10, 17, 10, 0, 0, 0, loc), time.Monday, wantTime(2026, 10, 12, 0, 0, 0, 0, loc)},
		{"周日-周一起始", wantTime(2026, 10, 18, 10, 0, 0, 0, loc), time.Monday, wantTime(2026, 10, 12, 0, 0, 0, 0, loc)},
		{"周五-周日起始", wantTime(2026, 10, 16, 10, 0, 0, 0, loc), time.Sunday, wantTime(2026, 10, 11, 0, 0, 0, 0, loc)},
		{"周日-周日起始", wantTime(2026, 10, 18, 10, 0, 0, 0, loc), time.Sunday, wantTime(2026, 10, 18, 0, 0, 0, 0, loc)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BeginningOfWeek(tt.in, tt.weekStart); !got.Equal(tt.want) {
				t.Errorf("BeginningOfWeek(%v, %v) = %v, want %v", tt.in, tt.weekStart, got, tt.want)
			}
		})
	}
}

func TestBeginningOfMonth(t *testing.T) {
	loc := cst
	in := time.Date(2026, 10, 16, 15, 4, 5, 123, loc)
	want := wantTime(2026, 10, 1, 0, 0, 0, 0, loc)
	if got := BeginningOfMonth(in); !got.Equal(want) {
		t.Errorf("BeginningOfMonth() = %v, want %v", got, want)
	}
}

// TestBeginningOfQuarter 覆盖四个季度的首月。
func TestBeginningOfQuarter(t *testing.T) {
	loc := cst
	tests := []struct {
		in   time.Time
		want time.Time
	}{
		{wantTime(2026, 1, 31, 10, 0, 0, 0, loc), wantTime(2026, 1, 1, 0, 0, 0, 0, loc)},
		{wantTime(2026, 3, 31, 10, 0, 0, 0, loc), wantTime(2026, 1, 1, 0, 0, 0, 0, loc)},
		{wantTime(2026, 4, 1, 10, 0, 0, 0, loc), wantTime(2026, 4, 1, 0, 0, 0, 0, loc)},
		{wantTime(2026, 9, 30, 10, 0, 0, 0, loc), wantTime(2026, 7, 1, 0, 0, 0, 0, loc)},
		{wantTime(2026, 12, 31, 10, 0, 0, 0, loc), wantTime(2026, 10, 1, 0, 0, 0, 0, loc)},
	}
	for _, tt := range tests {
		if got := BeginningOfQuarter(tt.in); !got.Equal(tt.want) {
			t.Errorf("BeginningOfQuarter(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestBeginningOfYear(t *testing.T) {
	loc := cst
	in := time.Date(2026, 10, 16, 15, 4, 5, 123, loc)
	want := wantTime(2026, 1, 1, 0, 0, 0, 0, loc)
	if got := BeginningOfYear(in); !got.Equal(want) {
		t.Errorf("BeginningOfYear() = %v, want %v", got, want)
	}
}

func TestEndOfMinuteAndHour(t *testing.T) {
	loc := cst
	in := time.Date(2026, 10, 16, 15, 4, 5, 123, loc)
	wantMin := wantTime(2026, 10, 16, 15, 4, 59, int(time.Second)-1, loc)
	if got := EndOfMinute(in); !got.Equal(wantMin) {
		t.Errorf("EndOfMinute() = %v, want %v", got, wantMin)
	}
	wantHour := wantTime(2026, 10, 16, 15, 59, 59, int(time.Second)-1, loc)
	if got := EndOfHour(in); !got.Equal(wantHour) {
		t.Errorf("EndOfHour() = %v, want %v", got, wantHour)
	}
}

func TestEndOfDay(t *testing.T) {
	loc := cst
	in := time.Date(2026, 10, 16, 15, 4, 5, 123, loc)
	want := wantTime(2026, 10, 16, 23, 59, 59, int(time.Second)-1, loc)
	if got := EndOfDay(in); !got.Equal(want) {
		t.Errorf("EndOfDay() = %v, want %v", got, want)
	}
}

func TestEndOfWeek(t *testing.T) {
	loc := cst
	in := wantTime(2026, 10, 16, 10, 0, 0, 0, loc) // 周五
	want := wantTime(2026, 10, 18, 23, 59, 59, int(time.Second)-1, loc)
	if got := EndOfWeek(in, time.Monday); !got.Equal(want) {
		t.Errorf("EndOfWeek(Monday) = %v, want %v", got, want)
	}
	want = wantTime(2026, 10, 17, 23, 59, 59, int(time.Second)-1, loc)
	if got := EndOfWeek(in, time.Sunday); !got.Equal(want) {
		t.Errorf("EndOfWeek(Sunday) = %v, want %v", got, want)
	}
}

// TestEndOfMonth 覆盖大小月与闰月：AddDate 基准取月初（1 号），
// 不受源日期 31 号回退归一化影响。
func TestEndOfMonth(t *testing.T) {
	loc := cst
	tests := []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{"31 天月", wantTime(2026, 10, 16, 10, 0, 0, 0, loc), wantTime(2026, 10, 31, 23, 59, 59, int(time.Second)-1, loc)},
		{"31 号进入 30 天月", wantTime(2026, 3, 31, 10, 0, 0, 0, loc), wantTime(2026, 3, 31, 23, 59, 59, int(time.Second)-1, loc)},
		{"平年二月", wantTime(2026, 2, 5, 10, 0, 0, 0, loc), wantTime(2026, 2, 28, 23, 59, 59, int(time.Second)-1, loc)},
		{"闰年二月", wantTime(2028, 2, 5, 10, 0, 0, 0, loc), wantTime(2028, 2, 29, 23, 59, 59, int(time.Second)-1, loc)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EndOfMonth(tt.in); !got.Equal(tt.want) {
				t.Errorf("EndOfMonth(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestEndOfQuarterAndYear(t *testing.T) {
	loc := cst
	in := wantTime(2026, 10, 16, 10, 0, 0, 0, loc)
	wantQ := wantTime(2026, 12, 31, 23, 59, 59, int(time.Second)-1, loc)
	if got := EndOfQuarter(in); !got.Equal(wantQ) {
		t.Errorf("EndOfQuarter() = %v, want %v", got, wantQ)
	}
	wantY := wantTime(2026, 12, 31, 23, 59, 59, int(time.Second)-1, loc)
	if got := EndOfYear(in); !got.Equal(wantY) {
		t.Errorf("EndOfYear() = %v, want %v", got, wantY)
	}
	// 二季度结束应为 6-30
	inQ2 := wantTime(2026, 4, 1, 10, 0, 0, 0, loc)
	wantQ2 := wantTime(2026, 6, 30, 23, 59, 59, int(time.Second)-1, loc)
	if got := EndOfQuarter(inQ2); !got.Equal(wantQ2) {
		t.Errorf("EndOfQuarter(Q2) = %v, want %v", got, wantQ2)
	}
}

// TestBoundaryKeepsLocation 边界函数不得改变时区（含非整小时偏移时区）。
func TestBoundaryKeepsLocation(t *testing.T) {
	loc := time.FixedZone("LH", 5*60*60+40*60) // +05:40，Truncate 按 UTC 对齐会算错的时区
	in := time.Date(2026, 10, 16, 15, 4, 5, 123, loc)
	got := BeginningOfDay(in)
	if got.Location() != loc {
		t.Errorf("BeginningOfDay() location = %v, want %v", got.Location(), loc)
	}
	want := wantTime(2026, 10, 16, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("BeginningOfDay() = %v, want %v", got, want)
	}
	if got := EndOfDay(in); !got.Equal(wantTime(2026, 10, 16, 23, 59, 59, int(time.Second)-1, loc)) {
		t.Errorf("EndOfDay() = %v, want same-day end in %v", got, loc)
	}
}

// TestBoundaryRoundTrip BeginningOfX/EndOfX 恰好夹住整个周期：
// 区间 [Beginning, End] 包含原时刻，且不含相邻周期的任何整点。
func TestBoundaryRoundTrip(t *testing.T) {
	loc := cst
	in := wantTime(2026, 10, 16, 15, 4, 5, 123, loc)
	cases := []struct {
		name string
		beg  time.Time
		end  time.Time
	}{
		{"day", BeginningOfDay(in), EndOfDay(in)},
		{"week", BeginningOfWeek(in, time.Monday), EndOfWeek(in, time.Monday)},
		{"month", BeginningOfMonth(in), EndOfMonth(in)},
		{"quarter", BeginningOfQuarter(in), EndOfQuarter(in)},
		{"year", BeginningOfYear(in), EndOfYear(in)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !in.After(tc.beg) || !in.Before(tc.end) {
				t.Fatalf("[%v, %v] 不夹住 %v", tc.beg, tc.end, in)
			}
			if !tc.beg.Before(tc.end) {
				t.Fatalf("begin %v 不早于 end %v", tc.beg, tc.end)
			}
		})
	}
}
