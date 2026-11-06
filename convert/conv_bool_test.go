package convert

import (
	"encoding/json"
	"math"
	"testing"
)

// TestAnyToBool_FractionalFloat 钉住「数值类型非零为 true」对 (0,1) 区间成立
// （[#205]）。这些取值不是刁钻样例：概率、比例、阈值这类字段的正常取值就在
// (0,1) 内，判成 false 会让 JSON 配置 / 查询参数里的开关静默失真。
func TestAnyToBool_FractionalFloat(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want bool
	}{
		{"float64 0.5", float64(0.5), true},
		{"float64 -0.9", float64(-0.9), true},
		{"float32 0.25", float32(0.25), true},
		{"float64 极小正数", math.SmallestNonzeroFloat64, true},
		{"float64 极小负数", -math.SmallestNonzeroFloat64, true},
		{"float64 0", float64(0), false},
		{"float64 负零", math.Copysign(0, -1), false},
		{"json.Number 0.5", json.Number("0.5"), true},
		{"json.Number -0.9", json.Number("-0.9"), true},
		{"json.Number 0", json.Number("0"), false},
		{"json.Number 非法", json.Number("abc"), false},
		// 整数侧回归：既有的非零即真 / 零即假不得被本次改动带偏
		{"int 1", 1, true},
		{"int -1", -1, true},
		{"int 0", 0, false},
		{"int64 最大值", int64(math.MaxInt64), true},
		{"uint64 最大值", uint64(math.MaxUint64), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AnyToBool(tt.in); got != tt.want {
				t.Errorf("AnyToBool(%#v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestAnyToBool_NotViaIntTruncation 是上一条的**机制**断言：只要实现回到
// 「先转整数再比零」，这些输入就会翻成 false。它比逐条取值更直接地钉住根因，
// 也让「有人把 AnyToFloat64 换回 AnyToInt64」这种改动立刻变红。
func TestAnyToBool_NotViaIntTruncation(t *testing.T) {
	for _, v := range []any{0.5, -0.9, float32(0.5), json.Number("0.5")} {
		if AnyToInt64(v) != 0 {
			t.Fatalf("前提不成立：AnyToInt64(%#v) 应为 0（否则本用例失去判别力）", v)
		}
		if !AnyToBool(v) {
			t.Errorf("AnyToBool(%#v) 走了整数截断路径：非零浮点被判成 false", v)
		}
	}
}
