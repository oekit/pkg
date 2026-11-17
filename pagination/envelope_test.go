package pagination

import (
	"math"
	"testing"
)

// TestComputePages 信封 Pages 字段口径（gormx#248）：向上取整 + 边界。
func TestComputePages(t *testing.T) {
	cases := []struct {
		name     string
		total    int64
		pageSize int
		want     int
	}{
		{"零数据", 0, 20, 0},
		{"负数据", -1, 20, 0},
		{"零页长", 100, 0, 0},
		{"负页长", 100, -5, 0},
		{"整除", 100, 20, 5},
		{"余数进位", 101, 20, 6},
		{"尾页不满", 1, 500, 1},
		{"尾页一条", 500, 500, 1},
		{"尾页两条", 501, 500, 2},
		{"1<<62 单条页", 1 << 62, 1, 1 << 62},
		{"1<<62+1 单条页（float64 精度域外）", 1<<62 + 1, 1, 1<<62 + 1},
		{"2^53+1 单条页（float64 精度边界外不丢 +1）", 1<<53 + 1, 1, 1<<53 + 1},
		{"MaxInt64 奇数余数进位", math.MaxInt64, 2, math.MaxInt64/2 + 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ComputePages(tc.total, tc.pageSize); got != tc.want {
				t.Fatalf("ComputePages(%d, %d) = %d，期望 %d", tc.total, tc.pageSize, got, tc.want)
			}
		})
	}

	// math.MaxInt 边界：64 位下 (MaxInt64, 1) 即 MaxInt；32 位下饱和到
	// math.MaxInt，不回绕成负数。
	if got := ComputePages(math.MaxInt64, 1); got != math.MaxInt {
		t.Fatalf("ComputePages(MaxInt64, 1) = %d，期望饱和到 math.MaxInt", got)
	}
	if got := ComputePages(math.MaxInt64, 1); got <= 0 {
		t.Fatalf("饱和结果不得回绕成非正数，实得 %d", got)
	}
}

// TestComputePages_CeilInvariant 性质面：对任意 (total, pageSize)，
// total <= pages*pageSize < total+pageSize（向上取整的定义式）。
// 网格取值限制在 pages*pageSize 不溢出 int64 的范围（极端 total 的精确值
// 已由 TestComputePages 的边界表钉住——那里 pages*pageSize 本身会溢出，
// 定义式校验只能在可表示域内做）。
func TestComputePages_CeilInvariant(t *testing.T) {
	totals := []int64{1, 19, 20, 21, 399, 400, 401, 10000, 1 << 40, 1 << 53, 1<<53 + 1}
	pageSizes := []int{1, 2, 3, 7, 20, 500, 4096}
	for _, total := range totals {
		for _, ps := range pageSizes {
			pages := ComputePages(total, ps)
			if pages <= 0 {
				t.Fatalf("ComputePages(%d, %d) = %d，正数据正页长应为正页数", total, ps, pages)
			}
			product := int64(pages) * int64(ps)
			if product < total || product >= total+int64(ps) {
				t.Fatalf("ComputePages(%d, %d) = %d 违反向上取整定义式（pages*pageSize=%d）",
					total, ps, pages, product)
			}
		}
	}
}

// TestServedPage 信封 Page 回显口径（#233/gormx#248）：偏移量溢出时回显
// 落回实际服务的第一页，未溢出原样回显。
func TestServedPage(t *testing.T) {
	cases := []struct {
		name     string
		page     int
		pageSize int
		want     int
	}{
		{"第一页", 1, 500, 1},
		{"普通页", 2, 500, 2},
		{"大页未溢出", 1000, 500, 1000},
		{"1<<62+2 溢出回退", 1<<62 + 2, 500, 1},
		{"1<<62 溢出回退", 1 << 62, 500, 1},
		{"MaxInt64 页长 2 溢出回退", math.MaxInt64, 2, 1},
		{"MaxInt64 页长 1 不溢出（(MaxInt64-1)*1 可表示）", math.MaxInt64, 1, math.MaxInt64},
		{"页长 1 临界不溢出", math.MaxInt64, 1, math.MaxInt64},
		{"溢出临界 1<<62+1", 1<<62 + 1, 500, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ServedPage(tc.page, tc.pageSize); got != tc.want {
				t.Fatalf("ServedPage(%d, %d) = %d，期望 %d", tc.page, tc.pageSize, got, tc.want)
			}
		})
	}

	// 不归一化：page <= 0 原样返回（归一化是调用方的职责）。
	if got := ServedPage(0, 500); got != 0 {
		t.Fatalf("ServedPage(0, 500) = %d，期望原样 0", got)
	}
}

// TestServedPage_MatchesOffsetFallback 一致性面：page > 1 且 pageSize > 0
// 时，「ServedPage 回退 1」与「Policy.Offset 溢出回退 0」必须是同一件事
// ——同一判据（offsetOverflows）单源的直接推论，网格压住防两处漂移。
func TestServedPage_MatchesOffsetFallback(t *testing.T) {
	var p Policy
	pages := []int{1, 2, 3, 1000, 1 << 62, 1<<62 + 1, 1<<62 + 2, math.MaxInt64}
	pageSizes := []int{1, 2, 20, 500, MaxPageSize}
	for _, page := range pages {
		for _, ps := range pageSizes {
			offset := p.Offset(page, ps)
			served := ServedPage(page, ps)
			switch {
			case offset == 0 && page > 1:
				if served != DefaultPage {
					t.Fatalf("Offset(%d,%d) 溢出回退 0 时 ServedPage 应回退 %d，实得 %d",
						page, ps, DefaultPage, served)
				}
			case page > 1 && ps > 0:
				if served != page {
					t.Fatalf("Offset(%d,%d)=%d 未溢出时 ServedPage 应原样回显 %d，实得 %d",
						page, ps, offset, page, served)
				}
			}
		}
	}
}
