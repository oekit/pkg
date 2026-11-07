package mathx

import (
	"strings"
	"testing"
)

func TestRandIntn(t *testing.T) {
	for i := 0; i < 100; i++ {
		v := RandIntn(10)
		if v < 0 || v >= 10 {
			t.Errorf("RandIntn(10) = %v, want [0, 10)", v)
		}
	}
}

func TestDivisionN(t *testing.T) {
	tests := []struct {
		name     string
		dividend float64
		divisor  float64
		n        int
		want     float64
	}{
		{"normal", 10, 3, 2, 3.33},
		{"zero dividend", 0, 3, 2, 0},
		{"zero divisor", 10, 0, 2, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DivisionN(tt.dividend, tt.divisor, tt.n); got != tt.want {
				t.Errorf("DivisionN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRound(t *testing.T) {
	tests := []struct {
		name string
		x    float64
		n    int
		want float64
	}{
		{"round up", 3.456, 2, 3.46},
		{"round down", 3.454, 2, 3.45},
		{"integer", 3.0, 0, 3.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Round(tt.x, tt.n); got != tt.want {
				t.Errorf("Round() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFloor(t *testing.T) {
	if got := Floor(3.999, 2); got != 3.99 {
		t.Errorf("Floor() = %v, want 3.99", got)
	}
}

func TestToFixed(t *testing.T) {
	if got := ToFixed(3.456, 2); got != 3.46 {
		t.Errorf("ToFixed() = %v, want 3.46", got)
	}
}

func TestRangeRandFloat64(t *testing.T) {
	for i := 0; i < 100; i++ {
		v := RangeRandFloat64(1.0, 2.0)
		if v < 1.0 || v >= 2.0 {
			t.Errorf("RangeRandFloat64(1.0, 2.0) = %v, want [1.0, 2.0)", v)
		}
	}
}

func TestProbabilityElems(t *testing.T) {
	elems := []float64{0.1, 0.3, 0.6}
	// precise 是精度基数，须 >= 1（precise<1 时 band 会退化为全 0 ⇒
	// Probability 恒选 index 0）。此前本用例传 0.1，只断言 len(band)==3，
	// 退化状态照样通过——该用例钉不住任何东西。
	band := ProbabilityElems(elems, 1)
	if len(band) != 3 {
		t.Errorf("ProbabilityElems() length = %v, want 3", len(band))
	}
	// band 必须按**调用方给定的顺序**建立（与 elems 同序）。
	want := []int{1, 4, 10}
	for i := range want {
		if band[i] != want[i] {
			t.Fatalf("ProbabilityElems() band = %v, want %v", band, want)
		}
	}
}

func TestProbability(t *testing.T) {
	band := []int{1, 4, 10}
	index, _ := Probability(band)
	if index < 0 || index >= len(band) {
		t.Errorf("Probability() index = %v, want [0, %v)", index, len(band))
	}
}

func TestClamp_Float64(t *testing.T) {
	tests := []struct {
		name string
		v    float64
		lo   float64
		hi   float64
		want float64
	}{
		{"in range", 5.0, 0.0, 10.0, 5.0},
		{"below lo", -1.0, 0.0, 10.0, 0.0},
		{"above hi", 15.0, 0.0, 10.0, 10.0},
		{"at lo boundary", 0.0, 0.0, 10.0, 0.0},
		{"at hi boundary", 10.0, 0.0, 10.0, 10.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Clamp(tt.v, tt.lo, tt.hi); got != tt.want {
				t.Errorf("Clamp(%v, %v, %v) = %v, want %v", tt.v, tt.lo, tt.hi, got, tt.want)
			}
		})
	}
}

func TestClamp_Int(t *testing.T) {
	tests := []struct {
		name string
		v    int
		lo   int
		hi   int
		want int
	}{
		{"in range", 5, 1, 10, 5},
		{"below lo", 0, 1, 10, 1},
		{"above hi", 20, 1, 10, 10},
		// lo == hi 是合法的单点区间：v 无论高低都被钳到该点——钉住
		// #228 的入参校验不得把退化区间误判为非法。
		{"lo == hi degenerate, v in", 5, 5, 5, 5},
		{"lo == hi degenerate, v below", 3, 5, 5, 5},
		{"lo == hi degenerate, v above", 7, 5, 5, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Clamp(tt.v, tt.lo, tt.hi); got != tt.want {
				t.Errorf("Clamp(%v, %v, %v) = %v, want %v", tt.v, tt.lo, tt.hi, got, tt.want)
			}
		})
	}
}

func TestClamp_String(t *testing.T) {
	if got := Clamp("m", "a", "z"); got != "m" {
		t.Errorf("Clamp(\"m\", \"a\", \"z\") = %v, want m", got)
	}
	if got := Clamp("aaa", "b", "z"); got != "b" {
		t.Errorf("Clamp(\"aaa\", \"b\", \"z\") = %v, want b", got)
	}
	if got := Clamp("zzz", "a", "m"); got != "m" {
		t.Errorf("Clamp(\"zzz\", \"a\", \"m\") = %v, want m", got)
	}
}

// TestRoundFloor_OverflowGuard 钉住 #182：n>308（Pow10 溢出）返回原值而非
// NaN/Inf——此前 NaN 经 ParseFloat 竟成功，指标/金额进库不报错。
func TestRoundFloor_OverflowGuard(t *testing.T) {
	for _, n := range []int{400, 309} {
		if got := Round(1.5, n); got != 1.5 {
			t.Fatalf("Round(1.5,%d) = %v, want 1.5（原值）", n, got)
		}
		if got := Floor(1.5, n); got != 1.5 {
			t.Fatalf("Floor(1.5,%d) = %v, want 1.5（原值）", n, got)
		}
	}
	if got := Round(0, 400); got != 0 {
		t.Fatalf("Round(0,400) = %v（此前 NaN）, want 0", got)
	}
}

// TestRandIntn_InvalidMaxPanics 钉住 #229：maxInt <= 0 必须以**带上下文**的
// panic 快速失败，而不是把 math/rand 的裸 panic（"invalid argument to
// Intn"）透传给调用方——同模块 ToFixed 对负精度已确立该纪律。
//
// 成对判据：最小合法入参 maxInt=1 必须照常工作（恒返 0），校验不得误伤。
func TestRandIntn_InvalidMaxPanics(t *testing.T) {
	cases := []struct {
		name   string
		maxInt int
	}{
		{"maxInt 为 0", 0},
		{"maxInt 为 -1", -1},
		{"maxInt 深负", -100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("RandIntn(%d) 应 panic（裸透传 math/rand 的 panic 不可接受）", c.maxInt)
				}
				msg, _ := r.(string)
				if !strings.Contains(msg, "maxInt > 0") {
					t.Fatalf("panic 信息应指明入参约束，got %v", r)
				}
			}()
			_ = RandIntn(c.maxInt)
		})
	}
	for i := 0; i < 100; i++ {
		if v := RandIntn(1); v != 0 {
			t.Fatalf("RandIntn(1) = %d, want 0（最小合法入参不得被校验误伤）", v)
		}
	}
}

// TestClamp_InvertedBoundsPanic 钉住 #228：lo > hi（上下界颠倒，如配置里
// max/min 顺序写反）是编程错误，必须快速失败——此前会静默返回落在
// [hi, lo] 之外的值。覆盖全部三族实例化（int/float64/string）。
func TestClamp_InvertedBoundsPanic(t *testing.T) {
	cases := []struct {
		name   string
		invoke func()
	}{
		{"int lo > hi", func() { _ = Clamp(5, 10, 1) }},
		{"float64 lo > hi", func() { _ = Clamp(5.0, 10.0, 1.0) }},
		{"string lo > hi", func() { _ = Clamp("m", "z", "a") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("Clamp 传入 lo > hi 应 panic（静默产生界外值更危险）")
				}
				msg, _ := r.(string)
				if !strings.Contains(msg, "lo <= hi") {
					t.Fatalf("panic 信息应指明边界约束，got %v", r)
				}
			}()
			c.invoke()
		})
	}
}
