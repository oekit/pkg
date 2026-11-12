package cron

import (
	"strings"
	"testing"
	"time"
)

// TestWrapSchedule_Next verifies WrapSchedule.Next adds the delay
// to the wrapped schedule's next time.
func TestWrapSchedule_Next(t *testing.T) {
	base := ConstantDelaySchedule{Delay: time.Second}
	wrapped := WrapSchedule{Schedule: base, Delay: 500 * time.Millisecond}

	now := time.Now()
	got := wrapped.Next(now)
	want := base.Next(now).Add(500 * time.Millisecond)
	if !got.Equal(want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

// TestWrapSchedule_UnsatisfiableStaysZero 钉住 #383：不可满足表达式（`0 0 31 2 *`
// ——2 月永无 31 日，#185 的形态）经 WrapSchedule 包装后必须**仍返回零值**
// （哨兵穿透包装器）。此前包装器对零值直接 Add ⇒ 零值变成公元 1 年+delay 的
// 非零时刻 ⇒ 调用方 IsZero 判定失效、time.Until 为极大负值 ⇒ 立即触发、
// prev 停在远古 ⇒ 内层 SpecSchedule 从远古重扫 5 年 ⇒ 调度器热转。
func TestWrapSchedule_UnsatisfiableStaysZero(t *testing.T) {
	spec, err := Parse("0 0 31 2 *") // 2 月 31 日：永不可满足
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local)
	wrapped := WrapSchedule{Schedule: spec, Delay: 500 * time.Millisecond}
	next := wrapped.Next(from)
	if !next.IsZero() {
		t.Fatalf("不可满足表达式经 WrapSchedule 包装后应仍返回零值哨兵, got %v", next)
	}
	// 调度器标准消费形态的防热转推演：IsZero 判定成立 ⇒ 不会把零值当触发点。
	if next.After(from) {
		t.Fatal("零值不应晚于入参")
	}
}

// TestWrapSchedule_Next verifies DelaySchedule.Next adds the
// configured delay to t.
func TestDelaySchedule_Next(t *testing.T) {
	d := NewDelaySchedule(2 * time.Second)
	now := time.Now()
	got := d.Next(now)
	want := now.Add(2 * time.Second)
	if !got.Equal(want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

// TestNewDelaySchedule_StoresDelay verifies the constructor stores
// the delay value.
func TestNewDelaySchedule_StoresDelay(t *testing.T) {
	d := NewDelaySchedule(3 * time.Second)
	if d.Delay != 3*time.Second {
		t.Errorf("expected Delay=3s, got %v", d.Delay)
	}
}

// TestNewRandomDelaySchedule_StoresMinMax verifies the constructor
// stores min and max.
func TestNewRandomDelaySchedule_StoresMinMax(t *testing.T) {
	r := NewRandomDelaySchedule(100*time.Millisecond, 500*time.Millisecond)
	if r.Min != 100*time.Millisecond {
		t.Errorf("expected Min=100ms, got %v", r.Min)
	}
	if r.Max != 500*time.Millisecond {
		t.Errorf("expected Max=500ms, got %v", r.Max)
	}
}

// TestRandomDelaySchedule_Next_WithinRange verifies the random
// delay is always within [Min, Max] bounds.
func TestRandomDelaySchedule_Next_WithinRange(t *testing.T) {
	min, max := 100*time.Millisecond, 500*time.Millisecond
	r := NewRandomDelaySchedule(min, max)
	now := time.Now()

	for i := range 100 {
		got := r.Next(now)
		delay := got.Sub(now)
		if delay < min || delay > max {
			t.Errorf("iteration %d: delay %v outside [%v, %v]", i, delay, min, max)
		}
	}
}

// TestRandomDelaySchedule_Next_ZeroDelta covers the edge case
// where Min == Max (delta == 0) — must return t + Min deterministically.
func TestRandomDelaySchedule_Next_ZeroDelta(t *testing.T) {
	r := NewRandomDelaySchedule(200*time.Millisecond, 200*time.Millisecond)
	now := time.Now()
	got := r.Next(now)
	want := now.Add(200 * time.Millisecond)
	if !got.Equal(want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

// TestNewRandomDelaySchedule_RejectsInvalidRange 钉住 #227：非法区间
// （min > max / 负值）在**构造期** panic——此前 min > max 静默退化固定
// 延迟、负值让 Next 返回过去时刻（原地热转/跳过任务），配置错误无信号。
func TestNewRandomDelaySchedule_RejectsInvalidRange(t *testing.T) {
	cases := []struct {
		name        string
		min, max    time.Duration
		wantPanic   bool
		wantInPanic string
	}{
		{name: "min大于max", min: 500 * time.Millisecond, max: 100 * time.Millisecond, wantPanic: true, wantInPanic: "0 <= min <= max"},
		{name: "min为负", min: -1 * time.Second, max: 100 * time.Millisecond, wantPanic: true, wantInPanic: "0 <= min <= max"},
		{name: "max为负", min: -2 * time.Second, max: -1 * time.Second, wantPanic: true, wantInPanic: "0 <= min <= max"},
		{name: "零区间合法", min: 0, max: 0, wantPanic: false},
		{name: "正常区间合法", min: 100 * time.Millisecond, max: 500 * time.Millisecond, wantPanic: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if tc.wantPanic && r == nil {
					t.Fatalf("NewRandomDelaySchedule(%v, %v) 应 panic", tc.min, tc.max)
				}
				if !tc.wantPanic && r != nil {
					t.Fatalf("合法区间不应 panic, got %v", r)
				}
				if tc.wantPanic {
					msg, ok := r.(string)
					if !ok || !strings.Contains(msg, tc.wantInPanic) {
						t.Fatalf("panic 信息应含 %q, got %v", tc.wantInPanic, r)
					}
				}
			}()
			_ = NewRandomDelaySchedule(tc.min, tc.max)
		})
	}
}

// TestNewDelaySchedule_RejectsNegative 钉住 #227：负延迟（Next 会产生
// 过去时刻）在构造期 panic。
func TestNewDelaySchedule_RejectsNegative(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("NewDelaySchedule(-1s) 应 panic")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "不能为负") {
			t.Fatalf("panic 信息应说明负延迟非法, got %v", r)
		}
	}()
	_ = NewDelaySchedule(-1 * time.Second)
}
