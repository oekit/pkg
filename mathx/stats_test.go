package mathx

import (
	"math"
	"testing"
)

func TestWilsonCI(t *testing.T) {
	lo, hi := WilsonCI(0, 0, 1.96)
	if lo != 0 || hi != 0 {
		t.Fatal("empty total must yield (0,0)")
	}
	// 8/10 → CI 约 [0.490, 0.943]
	lo, hi = WilsonCI(8, 10, 1.96)
	if lo < 0.48 || lo > 0.50 || hi < 0.94 || hi > 0.95 {
		t.Fatalf("Wilson(8,10)=[%v,%v] out of expected range", lo, hi)
	}
	// 全过的小样本：Wilson 上界为 1，但下界 ~0.566——"至少多好"才是诚实口径
	lo, hi = WilsonCI(5, 5, 1.96)
	if hi < 0.999 || lo <= 0.5 {
		t.Fatalf("Wilson(5,5)=[%v,%v]: lower bound must stay honest", lo, hi)
	}
	// 大样本趋近点估计
	lo, hi = WilsonCI(5000, 10000, 1.96)
	if hi-lo > 0.02 {
		t.Fatalf("Wilson(5000,10000) interval too wide: [%v,%v]", lo, hi)
	}
}

// TestWilsonCI_NegativeZNoInversion 钉住 #251：负 z 此前让 half 带符号，
// 产出 lo>hi 的颠倒区间且不报错——下游 `lo <= p && p <= hi` 判据静默全
// false，告警该报不报。归一 |z| 后负 z 与对应正 z 等价，恒有 lo <= hi。
func TestWilsonCI_NegativeZNoInversion(t *testing.T) {
	for _, tt := range []struct {
		name          string
		passed, total int
		z             float64
	}{
		{"issue repro 5/10", 5, 10, -1.96},
		{"boundary 0/10", 0, 10, -1.96},
		{"boundary 10/10", 10, 10, -1.96},
		{"z=0", 5, 10, 0},
		{"z NaN", 5, 10, math.NaN()},
		{"z +Inf", 5, 10, math.Inf(1)},
		{"z -Inf", 5, 10, math.Inf(-1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lo, hi := WilsonCI(tt.passed, tt.total, tt.z)
			if lo > hi {
				t.Fatalf("区间颠倒（#251）: WilsonCI(%d,%d,%v) = (%v, %v)", tt.passed, tt.total, tt.z, lo, hi)
			}
			if lo < 0 || hi > 1 {
				t.Fatalf("区间越出 [0,1]: (%v, %v)", lo, hi)
			}
		})
	}

	// 负 z 与对应正 z 结果逐位一致（归一 |z| 的直接后果）
	loNeg, hiNeg := WilsonCI(5, 10, -1.96)
	loPos, hiPos := WilsonCI(5, 10, 1.96)
	if loNeg != loPos || hiNeg != hiPos {
		t.Fatalf("负 z 应与 |z| 等价: (-1.96)=[%v,%v] vs (1.96)=[%v,%v]", loNeg, hiNeg, loPos, hiPos)
	}
	// issue 复现样本钉值：WilsonCI(5,10,±1.96) ≈ [0.2366, 0.7634]
	if loPos < 0.23 || loPos > 0.24 || hiPos < 0.76 || hiPos > 0.77 {
		t.Fatalf("WilsonCI(5,10,1.96) = [%v,%v], 期望约 [0.2366, 0.7634]", loPos, hiPos)
	}
	// NaN/±Inf z：区间无意义 ⇒ (0,0)
	for _, z := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if lo, hi := WilsonCI(5, 10, z); lo != 0 || hi != 0 {
			t.Fatalf("z=%v 应返回 (0,0), got (%v,%v)", z, lo, hi)
		}
	}
}

func TestMcNemarExact(t *testing.T) {
	cases := []struct {
		n01, n10 int
		want     float64 // 容差 1e-9；-1 = 只验证方向
		sig      bool
	}{
		{0, 0, 1, false},
		{1, 0, 1, false},        // 1 次翻转无意义
		{5, 0, 0.0625, false},   // 5:0 未达 0.05
		{6, 0, 0.03125, true},   // 6:0 显著变差
		{0, 6, 0.03125, true},   // 6:0 显著变好
		{8, 1, 0.0390625, true}, // 8:1 显著
	}
	for _, tc := range cases {
		got := McNemarExact(tc.n01, tc.n10)
		if tc.want >= 0 && math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("McNemar(%d,%d)=%v want %v", tc.n01, tc.n10, got, tc.want)
		}
		if (got < 0.05) != tc.sig {
			t.Fatalf("McNemar(%d,%d)=%v significant=%v want %v", tc.n01, tc.n10, got, got < 0.05, tc.sig)
		}
	}
}
