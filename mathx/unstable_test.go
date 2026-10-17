package mathx

import (
	"testing"
	"time"
)

func TestNewUnstable_ClampsDeviation(t *testing.T) {
	tests := []struct {
		name      string
		deviation float64
		want      float64
	}{
		{"negative to zero", -0.5, 0},
		{"in range kept", 0.25, 0.25},
		{"one kept", 1, 1},
		{"above one to one", 3.7, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewUnstable(tt.deviation).deviation; got != tt.want {
				t.Errorf("NewUnstable(%v).deviation = %v, want %v", tt.deviation, got, tt.want)
			}
		})
	}
}

func TestUnstable_ZeroDeviationIdentity(t *testing.T) {
	u := NewUnstable(0)
	if got := u.Around(42); got != 42 {
		t.Errorf("Around(42) = %d, want 42", got)
	}
	if got := u.AroundDuration(5 * time.Second); got != 5*time.Second {
		t.Errorf("AroundDuration(5s) = %v, want 5s", got)
	}
	if got := u.AroundFloat64(3.14); got != 3.14 {
		t.Errorf("AroundFloat64(3.14) = %v, want 3.14", got)
	}
}

// TestUnstable_Bounds 界检查：deviation=0.5、v=100 时结果必须落在
// [50,150]。rand.Float64 不取到上界，此处用闭区间断言保持稳健。
func TestUnstable_Bounds(t *testing.T) {
	u := NewUnstable(0.5)
	for i := 0; i < 10000; i++ {
		if got := u.Around(100); got < 50 || got > 150 {
			t.Fatalf("Around(100) = %d, out of [50,150]", got)
		}
		if got := u.AroundFloat64(100); got < 50 || got > 150 {
			t.Fatalf("AroundFloat64(100) = %v, out of [50,150]", got)
		}
	}
}

func TestUnstable_AroundDuration(t *testing.T) {
	u := NewUnstable(0.2)
	base := 30 * time.Minute
	lo, hi := time.Duration(0.8*float64(base)), time.Duration(1.2*float64(base))
	for i := 0; i < 1000; i++ {
		got := u.AroundDuration(base)
		if got < lo || got > hi {
			t.Fatalf("AroundDuration(30m) = %v, out of [%v, %v]", got, lo, hi)
		}
	}
}

// TestUnstable_Spread 结果确实在抖动：连续采样不应恒等于基准值
// （deviation>0 且样本量足够时，恒等的概率可忽略；rand 无注入点，
// 断言稳定性依赖连续性而非具体序列）。
func TestUnstable_Spread(t *testing.T) {
	u := NewUnstable(0.5)
	seen := map[float64]bool{}
	for i := 0; i < 100; i++ {
		seen[u.AroundFloat64(100)] = true
	}
	if len(seen) < 90 {
		t.Errorf("expected jittered spread, got %d distinct values in 100 samples", len(seen))
	}
}

func TestUnstable_UpwardZeroDeviationIdentity(t *testing.T) {
	u := NewUnstable(0)
	if got := u.Upward(42); got != 42 {
		t.Errorf("Upward(42) = %d, want 42", got)
	}
	if got := u.UpwardDuration(5 * time.Second); got != 5*time.Second {
		t.Errorf("UpwardDuration(5s) = %v, want 5s", got)
	}
	if got := u.UpwardFloat64(3.14); got != 3.14 {
		t.Errorf("UpwardFloat64(3.14) = %v, want 3.14", got)
	}
}

// TestUnstable_UpwardBounds 单边上界检查：deviation=0.5、v=100 时结果必须
// 落在 [100,150]——下界恒为基准（「不得早于基准」是本变体的存在理由）。
func TestUnstable_UpwardBounds(t *testing.T) {
	u := NewUnstable(0.5)
	for i := 0; i < 10000; i++ {
		if got := u.Upward(100); got < 100 || got > 150 {
			t.Fatalf("Upward(100) = %d, out of [100,150]", got)
		}
		if got := u.UpwardFloat64(100); got < 100 || got > 150 {
			t.Fatalf("UpwardFloat64(100) = %v, out of [100,150]", got)
		}
	}
}

func TestUnstable_UpwardDuration(t *testing.T) {
	u := NewUnstable(0.2)
	base := 30 * time.Minute
	lo, hi := base, time.Duration(1.2*float64(base))
	for i := 0; i < 1000; i++ {
		got := u.UpwardDuration(base)
		if got < lo || got > hi {
			t.Fatalf("UpwardDuration(30m) = %v, out of [%v,%v]", got, lo, hi)
		}
	}
}

// TestUnstable_UpwardSpread 覆盖率检查：dev=0.5 时 [100,150) 区间应被
// 均匀覆盖（桶 20..29 全命中；rand.Float64 不取到 1.0，整值 150 不可达，
// 与 AroundBounds 的闭区间注释同理），防止实现退化成恒等或只取端点。
func TestUnstable_UpwardSpread(t *testing.T) {
	u := NewUnstable(0.5)
	seen := make(map[int]bool)
	for i := 0; i < 5000; i++ {
		seen[u.Upward(100)/5] = true // 100..149 → 桶 20..29
	}
	for bucket := 20; bucket <= 29; bucket++ {
		if !seen[bucket] {
			t.Errorf("bucket %d never hit — distribution not uniform over [100,150)", bucket)
		}
	}
}
