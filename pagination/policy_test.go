package pagination

import (
	"math"
	"testing"
)

// TestPolicy_Normalize 表驱动覆盖各服务口径：包级默认、自定义默认页长、
// 钳到上限、超限回退默认、不限上限。
func TestPolicy_Normalize(t *testing.T) {
	cases := []struct {
		name     string
		policy   Policy
		page     int
		pageSize int
		wantPage int
		wantSize int
	}{
		{
			name:     "零值口径等于包级默认",
			policy:   Policy{},
			page:     1,
			pageSize: 1000,
			wantPage: 1,
			wantSize: MaxPageSize,
		},
		{
			name:     "零值口径的非法入参退回包级默认",
			policy:   Policy{},
			page:     0,
			pageSize: 0,
			wantPage: DefaultPage,
			wantSize: DefaultPageSize,
		},
		{
			name:     "负值入参退回默认",
			policy:   Policy{},
			page:     -3,
			pageSize: -7,
			wantPage: DefaultPage,
			wantSize: DefaultPageSize,
		},
		{
			name:     "自定义上限且钳到上限",
			policy:   Policy{DefaultPageSize: 20, MaxPageSize: 100},
			page:     2,
			pageSize: 500,
			wantPage: 2,
			wantSize: 100,
		},
		{
			name:     "自定义上限内原样返回",
			policy:   Policy{DefaultPageSize: 20, MaxPageSize: 100},
			page:     2,
			pageSize: 100,
			wantPage: 2,
			wantSize: 100,
		},
		{
			name:     "超限回退默认页长",
			policy:   Policy{DefaultPageSize: 50, MaxPageSize: 200, OverMax: OverMaxFallback},
			page:     1,
			pageSize: 1000,
			wantPage: 1,
			wantSize: 50,
		},
		{
			name:     "超限回退默认页长但未超限时不动",
			policy:   Policy{DefaultPageSize: 50, MaxPageSize: 200, OverMax: OverMaxFallback},
			page:     3,
			pageSize: 150,
			wantPage: 3,
			wantSize: 150,
		},
		{
			name:     "超限回退口径的非法页长也回退默认",
			policy:   Policy{DefaultPageSize: 50, MaxPageSize: 200, OverMax: OverMaxFallback},
			page:     0,
			pageSize: 0,
			wantPage: DefaultPage,
			wantSize: 50,
		},
		{
			name:     "不限上限",
			policy:   Policy{DefaultPageSize: 20, MaxPageSize: -1},
			page:     1,
			pageSize: 5000,
			wantPage: 1,
			wantSize: 5000,
		},
		{
			name:     "不限上限时非法页长仍取默认",
			policy:   Policy{DefaultPageSize: 20, MaxPageSize: -1},
			page:     2,
			pageSize: 0,
			wantPage: 2,
			wantSize: 20,
		},
		{
			name:     "自定义默认页码",
			policy:   Policy{DefaultPage: 3, DefaultPageSize: 20, MaxPageSize: 100},
			page:     0,
			pageSize: 10,
			wantPage: 3,
			wantSize: 10,
		},
		{
			name:     "未知 OverMax 取值按钳到上限处理",
			policy:   Policy{DefaultPageSize: 20, MaxPageSize: 100, OverMax: OverMaxPolicy(42)},
			page:     1,
			pageSize: 500,
			wantPage: 1,
			wantSize: 100,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, size := tc.policy.Normalize(tc.page, tc.pageSize)
			if page != tc.wantPage || size != tc.wantSize {
				t.Errorf("Normalize(%d, %d) = (%d, %d), want (%d, %d)",
					tc.page, tc.pageSize, page, size, tc.wantPage, tc.wantSize)
			}
		})
	}
}

// TestPolicy_OffsetLimit 表驱动覆盖偏移量与限制条数换算（含溢出回退）。
func TestPolicy_OffsetLimit(t *testing.T) {
	cases := []struct {
		name      string
		policy    Policy
		page      int
		pageSize  int
		wantOff   int
		wantLimit int
	}{
		{
			name:      "常规换算",
			policy:    Policy{},
			page:      3,
			pageSize:  20,
			wantOff:   40,
			wantLimit: 20,
		},
		{
			name:      "非法入参按默认口径换算",
			policy:    Policy{},
			page:      0,
			pageSize:  0,
			wantOff:   0,
			wantLimit: DefaultPageSize,
		},
		{
			name:      "超限回退口径的偏移量按回退后的页长算",
			policy:    Policy{DefaultPageSize: 50, MaxPageSize: 200, OverMax: OverMaxFallback},
			page:      3,
			pageSize:  1000,
			wantOff:   100,
			wantLimit: 50,
		},
		{
			name:      "钳到上限口径的偏移量按钳后的页长算",
			policy:    Policy{DefaultPageSize: 20, MaxPageSize: 100},
			page:      3,
			pageSize:  1000,
			wantOff:   200,
			wantLimit: 100,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.policy.Offset(tc.page, tc.pageSize); got != tc.wantOff {
				t.Errorf("Offset(%d, %d) = %d, want %d", tc.page, tc.pageSize, got, tc.wantOff)
			}
			if got := tc.policy.Limit(tc.page, tc.pageSize); got != tc.wantLimit {
				t.Errorf("Limit(%d, %d) = %d, want %d", tc.page, tc.pageSize, got, tc.wantLimit)
			}
		})
	}
}

// TestPolicy_NormalizeLimit 表驱动覆盖「条数口径」归一化：非法条数取默认、
// 上限内原样返回、超限按 OverMax 语义处理、不限上限不截断。
func TestPolicy_NormalizeLimit(t *testing.T) {
	cases := []struct {
		name   string
		policy Policy
		n      int
		want   int
	}{
		{
			name:   "零值口径的非法条数退回包级默认",
			policy: Policy{},
			n:      0,
			want:   DefaultPageSize,
		},
		{
			name:   "零值口径的负值条数退回包级默认",
			policy: Policy{},
			n:      -7,
			want:   DefaultPageSize,
		},
		{
			name:   "零值口径超上限钳到包级上限",
			policy: Policy{},
			n:      1000,
			want:   MaxPageSize,
		},
		{
			name:   "零值口径上限内原样返回",
			policy: Policy{},
			n:      100,
			want:   100,
		},
		{
			name:   "自定义默认条数",
			policy: Policy{DefaultPageSize: 50, MaxPageSize: 200},
			n:      0,
			want:   50,
		},
		{
			name:   "自定义上限内原样返回",
			policy: Policy{DefaultPageSize: 50, MaxPageSize: 200},
			n:      150,
			want:   150,
		},
		{
			name:   "超限钳到上限",
			policy: Policy{DefaultPageSize: 50, MaxPageSize: 200},
			n:      1000,
			want:   200,
		},
		{
			name:   "超限回退默认条数",
			policy: Policy{DefaultPageSize: 50, MaxPageSize: 200, OverMax: OverMaxFallback},
			n:      1000,
			want:   50,
		},
		{
			name:   "超限回退口径未超限时不动",
			policy: Policy{DefaultPageSize: 50, MaxPageSize: 200, OverMax: OverMaxFallback},
			n:      150,
			want:   150,
		},
		{
			name:   "不限上限不截断",
			policy: Policy{DefaultPageSize: 20, MaxPageSize: -1},
			n:      5000,
			want:   5000,
		},
		{
			name:   "不限上限时非法条数仍取默认",
			policy: Policy{DefaultPageSize: 20, MaxPageSize: -1},
			n:      0,
			want:   20,
		},
		{
			name:   "未知 OverMax 取值按钳到上限处理",
			policy: Policy{DefaultPageSize: 20, MaxPageSize: 100, OverMax: OverMaxPolicy(42)},
			n:      500,
			want:   100,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.policy.NormalizeLimit(tc.n); got != tc.want {
				t.Errorf("NormalizeLimit(%d) = %d, want %d", tc.n, got, tc.want)
			}
			// 等价不变量：条数口径 == 把页码钉成 1 时的页长档
			if got, want := tc.policy.NormalizeLimit(tc.n), tc.policy.Limit(1, tc.n); got != want {
				t.Errorf("NormalizeLimit(%d) = %d, Limit(1, %d) = %d（两者必须一致）",
					tc.n, got, tc.n, want)
			}
		})
	}
}

// TestPolicy_NormalizeLimit_VsPageDimension 钉住「条数口径」与「按页口径」各自自洽：
// 同一个 Policy 下，Normalize(3, 50) 给的是「第 3 页、每页 50 条」，
// NormalizeLimit(50) 给的是「最多 50 条」——两者语义不同，不得互相污染。
func TestPolicy_NormalizeLimit_VsPageDimension(t *testing.T) {
	policy := Policy{DefaultPageSize: 50, MaxPageSize: 200, OverMax: OverMaxFallback}

	page, size := policy.Normalize(3, 50)
	if page != 3 || size != 50 {
		t.Errorf("Normalize(3, 50) = (%d, %d), want (3, 50)", page, size)
	}
	if got := policy.NormalizeLimit(50); got != 50 {
		t.Errorf("NormalizeLimit(50) = %d, want 50", got)
	}

	// 条数口径不受默认页码影响：DefaultPage 改成 3 也不影响「最多 N 条」
	policyWithPage := Policy{DefaultPage: 3, DefaultPageSize: 50, MaxPageSize: 200}
	if got := policyWithPage.NormalizeLimit(50); got != 50 {
		t.Errorf("NormalizeLimit(50) with DefaultPage=3 = %d, want 50", got)
	}
	// 页码口径不受条数口径影响
	if page, size := policyWithPage.Normalize(0, 50); page != 3 || size != 50 {
		t.Errorf("Normalize(0, 50) = (%d, %d), want (3, 50)", page, size)
	}
}

// TestPolicy_OffsetOverflow 偏移量溢出（int 溢出成负数）时回退到 0。
func TestPolicy_OffsetOverflow(t *testing.T) {
	policy := Policy{DefaultPageSize: 20, MaxPageSize: -1}
	if got := policy.Offset(math.MaxInt, 1000); got != 0 {
		t.Errorf("Offset(math.MaxInt, 1000) = %d, want 0 (overflow must fall back to first page)", got)
	}
}

// TestPolicy_MatchesPageRequestDefaults 钉住零值口径与包级 PageRequest 口径逐字一致——
// Policy 是把既有口径参数化，不是另立一套。
func TestPolicy_MatchesPageRequestDefaults(t *testing.T) {
	policy := Policy{}
	for _, tc := range []struct {
		page     int
		pageSize int
	}{
		{page: 0, pageSize: 0},
		{page: -1, pageSize: -5},
		{page: 1, pageSize: 10},
		{page: 2, pageSize: 1000},
		{page: 5, pageSize: MaxPageSize},
	} {
		req := NewPageRequest(tc.page, tc.pageSize)
		req.Normalize()

		page, size := policy.Normalize(tc.page, tc.pageSize)
		if page != req.Page || size != req.PageSize {
			t.Errorf("Policy{}.Normalize(%d, %d) = (%d, %d), PageRequest.Normalize = (%d, %d)",
				tc.page, tc.pageSize, page, size, req.Page, req.PageSize)
		}
		if got, want := policy.Offset(tc.page, tc.pageSize), req.Offset(); got != want {
			t.Errorf("Policy{}.Offset(%d, %d) = %d, PageRequest.Offset = %d",
				tc.page, tc.pageSize, got, want)
		}
	}
}
