package stringx

import (
	"encoding/csv"
	"reflect"
	"strings"
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

// TestSplitCSVNonEmpty_QuotedFields 回归（[#259]）：此前实现是裸
// strings.Split，不认 RFC 4180 引号——`"a,b",c` 被静默切成 ["\"a"
// "b\"" "c"]，含逗号的引号字段（地址、JSON 片段、SQL 条件、带逗号的
// 产品名）拆成两段且不报错；引号还被原样保留（`"x"` → `"x"`），既不
// 解释也不剥离。
func TestSplitCSVNonEmpty_QuotedFields(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		// #258/#259 issue 复现样本
		{"引号内逗号不切分", `"a,b",c`, []string{"a,b", "c"}},
		{"引号剥离", `"x"`, []string{"x"}},
		{"既有口径不变", "a, b,,c", []string{"a", "b", "c"}},
		{"多字段含引号", `a,"b,c",d`, []string{"a", "b,c", "d"}},
		{"尾字段含引号", `"a,b",c,d`, []string{"a,b", "c", "d"}},
		{"双引号转义为字面引号", `"a""b",c`, []string{`a"b`, "c"}},
		{"引号内空白是值的一部分", `" a , b" , c`, []string{"a , b", "c"}},
		{"引号外空白仍 trim", `"a,b" ,c`, []string{"a,b", "c"}},
		{"闭合引号后残余宽松并入", `"a"x,b`, []string{"ax", "b"}},
		{"未闭合引号宽松兜底", `"a,b`, []string{"a,b"}},
		{"未闭合引号带后续段", `"a,b,c,d`, []string{"a,b,c,d"}},
		{"引号空字段丢弃", `"",a`, []string{"a"}},
		{"全引号空字段返回 nil", `""`, nil},
		{"裸引号在字段中部按字面保留", `a"b,c`, []string{`a"b`, "c"}},
		{"Unicode 内容含逗号", `"产品,名称",x`, []string{"产品,名称", "x"}},
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

// TestSplitCSVNonEmpty_MatchesStdlibCSV 与标准库 encoding/csv 交叉验证：
// 对一行规范的 CSV 记录（issue 建议的实现路径），两条实现拆出的字段
// 序列必须一致（本函数多做的只有 trim 与空项丢弃）。
func TestSplitCSVNonEmpty_MatchesStdlibCSV(t *testing.T) {
	for _, row := range []string{
		`a,b,c`,
		`"a,b",c`,
		`"x"`,
		`a,"b,c",d`,
		`"a""b",c`,
		`"addr, zip",name,2`,
		`one`,
		`"multi, comma", tail, x`,
	} {
		rec, err := csv.NewReader(strings.NewReader(row)).Read()
		if err != nil {
			t.Fatalf("stdlib csv 读 %q 失败: %v", row, err)
		}
		var want []string
		for _, f := range rec {
			if f = strings.TrimSpace(f); f != "" {
				want = append(want, f)
			}
		}
		if len(want) == 0 {
			want = nil
		}
		if got := SplitCSVNonEmpty(row); !reflect.DeepEqual(got, want) {
			t.Errorf("SplitCSVNonEmpty(%q) = %#v, 与 stdlib csv 口径 %#v 不一致", row, got, want)
		}
	}
}
