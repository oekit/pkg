package convert

import (
	"encoding/json"
	"testing"
)

// ===== 字符串 → 标量 =====

func TestStr2Float32(t *testing.T) {
	tests := []struct {
		s   string
		def float32
		out float32
	}{
		{"3.14", 0, 3.14},
		{"abc", 1.0, 1.0},
		{"", 2.5, 2.5},
	}
	for _, tt := range tests {
		if got := Str2Float32(tt.s, tt.def); got != tt.out {
			t.Errorf("Str2Float32(%q, %v) = %v, want %v", tt.s, tt.def, got, tt.out)
		}
	}
}

func TestStr2Uint(t *testing.T) {
	if Str2Uint("42", 0) != 42 {
		t.Error("expected 42")
	}
	if Str2Uint("-1", 7) != 7 {
		t.Error("negative should return default")
	}
	if Str2Uint("abc", 9) != 9 {
		t.Error("invalid should return default")
	}
}

func TestStr2Uint64(t *testing.T) {
	if Str2Uint64("18446744073709551615", 0) != 18446744073709551615 {
		t.Error("expected max uint64")
	}
	if Str2Uint64("-5", 3) != 3 {
		t.Error("negative should return default")
	}
}

// ===== 标量 → 字符串 =====

func TestUintToStr(t *testing.T) {
	if UintToStr(42) != "42" {
		t.Error("expected '42'")
	}
}

func TestUint64ToStr(t *testing.T) {
	if Uint64ToStr(18446744073709551615) != "18446744073709551615" {
		t.Error("expected max uint64 str")
	}
}

func TestInt32ToStr(t *testing.T) {
	if Int32ToStr(-123) != "-123" {
		t.Error("expected '-123'")
	}
}

func TestFloat32ToStr(t *testing.T) {
	if got := Float32ToStr(3.14, 2); got != "3.14" {
		t.Errorf("expected '3.14', got %q", got)
	}
}

// ===== Any 系列 =====

func TestAnyToInt64(t *testing.T) {
	tests := []struct {
		v   any
		out int64
	}{
		{nil, 0},
		{int(42), 42},
		{int64(100), 100},
		{uint(5), 5},
		{uint64(999), 999},
		{float64(123), 123}, // json.Unmarshal 的典型类型
		{float32(7), 7},
		{true, 1},
		{false, 0},
		{"456", 456},
		{[]byte("789"), 789},
		{"abc", 0},
	}
	for _, tt := range tests {
		if got := AnyToInt64(tt.v); got != tt.out {
			t.Errorf("AnyToInt64(%v(%T)) = %d, want %d", tt.v, tt.v, got, tt.out)
		}
	}
}

func TestAnyToInt(t *testing.T) {
	if AnyToInt(int64(42)) != 42 {
		t.Error("expected 42")
	}
	if AnyToInt("abc") != 0 {
		t.Error("expected 0 for invalid string")
	}
}

func TestAnyToUint64(t *testing.T) {
	if AnyToUint64(uint(42)) != 42 {
		t.Error("expected 42")
	}
	if AnyToUint64("99") != 99 {
		t.Error("expected 99")
	}
	// 负数 int 按位重新解释为大 uint（uint 语义）
	if AnyToUint64(int(-1)) != 18446744073709551615 {
		t.Error("negative int reinterpreted as large uint")
	}
	if AnyToUint64(nil) != 0 {
		t.Error("nil expected 0")
	}
}

func TestAnyToFloat64(t *testing.T) {
	tests := []struct {
		v   any
		out float64
	}{
		{nil, 0},
		{float64(3.14), 3.14},
		{int(10), 10},
		{int64(-5), -5},
		{true, 1},
		{false, 0},
		{"2.5", 2.5},
		{[]byte("9.9"), 9.9},
	}
	for _, tt := range tests {
		if got := AnyToFloat64(tt.v); got != tt.out {
			t.Errorf("AnyToFloat64(%v(%T)) = %v, want %v", tt.v, tt.v, got, tt.out)
		}
	}
}

func TestAnyToFloat32(t *testing.T) {
	if AnyToFloat32(float64(3.14)) != float32(3.14) {
		t.Error("expected 3.14 as float32")
	}
}

func TestAnyToBool(t *testing.T) {
	if !AnyToBool(true) {
		t.Error("bool true expected")
	}
	if AnyToBool(false) {
		t.Error("bool false expected")
	}
	if !AnyToBool("true") {
		t.Error("string 'true' expected true")
	}
	if AnyToBool("xyz") {
		t.Error("invalid string expected false")
	}
	if !AnyToBool(1) {
		t.Error("int 1 expected true")
	}
	if AnyToBool(0) {
		t.Error("int 0 expected false")
	}
	if AnyToBool(nil) {
		t.Error("nil expected false")
	}
}

// ===== byte.go 新增 X→Bytes =====

func TestIntToBytes(t *testing.T) {
	if got := string(IntToBytes(42)); got != "42" {
		t.Errorf("expected '42', got %q", got)
	}
}

func TestInt64ToBytes(t *testing.T) {
	if got := string(Int64ToBytes(-99)); got != "-99" {
		t.Errorf("expected '-99', got %q", got)
	}
}

func TestFloat64ToBytes(t *testing.T) {
	if got := string(Float64ToBytes(3.14, 2)); got != "3.14" {
		t.Errorf("expected '3.14', got %q", got)
	}
}

func TestBoolToBytes(t *testing.T) {
	if got := string(BoolToBytes(true)); got != "true" {
		t.Errorf("expected 'true', got %q", got)
	}
	if got := string(BoolToBytes(false)); got != "false" {
		t.Errorf("expected 'false', got %q", got)
	}
}

// TestAnyToUint64_JSONNumberClamped 回归（[#256]）：json.Number 分支此前
// 绕过 float64ToUint64Safe 直接 uint64(fv)——1e22 饱和成 uint64 最大值
// （看起来像合法值，`if id > 0` 之类校验全部通过），而同一个值走 float64
// 分支是 0。encoding.Unmarshal 用 UseNumber，JSON 数值到业务侧就是
// json.Number——这条是主路径，不是边角。
func TestAnyToUint64_JSONNumberClamped(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want uint64
	}{
		{"json.Number 1e22 溢出归零", json.Number("1e22"), 0},
		{"float64 1e22 同值同答案", float64(1e22), 0},
		{"json.Number -5 负数归零", json.Number("-5"), 0},
		{"json.Number -1 溢出侧饱和归零", json.Number("-99999999999999999999"), 0},
		{"json.Number 0", json.Number("0"), 0},
		{"json.Number 42", json.Number("42"), 42},
		{"json.Number 1.5 截断同 float64", json.Number("1.5"), 1},
		{"float64 1.5 截断", float64(1.5), 1},
		{"json.Number 1e19 范围内", json.Number("1e19"), 10000000000000000000},
		{"json.Number 非法", json.Number("abc"), 0},
		// 整数分支回归：有符号按位重解释（uint 语义）是文档明写的既有契约，不动
		{"int64 -5 按位重解释", int64(-5), 18446744073709551611},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AnyToUint64(tt.in); got != tt.want {
				t.Errorf("AnyToUint64(%#v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// TestAnyToUint64_JSONNumberMatchesFloatPath 钉住「同值两条入口同答案」：
// json.Number 与 float64 的唯一差别应是解析来源，不是转换口径。
func TestAnyToUint64_JSONNumberMatchesFloatPath(t *testing.T) {
	for _, s := range []string{"1e22", "-5", "0", "42", "1.5", "1e19", "3e20", "abc"} {
		n := AnyToUint64(json.Number(s))
		f, err := json.Number(s).Float64()
		if err != nil {
			if n != 0 {
				t.Errorf("AnyToUint64(json.Number(%q)) = %d, want 0（不可解析）", s, n)
			}
			continue
		}
		if want := AnyToUint64(f); n != want {
			t.Errorf("AnyToUint64 同值两条入口分叉：json.Number(%q)=%d, float64=%d", s, n, want)
		}
	}
}
