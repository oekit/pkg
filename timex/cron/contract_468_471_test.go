package cron

import (
	"testing"
	"time"
)

// #468：ConstantDelaySchedule 零值/亚秒直构时 Next 曾返回不晚于入参的时刻
// ——违反包级「严格晚于」活性契约，轮询循环原地热转。
func TestConstantDelayScheduleDirectConstructionStrictlyAfter(t *testing.T) {
	cases := []struct {
		name  string
		delay time.Duration
	}{
		{"zero delay", 0},
		{"subsecond delay", 500 * time.Millisecond},
		{"subsecond delay with nanos", 2500 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 扫描整个秒内的纳秒位，任何入参都不得返回 ≤t。
			for ns := int64(0); ns < 100; ns++ {
				from := time.Date(2026, 1, 1, 0, 0, 0, int(ns*10*int64(time.Millisecond)), time.UTC)
				next := ConstantDelaySchedule{Delay: tc.delay}.Next(from)
				if !next.After(from) {
					t.Fatalf("Delay=%v from nanosecond=%d: Next() = %v, not after %v", tc.delay, ns, next, from)
				}
			}
		})
	}
}

// #468：构造器路径对非正 delay fail-fast（对齐 NewDelaySchedule 的口径）。
func TestNewConstantDelayScheduleRejectsNonPositive(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("NewConstantDelaySchedule(%v) should panic", d)
				}
			}()
			NewConstantDelaySchedule(d)
		}()
	}
	if got := NewConstantDelaySchedule(time.Hour); got.Delay != time.Hour {
		t.Fatalf("NewConstantDelaySchedule(1h).Delay = %v", got.Delay)
	}
}

// #469：NewDelaySchedule(0) 曾被放行且 Next 返回入参本身——「不晚于当前
// 时间」的危险判据恰被 0 穿透。
func TestNewDelayScheduleRejectsZero(t *testing.T) {
	func() {
		defer func() {
			if recover() == nil {
				t.Error("NewDelaySchedule(0) should panic")
			}
		}()
		NewDelaySchedule(0)
	}()
	// 直构兜底：Delay<=0 的字面量同样不得打破「严格晚于」。
	zero := DelaySchedule{}
	from := time.Date(2026, 1, 1, 0, 0, 0, 700*int(time.Millisecond), time.UTC)
	if next := zero.Next(from); !next.After(from) {
		t.Fatalf("DelaySchedule{} Next() = %v, not after %v", next, from)
	}
}

// #469：NewRandomDelaySchedule(0,0) 曾被放行且 Next 恒等于入参。
func TestNewRandomDelayScheduleRejectsZeroInterval(t *testing.T) {
	func() {
		defer func() {
			if recover() == nil {
				t.Error("NewRandomDelaySchedule(0, 0) should panic")
			}
		}()
		NewRandomDelaySchedule(0, 0)
	}()
	// min=0 且 jitter 抽到 0 是 delta>0 时唯一可能等于入参的形态：抽样到
	// 覆盖整个 jitter 域，任何一次都不得返回 ≤t。
	s := NewRandomDelaySchedule(0, time.Microsecond)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for range 10000 {
		if next := s.Next(from); !next.After(from) {
			t.Fatalf("Next() = %v, not after %v", next, from)
		}
	}
	// 直构兜底：零值结构体（delta<=0 且 Min<=0）前进到下一整秒界。
	var zero RandomDelaySchedule
	if next := zero.Next(from); !next.After(from) {
		t.Fatalf("zero RandomDelaySchedule Next() = %v, not after %v", next, from)
	}
}

// #470：RoundDuration 负 precision panic（对齐 toFixed 口径）；整数算术
// 路径在大 duration（>2^53 ns）下不丢精度，结果不小于入参。
func TestRoundDurationContract(t *testing.T) {
	func() {
		defer func() {
			if recover() == nil {
				t.Error("RoundDuration with negative precision should panic")
			}
		}()
		RoundDuration(10*time.Second, -time.Second)
	}()
	// 2^53+1 ns 经 float64 中转曾舍到 2^53（结果 < 入参）。
	big := time.Duration(1<<53) + 1
	if got := RoundDuration(big, time.Nanosecond); got < big {
		t.Fatalf("RoundDuration(2^53+1ns, 1ns) = %v < input", got)
	}
	// 负 d 沿用 Ceil 语义（向 +∞ 取整）。
	if got := RoundDuration(-11*time.Second, 2*time.Second); got != -10*time.Second {
		t.Fatalf("RoundDuration(-11s, 2s) = %v, want -10s", got)
	}
	if got := RoundDuration(7*time.Second, 5*time.Second); got != 10*time.Second {
		t.Fatalf("RoundDuration(7s, 5s) = %v, want 10s", got)
	}
}

// #471：WrapSchedule 的负 Delay 曾零校验——inner 严格晚于入参，减掉更大的
// 负 Delay 后仍可 ≤t。
func TestNewWrapScheduleRejectsNegativeDelay(t *testing.T) {
	func() {
		defer func() {
			if recover() == nil {
				t.Error("NewWrapSchedule with negative delay should panic")
			}
		}()
		NewWrapSchedule(ConstantDelaySchedule{Delay: time.Hour}, -2*time.Hour)
	}()
	inner := NewConstantDelaySchedule(time.Hour)
	w := NewWrapSchedule(inner, 10*time.Minute)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if next := w.Next(from); !next.After(from) {
		t.Fatalf("Next() = %v, not after %v", next, from)
	}
}
