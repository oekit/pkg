package mathx

import (
	"math"
	"strings"
	"testing"
)

// 本文件钉住 ProbabilityElems 的动态范围边界（pkg#208）。
//
// band 是「以最小权重为精度单位」的整数表示，最大值是 total × p。超出 int
// 表示范围时 float64 → int 的转换结果是**实现相关的**：实测 arm64 饱和到
// MaxInt64、amd64 给 MinInt64。两者都不是错误信号——
//
//   - 饱和把多个大桶挤成同一个数，比例被静默改写（实测 [1e-30, 1e-30, 1e3]
//     下第二个桶被高估约 17 个数量级）；
//   - MinInt64 让 Probability 的 max<=0 分支直接退化到「恒选 index 0」。
//
// 这不是「算错了」，而是表示法装不下。故与既有输入校验同口径：快速失败。

// TestProbabilityElems_DynamicRangePanics 钉住极端权重跨度必须 panic。
func TestProbabilityElems_DynamicRangePanics(t *testing.T) {
	cases := []struct {
		name    string
		elems   []float64
		precise float64
	}{
		// total/min = 1e303，× precise 1000 ⇒ 1e306，远超 MaxInt64。
		// 修复前实测 band = [1000, MaxInt64]（arm64 饱和）——比例已失真。
		{"跨度 1e303", []float64{1e-300, 1e3}, 1000},
		// 修复前实测 band = [1000, 2000, MaxInt64]：第二个桶真实占比 1e-33，
		// band 却给出 2000:9.2e18 ≈ 2.2e-16，被高估约 17 个数量级。
		{"跨度 1e33 且多桶", []float64{1e-30, 1e-30, 1e3}, 1000},
		// 次正规权重让 p 本身变成 +Inf：任何 acc*p 都是 Inf。
		{"p 溢出为 Inf", []float64{1e-320, 1.0}, 1},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("ProbabilityElems(%v, %v) 未 panic——整数 band 装不下时必须快速失败",
						tt.elems, tt.precise)
				}
				msg, _ := r.(string)
				if !strings.Contains(msg, "too wide a dynamic range") {
					t.Fatalf("panic 信息应指明动态范围，got %v", r)
				}
			}()
			_ = ProbabilityElems(tt.elems, tt.precise)
		})
	}
}

// TestProbabilityElems_DynamicRangeBoundary 是对上的**成对判据**：跨度巨大
// 但仍在 int 表示范围内时，必须正常工作且比例正确。
//
// 没有这一条，上一条的「全绿」可能只是因为判据把所有大跨度输入都拒了。
func TestProbabilityElems_DynamicRangeBoundary(t *testing.T) {
	// minW = 1e-9、precise = 1000 ⇒ p = 1e12、total ≈ 1e3 ⇒ max band ≈ 1e15，
	// 落在 int 范围内（MaxInt64 ≈ 9.22e18），比值 1e12 也远未到触发线。
	elems := []float64{1e-9, 1e3}
	band := ProbabilityElems(elems, 1000)
	if len(band) != 2 {
		t.Fatalf("len(band) = %d, want 2", len(band))
	}
	// 最小权重分到的就是 precise 个单位（p 的定义）
	if band[0] != 1000 {
		t.Fatalf("band[0] = %d, want 1000（最小权重应恰好分到 precise 个单位）", band[0])
	}
	// 比例必须还成立：w1:w0 = 1e12
	ratio := float64(band[1]) / float64(band[0])
	if math.Abs(ratio-1e12)/1e12 > 1e-6 {
		t.Fatalf("band 比例 = %v, want ~1e12（band 必须保持权重比例）", ratio)
	}
}

// TestProbabilityElems_BandStaysMonotone 是通用不变式：任何被接受的输入，
// band 都必须非递减且首项 ≥ precise。它同时兜住「判据放得太宽」这种反向
// 失误——若某个本该拒绝的输入被放过去，band 的单调性通常第一个崩。
func TestProbabilityElems_BandStaysMonotone(t *testing.T) {
	cases := []struct {
		elems   []float64
		precise float64
	}{
		{[]float64{1, 1, 1}, 100},
		{[]float64{0.6, 0.1, 0.3}, 10},
		{[]float64{1e-6, 1, 1e3}, 1000},
		{[]float64{1e-9, 1e3}, 1000},
	}
	for _, tt := range cases {
		band := ProbabilityElems(tt.elems, tt.precise)
		if band[0] < int(tt.precise) {
			t.Fatalf("ProbabilityElems(%v, %v): band[0] = %d, want >= %v",
				tt.elems, tt.precise, band[0], tt.precise)
		}
		for i := 1; i < len(band); i++ {
			if band[i] < band[i-1] {
				t.Fatalf("ProbabilityElems(%v, %v) = %v：band 必须非递减",
					tt.elems, tt.precise, band)
			}
		}
	}
}
