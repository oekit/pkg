package stringx

import (
	"reflect"
	"testing"
)

// TestSplitCSVNonEmpty 口径表驱动：拆 + trim + 丢空项，空串/全空返回 nil。
func TestSplitCSVNonEmpty(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"普通三段", "a,b,c", []string{"a", "b", "c"}},
		{"单段", "a", []string{"a"}},
		{"值含空格保留（不按空白切）", "my glob,other", []string{"my glob", "other"}},
		{"逐项 trim 首尾空白", "a, b ,c", []string{"a", "b", "c"}},
		{"丢弃中间空项", "a,,b", []string{"a", "b"}},
		{"丢弃尾部空项", "a,b,", []string{"a", "b"}},
		{"丢弃首部空项", ",a,b", []string{"a", "b"}},
		{"全是空项丢弃", ",,", nil},
		{"全是空白丢弃", "  ,  ", nil},
		{"空串返回 nil", "", nil},
		{"纯空白串返回 nil", "   ", nil},
		{"连续多个逗号", "a,,,b", []string{"a", "b"}},
		{"制表符也是空白", "a,\tb", []string{"a", "b"}},
		{"Unicode 空白", "a,\u00a0b", []string{"a", "b"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SplitCSVNonEmpty(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("SplitCSVNonEmpty(%q) = %#v, want %#v", c.in, got, c.want)
			}
		})
	}
}

// TestSplitCSVNonEmpty_NilNotEmptySlice 契约钉死：空输入必须返回 nil，
// 不是空切片——调用方用 != nil 区分「没配」与「配了空」。
func TestSplitCSVNonEmpty_NilNotEmptySlice(t *testing.T) {
	for _, in := range []string{"", "  ", ",", " , "} {
		got := SplitCSVNonEmpty(in)
		if got != nil {
			t.Errorf("SplitCSVNonEmpty(%q) = %#v, want nil", in, got)
		}
	}
}

// TestSplitCSVNonEmpty_NonEmptyNotNil 非空结果必须是非 nil 切片。
func TestSplitCSVNonEmpty_NonEmptyNotNil(t *testing.T) {
	got := SplitCSVNonEmpty("a")
	if got == nil {
		t.Fatal("SplitCSVNonEmpty(\"a\") = nil, want non-nil slice")
	}
	if len(got) != 1 || got[0] != "a" {
		t.Errorf("SplitCSVNonEmpty(\"a\") = %#v, want [\"a\"]", got)
	}
}

// TestSplitCSVNonEmpty_NotSplitOnWhitespace 钉死与 SplitWith 的区别：
// 值里的空格有意义，不能被静默拆成两项。
func TestSplitCSVNonEmpty_NotSplitOnWhitespace(t *testing.T) {
	got := SplitCSVNonEmpty("my glob")
	if len(got) != 1 || got[0] != "my glob" {
		t.Errorf("SplitCSVNonEmpty(%q) = %#v, want [\"my glob\"]", "my glob", got)
	}
}

// TestSplitCSVNonEmpty_DiffersFromSplitByComma 钉死与 SplitByComma 的区别：
// 新原语 trim + 丢空项，SplitByComma 保持原样（不破坏既有语义）。
func TestSplitCSVNonEmpty_DiffersFromSplitByComma(t *testing.T) {
	in := "a, b,,c"
	old := SplitByComma(in)
	wantOld := []string{"a", " b", "", "c"}
	if !reflect.DeepEqual(old, wantOld) {
		t.Errorf("SplitByComma(%q) = %#v, want %#v（既有语义不得被改动）", in, old, wantOld)
	}
	new := SplitCSVNonEmpty(in)
	wantNew := []string{"a", "b", "c"}
	if !reflect.DeepEqual(new, wantNew) {
		t.Errorf("SplitCSVNonEmpty(%q) = %#v, want %#v", in, new, wantNew)
	}
}
