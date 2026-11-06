package convert

import (
	"strings"
	"testing"
)

// structContractSrc / structContractDst 是 #207 用例的合成夹具。
type structContractSrc struct {
	Name  string
	Age   int
	OnlyS int
}

type structContractDst struct {
	Name  string
	Age   int
	OnlyD int
}

// TestConvertStruct_ContractViolations 钉住「入参不满足契约时报错，不静默
// no-op」（[#207]）。静默 no-op 是最难排查的形态：调用方拿到 nil 就认为 dst
// 已填充，实际一个字都没变，错误在下游很远的地方才暴露，且现场已无迹可循。
func TestConvertStruct_ContractViolations(t *testing.T) {
	var nilDst *structContractDst
	var nilSrc *structContractSrc

	tests := []struct {
		name    string
		src     any
		dst     any
		wantSub string
	}{
		{"dst 为 nil 接口值", structContractSrc{}, nil, "dst must be a non-nil pointer"},
		{"dst 为 nil typed pointer", structContractSrc{}, nilDst, "dst must be a non-nil pointer"},
		{"dst 非指针（结构体值）", structContractSrc{}, structContractDst{}, "dst must be a non-nil pointer"},
		{"dst 非指针（标量）", structContractSrc{}, 42, "dst must be a non-nil pointer"},
		{"dst 是指针但不是指向结构体", structContractSrc{}, new(int), "dst must point to a struct"},
		{"src 为 nil 接口值", nil, &structContractDst{}, "src must be a struct"},
		{"src 为 nil typed pointer", nilSrc, &structContractDst{}, "src must be a struct"},
		{"src 不是结构体", 42, &structContractDst{}, "src must be a struct"},
		{"src 指向的不是结构体", new(int), &structContractDst{}, "src must be a struct"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ConvertStruct(tt.src, tt.dst)
			if err == nil {
				t.Fatalf("ConvertStruct(%T, %T) = nil, want contract error", tt.src, tt.dst)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantSub)
			}
		})
	}
}

// TestConvertStruct_HappyPathAndFieldLevelTolerance 是上一条的**成对正例**：
// 入参合法时必须成功，且字段层面仍是尽力而为——src 独有的字段、dst 独有的
// 字段、类型不一致的字段都跳过而不报错。若把「字段不匹配」也升级成错误，
// 这条会红，从而防止本次修复把契约收得过紧、破坏既有调用方。
func TestConvertStruct_HappyPathAndFieldLevelTolerance(t *testing.T) {
	dst := &structContractDst{OnlyD: 7}
	if err := ConvertStruct(structContractSrc{Name: "alice", Age: 30, OnlyS: 99}, dst); err != nil {
		t.Fatalf("ConvertStruct: %v", err)
	}
	if dst.Name != "alice" || dst.Age != 30 {
		t.Errorf("同名字段未复制: %+v", dst)
	}
	if dst.OnlyD != 7 {
		t.Errorf("dst 独有字段被覆盖: OnlyD = %d, want 7", dst.OnlyD)
	}

	// src 也可以是指向结构体的指针
	dst2 := &structContractDst{}
	if err := ConvertStruct(&structContractSrc{Name: "bob", Age: 41}, dst2); err != nil {
		t.Fatalf("ConvertStruct(ptr): %v", err)
	}
	if dst2.Name != "bob" || dst2.Age != 41 {
		t.Errorf("指针 src 未被解引用: %+v", dst2)
	}

	// 类型不一致的字段必须跳过而非报错
	type mismatchSrc struct {
		Name int
		Age  int
	}
	dst3 := &structContractDst{Name: "keep"}
	if err := ConvertStruct(mismatchSrc{Name: 123, Age: 9}, dst3); err != nil {
		t.Fatalf("ConvertStruct(mismatched type): %v", err)
	}
	if dst3.Name != "keep" {
		t.Errorf("类型不一致的字段不应被写入: Name = %q, want \"keep\"", dst3.Name)
	}
	if dst3.Age != 9 {
		t.Errorf("类型一致的字段应仍被写入: Age = %d, want 9", dst3.Age)
	}
}
