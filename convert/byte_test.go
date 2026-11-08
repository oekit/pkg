package convert

import (
	"testing"
)

func TestBytesToString(t *testing.T) {
	result := BytesToString([]byte("hello"))
	if result != "hello" {
		t.Errorf("BytesToString() = %v, want hello", result)
	}
}

func TestStringToBytes(t *testing.T) {
	result := StringToBytes("hello")
	if string(result) != "hello" {
		t.Errorf("StringToBytes() = %v, want hello", string(result))
	}
}

func TestBytesToInt(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want int
	}{
		{"valid", []byte("123"), 123},
		{"invalid", []byte("abc"), 0},
		{"empty", []byte(""), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BytesToInt(tt.data); got != tt.want {
				t.Errorf("BytesToInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBytesToInt64(t *testing.T) {
	result := BytesToInt64([]byte("123"))
	if result != 123 {
		t.Errorf("BytesToInt64() = %v, want 123", result)
	}
}

func TestBytesToFloat32(t *testing.T) {
	result := BytesToFloat32([]byte("3.14"))
	if result < 3.13 || result > 3.15 {
		t.Errorf("BytesToFloat32() = %v, want ~3.14", result)
	}
}

func TestBytesToFloat64(t *testing.T) {
	result := BytesToFloat64([]byte("3.14"))
	if result < 3.13 || result > 3.15 {
		t.Errorf("BytesToFloat64() = %v, want ~3.14", result)
	}
}

func TestByteToBytes(t *testing.T) {
	result := ByteToBytes('a')
	if len(result) != 1 || result[0] != 'a' {
		t.Errorf("ByteToBytes() = %v, want [97]", result)
	}
}

// TestBytesToInt64_FailReturnsZero 回归（[#257]）：此前丢弃 err，而
// strconv.ParseInt 的范围错误（ErrRange）会同时返回**钳制后的饱和值**——
// "99999999999999999999" 变成 MaxInt64，与文档「失败返回 0」及同文件
// BytesToInt 相反。
func TestBytesToInt64_FailReturnsZero(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want int64
	}{
		{"正常", []byte("123"), 123},
		{"负数", []byte("-5"), -5},
		{"非法", []byte("abc"), 0},
		{"空串", []byte(""), 0},
		{"正溢出饱和值归零", []byte("99999999999999999999"), 0},
		{"负溢出饱和值归零", []byte("-99999999999999999999"), 0},
		{"uint64 上界超 int64", []byte("18446744073709551615"), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BytesToInt64(tt.data); got != tt.want {
				t.Errorf("BytesToInt64(%q) = %d, want %d", tt.data, got, tt.want)
			}
		})
	}
}

// TestBytesToFloat_FailReturnsZero 同型（[#257]）：ParseFloat 的 ErrRange
// 返回 ±Inf（"1e999" → +Inf），不判 err 就把 ±Inf 当合法值。
func TestBytesToFloat_FailReturnsZero(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want float64
	}{
		{"正常", []byte("3.14"), 3.14},
		{"非法", []byte("abc"), 0},
		{"空串", []byte(""), 0},
		{"上溢归零（此前 +Inf）", []byte("1e999"), 0},
		{"下溢归零（此前 -Inf）", []byte("-1e999"), 0},
		{"大数仍在范围内", []byte("99999999999999999999"), 1e20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BytesToFloat64(tt.data); got != tt.want {
				t.Errorf("BytesToFloat64(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

func TestBytesToFloat32_FailReturnsZero(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want float32
	}{
		{"正常", []byte("3.14"), 3.14},
		{"非法", []byte("abc"), 0},
		{"空串", []byte(""), 0},
		{"上溢归零（此前 +Inf）", []byte("1e999"), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BytesToFloat32(tt.data); got != tt.want {
				t.Errorf("BytesToFloat32(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

// TestAnyToInt64_BytesAndStringAgree 钉住后果侧：AnyToInt64 内部按类型
// 分派到 BytesToInt64 / Str2Int64，同一文本的两条入口必须同答案（此前
// []byte 走饱和值、string 走 0）。
func TestAnyToInt64_BytesAndStringAgree(t *testing.T) {
	for _, s := range []string{"99999999999999999999", "-99999999999999999999", "123", "-5", "abc", ""} {
		if got, want := AnyToInt64([]byte(s)), AnyToInt64(s); got != want {
			t.Errorf("AnyToInt64 同文本两条入口分叉：[]byte=%d, string=%d (%q)", got, want, s)
		}
	}
}
