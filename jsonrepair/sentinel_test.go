package jsonrepair

import (
	"errors"
	"strings"
	"testing"
)

// #384 回归：终态错误必须有导出哨兵，调用方经 errors.Is 分流，不再对中文
// 文案做前缀匹配。「空输入」= programmer error；「无法解析」= LLM 输出质量
// 问题（可重试）——两类语义此前只能靠文案区分。

func TestParseLenient_EmptyInputSentinel(t *testing.T) {
	err := ParseLenient("", &map[string]any{}, nil)
	if !errors.Is(err, ErrEmptyInput) {
		t.Fatalf("空输入 err = %v, want ErrEmptyInput", err)
	}
	if errors.Is(err, ErrUnparseable) {
		t.Fatal("空输入不应归为 ErrUnparseable（分流语义相反）")
	}
	// 文案兼容：哨兵文本即历史文案。
	if got, want := err.Error(), "jsonrepair: 空输入"; got != want {
		t.Fatalf("文案漂移: %q != %q", got, want)
	}
	// 仅空白（剥围栏后为空）同口径。
	if err := ParseLenient("   \n\t ", &map[string]any{}, nil); !errors.Is(err, ErrEmptyInput) {
		t.Fatalf("空白输入 err = %v, want ErrEmptyInput", err)
	}
}

func TestParseLenient_UnparseableSentinel(t *testing.T) {
	err := ParseLenient("登录已过期，请重新认证", &map[string]any{}, nil)
	if !errors.Is(err, ErrUnparseable) {
		t.Fatalf("纯垃圾 err = %v, want ErrUnparseable", err)
	}
	if errors.Is(err, ErrEmptyInput) {
		t.Fatal("纯垃圾不应归为 ErrEmptyInput")
	}
	if got, want := err.Error(), "jsonrepair: 无法解析为 JSON"; got != want {
		t.Fatalf("文案漂移: %q != %q", got, want)
	}
}

func TestUnmarshalLenient_UnparseableSentinel(t *testing.T) {
	// 宽松闭合失败同归 ErrUnparseable，文案带后缀（历史文案逐字保留）。
	partial, err := UnmarshalLenient("登录已过期", &map[string]any{})
	if partial {
		t.Fatal("纯垃圾不应报 partial")
	}
	if !errors.Is(err, ErrUnparseable) {
		t.Fatalf("宽松失败 err = %v, want ErrUnparseable", err)
	}
	if !strings.HasPrefix(err.Error(), "jsonrepair: 无法解析为 JSON（宽松模式无法闭合出合法 JSON）") {
		t.Fatalf("文案漂移: %q", err.Error())
	}
}
