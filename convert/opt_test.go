package convert

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// TestAnyToStr_Unchanged 钉住既有语义不动：AnyToStr(nil) 仍是 "<nil>"。
//
// 本次补的是**并列**的一档（AnyToStrOr / Opt*），不是改既有函数的语义——
// 改语义会让所有在用 AnyToStr(nil) 的调用点静默改变行为。
func TestAnyToStr_Unchanged(t *testing.T) {
	if got := AnyToStr(nil); got != "<nil>" {
		t.Fatalf("AnyToStr(nil) = %q, want %q（既有语义必须不变）", got, "<nil>")
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
