package mathx

import (
	"math"
	"testing"
)

// TestProbabilityElems_BandFollowsCallerOrder 回归测试：band 必须按**调用方
// 给定的顺序**建立（与 elems 同序）。
//
// 缺陷成因：ProbabilityElems 内部排序（排序副本）后**丢弃了置换关系** ⇒
// 返回的 band 对应排序后的顺序，而 Probability 返回的是那个顺序里的下标 ⇒
// 调用方拿它索引自己的 elems 会选错元素（权重被静默置换）。
//
// 实测（修前）：elems=[0.6,0.1,0.3] → band=[10,40,100]（对应排序后的
// [0.1,0.3,0.6]）；按调用方顺序应为 [60,70,100]。
func TestProbabilityElems_BandFollowsCallerOrder(t *testing.T) {
	elems := []float64{0.6, 0.1, 0.3}
	band := ProbabilityElems(elems, 10)
	want := []int{60, 70, 100}
	for i := range want {
		if band[i] != want[i] {
			t.Fatalf("ProbabilityElems(%v) = %v，期望 %v（band 必须与 elems 同序）", elems, band, want)
		}
	}
	// band 必须严格递增（每个元素至少分到 1 个单位）。
	for i := 1; i < len(band); i++ {
		if band[i] <= band[i-1] {
			t.Fatalf("band %v 非严格递增", band)
		}
	}
}

// TestProbability_AllBucketsReachable 回归测试：每个桶都必须能被选中。
//
// 缺陷成因：比较用 `r <= v` 而非 `r < v` ⇒ 边界值被算给前一个桶 ⇒
// **最后一个桶永远选不到**（实测 band=[1,2,3] 时下标 2 一次都没被选中，
// band=[1,2] 时下标 1 一次都没被选中）。
func TestProbability_AllBucketsReachable(t *testing.T) {
	counts := map[int]int{}
	const rounds = 3000
	for i := 0; i < rounds; i++ {
		idx, _ := Probability([]int{1, 2, 3})
		counts[idx]++
	}
	for idx := 0; idx < 3; idx++ {
		if counts[idx] == 0 {
			t.Errorf("桶 %d 从未被选中（分布 %v）——边界值被判给了前一个桶", idx, counts)
		}
	}
	// 均匀权重的三个桶应大致等分。
	for idx := 0; idx < 3; idx++ {
		if counts[idx] < rounds/4 || counts[idx] > rounds/2 {
			t.Errorf("桶 %d 选中 %d 次（期望约 %d），分布 %v", idx, counts[idx], rounds/3, counts)
		}
	}
}

// TestProbabilityElems_InvalidInputPanics 非法输入必须快速失败——静默退化成
// 「恒选 index 0」的概率路由比报错更危险。
func TestProbabilityElems_InvalidInputPanics(t *testing.T) {
	cases := []struct {
		name    string
		elems   []float64
		precise float64
	}{
		{"含零权重", []float64{0, 0.5}, 1},
		{"含负权重", []float64{0.5, -0.1}, 1},
		{"precise 小于 1（band 会退化为全 0）", []float64{0.1, 0.3, 0.6}, 0.1},
		{"precise 为 0", []float64{0.1, 0.3}, 0},
		{"precise 为 NaN", []float64{0.1, 0.3}, math.NaN()},
		{"权重为 NaN", []float64{math.NaN(), 0.3}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("ProbabilityElems(%v, %v) 应 panic（静默退化更危险）", c.elems, c.precise)
				}
			}()
			_ = ProbabilityElems(c.elems, c.precise)
		})
	}
}

// TestDivisionN_NoInt64Overflow 回归测试：截断不得经 int64 中转——超出 int64
// 范围时 int64(x) 会回绕（Go 规范未定义，amd64/arm64 实测得到
// -9223372036854775808）。
//
// 实测（修前）：DivisionN(1e18, 1, 6) = 9.223372036854775e+12（期望 1e18）。
func TestDivisionN_NoInt64Overflow(t *testing.T) {
	cases := []struct {
		dividend, divisor float64
		n                 int
		want              float64
	}{
		{1e18, 1, 6, 1e18},
		{1e300, 1, 0, 1e300},
		{3, 2, 2, 1.5},
		{10, 4, 1, 2.5},
	}
	for _, c := range cases {
		got := DivisionN(c.dividend, c.divisor, c.n)
		if math.Abs(got-c.want) > math.Abs(c.want)*1e-9 {
			t.Errorf("DivisionN(%v, %v, %d) = %v，期望 %v", c.dividend, c.divisor, c.n, got, c.want)
		}
	}
}

// TestToFixed_NegativePrecisionPanics 回归测试：负精度不得静默返回 0。
//
// 缺陷成因：toFixed 用 `%.Nf` 格式化，负精度产出 "%!.-1f(...)"（非法浮点
// 字面量）⇒ ParseFloat 失败 ⇒ 错误被吞 ⇒ 返回 0（实测 ToFixed(3.456,-1)==0）。
func TestToFixed_NegativePrecisionPanics(t *testing.T) {
	for _, f := range []func(float64, int) float64{ToFixed, func(x float64, n int) float64 { return Round(x, n) }} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Error("负精度应 panic（静默返回 0 是错值）")
				}
			}()
			_ = f(3.456, -1)
		}()
	}
}

// TestMcNemarExact_NoNaNForLargeN 回归测试：大 n 不得返回 NaN。
//
// 缺陷成因：直接累加组合数 C(n,i) 会溢出（C(1200,600) ≈ 1e360 > float64
// 上限）⇒ 累加和与 2^n 同时成 +Inf ⇒ Inf/Inf = NaN ⇒ 返回 NaN。
//
// 实测（修前）：McNemarExact(600,600) = NaN。
func TestMcNemarExact_NoNaNForLargeN(t *testing.T) {
	cases := []struct {
		n01, n10 int
		wantLo   float64
		wantHi   float64
	}{
		{600, 600, 0.9, 1.0},   // 高度一致 ⇒ 不显著 ⇒ p 接近 1
		{600, 0, 0, 0.1},       // 极端不一致 ⇒ 极显著 ⇒ p 接近 0
		{5, 0, 0.0625, 0.0625}, // 小 n 的经典值
		{5, 5, 1, 1},
		{0, 0, 1, 1},
	}
	for _, c := range cases {
		got := McNemarExact(c.n01, c.n10)
		if math.IsNaN(got) {
			t.Errorf("McNemarExact(%d, %d) = NaN", c.n01, c.n10)
			continue
		}
		if got < c.wantLo || got > c.wantHi {
			t.Errorf("McNemarExact(%d, %d) = %v，期望落在 [%v, %v]", c.n01, c.n10, got, c.wantLo, c.wantHi)
		}
	}
}

// TestMcNemarExact_NegativeCountsTreatedAsZero 负数计数按 0 处理（不能变成
// 「极显著」）。
func TestMcNemarExact_NegativeCountsTreatedAsZero(t *testing.T) {
	got := McNemarExact(-1, 5)
	if math.IsNaN(got) || got < 0 || got > 1 {
		t.Fatalf("McNemarExact(-1, 5) = %v，期望落在 [0,1]", got)
	}
}

// TestWilsonCI_NoNaNForOutOfRangeCounts 回归测试：越界计数不得产出 NaN 区间。
//
// 缺陷成因：passed 超出 [0,total] ⇒ p 落到 [0,1] 之外 ⇒ p*(1-p)<0 ⇒
// math.Sqrt(负数)=NaN ⇒ 返回 (NaN,NaN)（实测 WilsonCI(5,3,1.96)）。
func TestWilsonCI_NoNaNForOutOfRangeCounts(t *testing.T) {
	cases := []struct {
		passed, total int
	}{
		{5, 3}, {-1, 10}, {20, 10},
	}
	for _, c := range cases {
		lo, hi := WilsonCI(c.passed, c.total, 1.96)
		if math.IsNaN(lo) || math.IsNaN(hi) {
			t.Errorf("WilsonCI(%d, %d, 1.96) = (%v, %v)，不得为 NaN", c.passed, c.total, lo, hi)
			continue
		}
		if lo < 0 || hi > 1 {
			t.Errorf("WilsonCI(%d, %d, 1.96) = (%v, %v)，区间应落在 [0,1]", c.passed, c.total, lo, hi)
		}
	}
}

// TestNewUnstable_NaNDeviation 回归测试：NaN 偏差必须被夹住——NaN 与任何值
// 比较都是 false ⇒ 不夹的话 NaN 一路传下去，int(NaN) 得到实现相关的垃圾
// 整数（TTL 抖动场景下就是垃圾 TTL）。
func TestNewUnstable_NaNDeviation(t *testing.T) {
	u := NewUnstable(math.NaN())
	if got := u.Around(10); got != 10 {
		t.Errorf("NewUnstable(NaN).Around(10) = %v，期望 10（恒等）", got)
	}
}
