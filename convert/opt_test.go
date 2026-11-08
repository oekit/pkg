package convert

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// TestAnyToStr_NilSafe 钉住 nil 语义（#295）：AnyToStr(nil) 是空串，不是
// Go 内部表示 "<nil>"——REST 应答解出的 null 字段（无类型 nil 接口）经它
// 不得把 "<nil>" 字面写进答复/留痕。
//
// 历史注记：v0.38.0 及之前 AnyToStr(nil) 返回 "<nil>"（彼时本测试名
// TestAnyToStr_Unchanged 钉的是旧语义）；#295 依「转字符串的直觉语义就是
// 没有值 ⇒ 空串」收敛，AnyToStrOr(nil, def) 承接需要非空缺省的调用方。
func TestAnyToStr_NilSafe(t *testing.T) {
	if got := AnyToStr(nil); got != "" {
		t.Fatalf("AnyToStr(nil) = %q, want %q（#295：nil ⇒ 空串）", got, "")
	}
}

// TestAnyToStrOr_NilSafe 钉住 nil 安全那一档：nil 读成 def，不是 "<nil>"。
func TestAnyToStrOr_NilSafe(t *testing.T) {
	if got := AnyToStrOr(nil, ""); got != "" {
		t.Errorf("AnyToStrOr(nil, \"\") = %q, want %q", got, "")
	}
	if got := AnyToStrOr(nil, "def"); got != "def" {
		t.Errorf("AnyToStrOr(nil, \"def\") = %q, want %q", got, "def")
	}
	if got := AnyToStrOr("x", "def"); got != "x" {
		t.Errorf("AnyToStrOr(\"x\", \"def\") = %q, want %q", got, "x")
	}
	if got := AnyToStrOr(42, "def"); got != "42" {
		t.Errorf("AnyToStrOr(42, \"def\") = %q, want %q", got, "42")
	}
}

// TestOptStr_AbsentIsNil 钉住「没有就是没有」：缺失 ⇒ nil 指针。
func TestOptStr_AbsentIsNil(t *testing.T) {
	if got := OptStr(nil); got != nil {
		t.Errorf("OptStr(nil) = %v, want nil", *got)
	}
	got := OptStr("x")
	if got == nil || *got != "x" {
		t.Errorf("OptStr(\"x\") = %v, want &\"x\"", got)
	}
	// 存在但为空串 ⇒ 是「有这个字段，值是空串」，不是「没有」。
	got = OptStr("")
	if got == nil || *got != "" {
		t.Errorf("OptStr(\"\") = %v, want &\"\"", got)
	}
}

// TestOptBool_AbsentAndUnconvertibleAreBothNil 钉住两点：
//   - 缺失 ⇒ nil；
//   - 无法表示为 bool ⇒ nil（**不是** &false——把不可用值伪装成合法值，
//     正是这一族 API 存在的意义）。
func TestOptBool_AbsentAndUnconvertibleAreBothNil(t *testing.T) {
	if got := OptBool(nil); got != nil {
		t.Errorf("OptBool(nil) = %v, want nil", *got)
	}
	if got := OptBool("abc"); got != nil {
		t.Errorf("OptBool(\"abc\") = %v, want nil（无法表示为 bool）", *got)
	}
	got := OptBool(true)
	if got == nil || !*got {
		t.Errorf("OptBool(true) = %v, want &true", got)
	}
	got = OptBool(false)
	if got == nil || *got {
		t.Errorf("OptBool(false) = %v, want &false（存在但为 false ≠ 缺失）", got)
	}
	got = OptBool("true")
	if got == nil || !*got {
		t.Errorf("OptBool(\"true\") = %v, want &true", got)
	}
	got = OptBool(1)
	if got == nil || !*got {
		t.Errorf("OptBool(1) = %v, want &true", got)
	}
}

// TestOptInt32_Float64ToInt32 钉住 JSON 数字（float64）进 int32 契约这条路径。
func TestOptInt32_Float64ToInt32(t *testing.T) {
	if got := OptInt32(nil); got != nil {
		t.Errorf("OptInt32(nil) = %v, want nil", *got)
	}
	got := OptInt32(float64(5))
	if got == nil || *got != 5 {
		t.Errorf("OptInt32(float64(5)) = %v, want &5", got)
	}
	// 超 int32 范围 ⇒ 装不下 ⇒ 没有 int32 值（返回 &0 会把垃圾值伪装成合法值）。
	if got := OptInt32(float64(math.MaxInt32) + 1); got != nil {
		t.Errorf("OptInt32(MaxInt32+1) = %v, want nil（超出 int32 范围）", *got)
	}
	if got := OptInt32(float64(math.MinInt32) - 1); got != nil {
		t.Errorf("OptInt32(MinInt32-1) = %v, want nil（超出 int32 范围）", *got)
	}
	if got := OptInt32("abc"); got != nil {
		t.Errorf("OptInt32(\"abc\") = %v, want nil（不可解析）", *got)
	}
}

// TestOptInt64 钉住 int64 那一档。
func TestOptInt64(t *testing.T) {
	if got := OptInt64(nil); got != nil {
		t.Errorf("OptInt64(nil) = %v, want nil", *got)
	}
	got := OptInt64("123")
	if got == nil || *got != 123 {
		t.Errorf("OptInt64(\"123\") = %v, want &123", got)
	}
	got = OptInt64(float64(7))
	if got == nil || *got != 7 {
		t.Errorf("OptInt64(float64(7)) = %v, want &7", got)
	}
	if got := OptInt64("abc"); got != nil {
		t.Errorf("OptInt64(\"abc\") = %v, want nil", *got)
	}
}

// TestAnyToInt32 钉住补上的那一档：装不下 ⇒ 0（与 AnyToInt64 同族的
// 「无法转换 ⇒ 零值」口径），且**不发生回绕**。
func TestAnyToInt32(t *testing.T) {
	if got := AnyToInt32(float64(5)); got != 5 {
		t.Errorf("AnyToInt32(float64(5)) = %d, want 5", got)
	}
	if got := AnyToInt32("7"); got != 7 {
		t.Errorf("AnyToInt32(\"7\") = %d, want 7", got)
	}
	if got := AnyToInt32(nil); got != 0 {
		t.Errorf("AnyToInt32(nil) = %d, want 0", got)
	}
	// 回绕会把越界值变成符号相反的合法值 ⇒ 必须是 0。
	if got := AnyToInt32(int64(1) << 40); got != 0 {
		t.Errorf("AnyToInt32(1<<40) = %d, want 0（不得回绕）", got)
	}
	if got := AnyToInt32(int64(-1) << 40); got != 0 {
		t.Errorf("AnyToInt32(-1<<40) = %d, want 0（不得回绕）", got)
	}
}

// TestOptJSONNumber 钉住 encoding/json（UseNumber）路径：数值是 json.Number。
func TestOptJSONNumber(t *testing.T) {
	var v any
	dec := json.NewDecoder(strings.NewReader(`{"n": 42, "s": "x"}`))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	m := v.(map[string]any)
	got := OptInt32(m["n"])
	if got == nil || *got != 42 {
		t.Errorf("OptInt32(json.Number(\"42\")) = %v, want &42", got)
	}
	if got := OptStr(m["s"]); got == nil || *got != "x" {
		t.Errorf("OptStr(\"x\") = %v, want &\"x\"", got)
	}
	if got := OptStr(m["missing"]); got != nil {
		t.Errorf("OptStr(missing) = %v, want nil", *got)
	}
}

// TestOptBool_MatchesAnyToBool 回归（[#255]）：OptBool 与 AnyToBool 对同一
// 值必须给同一结论。此前 anyToBoolOK 的数值/json.Number 分支经 AnyToInt64
// 中转，|v|<1 的浮点被截断成 0 ⇒ OptBool(0.5) 返回 &false 而
// AnyToBool(0.5) 是 true——不仅值错了，还把「有一个值」降级成「没有这个
// 字段」（nil 的第二层语义），两条错误叠加。
func TestOptBool_MatchesAnyToBool(t *testing.T) {
	tests := []struct {
		name    string
		in      any
		want    bool // AnyToBool 的答案（OptBool 必须同答案）
		wantNil bool // OptBool 报「没有这个值」
	}{
		// #255 复现样本：(0,1) 恰是概率、比例、阈值字段的典型取值
		{"float64 0.5", 0.5, true, false},
		{"float64 -0.9", -0.9, true, false},
		{"json.Number 0.5", json.Number("0.5"), true, false},
		{"int 0", 0, false, false},
		{"float64 0", 0.0, false, false},
		{"json.Number 0", json.Number("0"), false, false},
		// 整数侧回归：非零即真 / 零即假不得被带偏
		{"int 1", 1, true, false},
		{"int -1", -1, true, false},
		{"uint64 最大值", uint64(math.MaxUint64), true, false},
		{"int64 最小值", int64(math.MinInt64), true, false},
		{"float32 0.25", float32(0.25), true, false},
		{"json.Number -0.9", json.Number("-0.9"), true, false},
		{"json.Number 非法", json.Number("abc"), false, false},
		// 「无法表示为 bool」⇒ nil（不是 &false）
		{"nil", nil, false, true},
		{"string abc", "abc", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AnyToBool(tt.in); got != tt.want {
				t.Errorf("AnyToBool(%#v) = %v, want %v", tt.in, got, tt.want)
			}
			got := OptBool(tt.in)
			if tt.wantNil {
				if got != nil {
					t.Errorf("OptBool(%#v) = %v, want nil", tt.in, *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("OptBool(%#v) = nil, want &%v", tt.in, tt.want)
			}
			if *got != tt.want {
				t.Errorf("OptBool(%#v) = %v, want %v（两个入口对同一值必须同答案）", tt.in, *got, tt.want)
			}
		})
	}
}

// TestOptBool_NotViaIntTruncation 机制断言（同 TestAnyToBool_NotViaIntTruncation
// 的手法）：只要实现回到「先转整数再比零」，这些输入就翻红——比逐条取值
// 更直接地钉住根因。
func TestOptBool_NotViaIntTruncation(t *testing.T) {
	for _, v := range []any{0.5, -0.9, float32(0.5), json.Number("0.5")} {
		if AnyToInt64(v) != 0 {
			t.Fatalf("前提不成立：AnyToInt64(%#v) 应为 0（否则本用例失去判别力）", v)
		}
		b := OptBool(v)
		if b == nil || !*b {
			t.Errorf("OptBool(%#v) = %v, want &true（整数截断路径复发）", v, b)
		}
	}
}
