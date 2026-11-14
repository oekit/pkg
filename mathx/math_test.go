package mathx

import (
	"math"
	"strings"
	"sync"
	"testing"
)

func TestRandIntn(t *testing.T) {
	for range 100 {
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
	for range 100 {
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
	for range 100 {
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

// TestRoundFloorDivisionN_ProductOverflowGuard 钉住 #317：溢出防护必须检查
// **乘积**（x*times / (dividend/divisor)*m），而不是只查 math.Pow10(n)——
// 中间量溢出成 ±Inf 后 math.Round(+Inf)=+Inf，而 ParseFloat("+Inf") 竟解析
// 成功（err=nil），±Inf 被当成合法结果原样放行：原值被静默丢弃替换成 +Inf。
// 触发面不限于天文数字：n=300 时阈值降到 ~1.8e308/10^n，1e9 即可中招。
func TestRoundFloorDivisionN_ProductOverflowGuard(t *testing.T) {
	cases := []struct {
		name string
		got  float64
		want float64
	}{
		{"Round(1e300, 15)", Round(1e300, 15), 1e300},
		{"Round(1e294, 15)", Round(1e294, 15), 1e294},
		{"Round(1e9, 300)", Round(1e9, 300), 1e9},
		{"Round(1e10, 300)", Round(1e10, 300), 1e10},
		{"Floor(1e300, 15)", Floor(1e300, 15), 1e300},
		{"Floor(1e294, 15)", Floor(1e294, 15), 1e294},
		{"Floor(1e9, 300)", Floor(1e9, 300), 1e9},
		{"DivisionN(1e300, 1, 15)", DivisionN(1e300, 1, 15), 1e300},
		{"DivisionN(1e9, 1, 300)", DivisionN(1e9, 1, 300), 1e9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if math.IsInf(c.got, 0) || math.IsNaN(c.got) {
				t.Fatalf("%v = %v：中间量溢出不得放行（want 原值 %v）", c.name, c.got, c.want)
			}
			if c.got != c.want {
				t.Errorf("%v = %v, want %v（溢出时原值返回）", c.name, c.got, c.want)
			}
		})
	}
	// 未溢出边界不误伤：乘积仍在 float64 表示范围内的输入走正常路径，
	// 结果与原值相等或仅差舍入误差。
	sanity := []struct {
		name  string
		got   float64
		exact float64 // 期望值；相对误差 1e-12 内视为通过
	}{
		{"Round(1e293, 15)", Round(1e293, 15), 1e293}, // 1e293×1e15 < MaxFloat64
		{"Round(1e8, 300)", Round(1e8, 300), 1e8},     // 1e8×1e300 < MaxFloat64
		{"Floor(1e293, 15)", Floor(1e293, 15), 1e293},
		{"DivisionN(1e293, 1, 15)", DivisionN(1e293, 1, 15), 1e293},
		{"Round(3.456, 2)", Round(3.456, 2), 3.46},
	}
	for _, c := range sanity {
		t.Run(c.name, func(t *testing.T) {
			if math.IsInf(c.got, 0) || math.IsNaN(c.got) {
				t.Fatalf("%v = %v：未溢出输入不得被误判", c.name, c.got)
			}
			if math.Abs(c.got/c.exact-1) > 1e-12 {
				t.Errorf("%v = %v, want ~%v", c.name, c.got, c.exact)
			}
		})
	}
}

// TestRandIntn_ConcurrentNoSharedLock 钉住 #386：迁移 math/rand/v2 后包级
// 随机源不再有手搓 rngMu——并发调用 -race 下无数据竞争（v2 全局源每
// goroutine 独立状态、无锁），同批调用的取值集合覆盖多个不同值（不是恒同
// 一值的退化）。
func TestRandIntn_ConcurrentNoSharedLock(t *testing.T) {
	const workers, calls = 8, 500
	var seen sync.Map
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range calls {
				v := RandIntn(1000)
				seen.Store(v, true)
			}
		})
	}
	wg.Wait()
	distinct := 0
	seen.Range(func(_, _ any) bool {
		distinct++
		return true
	})
	if distinct < 100 {
		t.Fatalf("并发 4000 次取值只出现 %d 个不同值——随机源疑似退化", distinct)
	}
}

// TestRandSources_Unified 钉住 #386 的双轨制收敛：RangeRandFloat64 与
// Unstable（此前 v1 全局源另一套）如今同源 rand/v2——两者输出都落在各自
// 契约区间且分布非退化。
func TestRandSources_Unified(t *testing.T) {
	u := NewUnstable(0.5)
	var sumF float64
	distinctF, distinctA := map[float64]bool{}, map[int]bool{}
	for range 200 {
		f := RangeRandFloat64(10, 20)
		if f < 10 || f >= 20 {
			t.Fatalf("RangeRandFloat64 越界: %v", f)
		}
		a := u.Around(100)
		if a < 50 || a > 150 {
			t.Fatalf("Unstable.Around 越界: %v", a)
		}
		sumF += f
		distinctF[f] = true
		distinctA[a] = true
	}
	if sumF <= 10*200 {
		t.Fatal("RangeRandFloat64 分布疑似退化（恒等于下界）")
	}
	if len(distinctF) < 50 || len(distinctA) < 50 {
		t.Fatalf("分布疑似退化：distinct(F)=%d distinct(A)=%d", len(distinctF), len(distinctA))
	}
}

func TestDivisionN_NegativePrecisionPanics(t *testing.T) {
	// #467：负精度与 toFixed 同口径 fail-fast——此前静默按负位数计算，
	// n≤-308 时 Pow10 下溢为 0 ⇒ NaN。
	defer func() {
		if recover() == nil {
			t.Fatal("n<0 应 panic(与 toFixed 同口径)")
		}
	}()
	DivisionN(1, 3, -1)
}
