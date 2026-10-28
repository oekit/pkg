package pagination

import (
	"math"
	"testing"
)

// TestPageRequest_Offset_OverflowBeforeMultiply 回归测试：偏移量溢出必须
// 在**乘法前**判定。
//
// 缺陷成因：原判据是「乘法后 offset < 0 ⇒ 回退 0」，但 int 溢出回绕后的
// 结果有一半是**正数**——那些被原判据放过去，返回的是一个完全错误的偏移量
// （实测 page=1<<62+2、pageSize=500 ⇒ 真值 2.3e18 超出 int64 ⇒ 回绕成
// +500）。page 来自 query/form 参数（外部可控）⇒ 不能靠「回绕后是否为负」兜底。
func TestPageRequest_Offset_OverflowBeforeMultiply(t *testing.T) {
	cases := []int{1 << 62, 1<<62 + 1, 1<<62 + 2, 1<<62 + 3, math.MaxInt64}
	for _, page := range cases {
		r := NewPageRequest(page, 500)
		if got := r.Offset(); got != 0 {
			t.Errorf("NewPageRequest(%d, 500).Offset() = %d，期望 0（溢出应回退第一页）", page, got)
		}
	}
	// 未溢出的正常值不受影响。
	if got := NewPageRequest(3, 20).Offset(); got != 40 {
		t.Errorf("Offset() = %d，期望 40", got)
	}
}

// TestPolicy_Offset_OverflowBeforeMultiply 同样的溢出保护要覆盖 Policy
// （服务级口径的偏移量入口）。
func TestPolicy_Offset_OverflowBeforeMultiply(t *testing.T) {
	var p Policy
	for _, page := range []int{1 << 62, 1<<62 + 2, math.MaxInt64} {
		if got := p.Offset(page, 500); got != 0 {
			t.Errorf("Policy{}.Offset(%d, 500) = %d，期望 0（溢出应回退第一页）", page, got)
		}
	}
	if got := p.Offset(3, 20); got != 40 {
		t.Errorf("Policy{}.Offset(3, 20) = %d，期望 40", got)
	}
}

// TestPolicy_Offset_UnlimitedMaxPageSize 不限上限口径下同样不得溢出。
func TestPolicy_Offset_UnlimitedMaxPageSize(t *testing.T) {
	p := Policy{MaxPageSize: -1}
	if got := p.Offset(1<<62+2, 500); got != 0 {
		t.Errorf("不限上限口径下溢出仍应回退 0，实得 %d", got)
	}
}
