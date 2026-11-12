package errors

import (
	stderrors "errors"
	"fmt"
	"testing"
)

// ---- #47：带格式化的构造原语（Errorf） ----

var errBoom = stderrors.New("boom")

func TestErrorf_PlainFormat(t *testing.T) {
	err := Errorf("load profile %s failed after %d tries", "u42", 3)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if got, want := err.Error(), "load profile u42 failed after 3 tries"; got != want {
		t.Fatalf("消息不符：%q != %q", got, want)
	}
}

func TestErrorf_WrapSingle(t *testing.T) {
	err := Errorf("load profile %s: %w", "u42", errBoom)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if got, want := err.Error(), "load profile u42: boom"; got != want {
		t.Fatalf("消息不符：%q != %q", got, want)
	}
	if !Is(err, errBoom) {
		t.Fatal("errors.Is 应识别被包装的错误")
	}
	if Unwrap(err) != errBoom {
		t.Fatalf("Unwrap 应返回被包装的错误：%v", Unwrap(err))
	}
}

func TestErrorf_WrapMultiple(t *testing.T) {
	errA := stderrors.New("a-fail")
	errB := stderrors.New("b-fail")
	err := Errorf("聚合: %w / %w", errA, errB)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if got, want := err.Error(), "聚合: a-fail / b-fail"; got != want {
		t.Fatalf("消息不符：%q != %q", got, want)
	}
	if !Is(err, errA) || !Is(err, errB) {
		t.Fatal("errors.Is 应识别链上每个错误")
	}
}

func TestErrorf_NilWrapArgIsSafe(t *testing.T) {
	// %w 的实参为 nil：不得产出 "%!w(<nil>)" 噪声，也不得 panic。
	err := Errorf("load profile %s: %w", "u42", error(nil))
	if err == nil {
		t.Fatal("应返回错误")
	}
	if got, want := err.Error(), "load profile u42: <nil>"; got != want {
		t.Fatalf("消息不符：%q != %q", got, want)
	}
	if Is(err, errBoom) {
		t.Fatal("nil 实参不应进包装链")
	}
}

func TestErrorf_NonErrorWrapArgIsSafe(t *testing.T) {
	// %w 的实参不是 error：按 %v 渲染，不进包装链，也不 panic。
	err := Errorf("值: %w", "not-an-error")
	if err == nil {
		t.Fatal("应返回错误")
	}
	if got, want := err.Error(), "值: not-an-error"; got != want {
		t.Fatalf("消息不符：%q != %q", got, want)
	}
}

func TestErrorf_EmptyMessageReturnsNil(t *testing.T) {
	if err := Errorf(""); err != nil {
		t.Fatalf("格式化结果为空串时应返回 nil，实得 %v", err)
	}
}

func TestErrorf_EscapedPercent(t *testing.T) {
	err := Errorf("100%% done: %w", errBoom)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if got, want := err.Error(), "100% done: boom"; got != want {
		t.Fatalf("消息不符：%q != %q", got, want)
	}
	if !Is(err, errBoom) {
		t.Fatal("errors.Is 应识别被包装的错误")
	}
}

func TestErrorf_ExplicitArgIndex(t *testing.T) {
	err := Errorf("%[2]w 里的 %[1]s", "ctx", errBoom)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if got, want := err.Error(), "boom 里的 ctx"; got != want {
		t.Fatalf("消息不符：%q != %q", got, want)
	}
	if !Is(err, errBoom) {
		t.Fatal("errors.Is 应识别被包装的错误")
	}
}

// TestErrorf_WrapArgIndexNotShifted 钉住「%w 的实参下标不被前面的动词带偏」：
// %w 前若有其它动词，取错下标会让 errors.Is 判定失效。
func TestErrorf_WrapArgIndexNotShifted(t *testing.T) {
	err := Errorf("%s %d %v: %w", "a", 1, true, errBoom)
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !Is(err, errBoom) {
		t.Fatal("errors.Is 应识别被包装的错误")
	}
	if got, want := err.Error(), "a 1 true: boom"; got != want {
		t.Fatalf("消息不符：%q != %q", got, want)
	}
}

// TestErrorf_AsThroughChain errors.As 应穿过包装链命中目标类型。
func TestErrorf_AsThroughChain(t *testing.T) {
	// 真实目标：BizError 经包装链仍可被 As 取到
	be := NewNotFoundError("用户不存在")
	wrapped := Errorf("查用户: %w", be)
	got, ok := AsBizError(wrapped)
	if !ok {
		t.Fatal("AsBizError 应命中包装链上的 BizError")
	}
	if got != be {
		t.Fatalf("取到的 BizError 应是同一个：%v", got)
	}

	// 自定义错误类型同样能穿过包装链被 As 取到
	var te *targetErr
	if !As(Errorf("外层: %w", &targetErr{code: 7}), &te) {
		t.Fatal("errors.As 应命中包装链上的自定义错误类型")
	}
	if te.code != 7 {
		t.Fatalf("取到的目标类型内容不符：%d", te.code)
	}
}

// targetErr 是测试用的自定义错误类型。
type targetErr struct{ code int }

func (e *targetErr) Error() string { return fmt.Sprintf("target %d", e.code) }

// TestErrorf_MessageParityWithStdlib 消息必须与 fmt.Errorf 逐字一致（nil 实参除外）：
// 从 fmt.Errorf 迁过来的消费方，日志与告警文案不能变。
func TestErrorf_MessageParityWithStdlib(t *testing.T) {
	cases := []struct {
		format string
		args   []any
	}{
		{"load %s: %w", []any{"u42", errBoom}},
		{"聚合 %w 和 %w", []any{errBoom, errBoom}},
		{"%s=%d %v", []any{"k", 7, true}},
		{"%q %x", []any{"a", 255}},
		{"100%% ok", nil},
		{"宽度 %-6s|", []any{"ab"}},
		{"精度 %.2f", []any{1.239}},
	}
	for _, c := range cases {
		want := fmt.Errorf(c.format, c.args...)
		got := Errorf(c.format, c.args...)
		if got == nil || want == nil {
			t.Fatalf("nil 不一致：format=%q", c.format)
		}
		if got.Error() != want.Error() {
			t.Fatalf("消息与 stdlib 不一致：format=%q got=%q want=%q", c.format, got.Error(), want.Error())
		}
	}
}

// missingArgFormat 走变量而非常量：故意少传一个实参，
// 同时绕开 vet 的 printf 实参个数检查（该检查要求格式串为常量）。
var missingArgFormat = "缺 %s 和 %d"

// TestErrorf_MissingArgMatchesStdlib 实参不足时与 stdlib 同样渲染（%!d(MISSING)）。
func TestErrorf_MissingArgMatchesStdlib(t *testing.T) {
	want := fmt.Errorf(missingArgFormat, "a")
	got := Errorf(missingArgFormat, "a")
	if got.Error() != want.Error() {
		t.Fatalf("消息与 stdlib 不一致：got=%q want=%q", got.Error(), want.Error())
	}
}

// TestErrorf_NoPanicOnHostileFormats 畸形格式串不得 panic。
func TestErrorf_NoPanicOnHostileFormats(t *testing.T) {
	for _, format := range []string{
		"%", "%w", "%%w", "%[1]w", "%[999]w", "%w %w %w", "%*w", "%.*w", "%+w", "%-10w",
		"%w", "%w", "%[1]w %[2]w", "abc%", "%!w(",
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("format=%q 引发 panic：%v", format, r)
				}
			}()
			_ = Errorf(format, "x", 1, errBoom)
		}()
	}
}

// TestErrorf_DifferentialAgainstStdlib 差分测试：以 stdlib fmt.Errorf 为参照实现，
// 逐字比对消息、并比对 errors.Is 的判定结果。
//
// 参照实现与被检对象不是同一份代码——判据不与被检对象共享逻辑。
// 这里排除含 nil %w 实参的组合（那正是本原语与 stdlib 的**有意差异**）。
func TestErrorf_DifferentialAgainstStdlib(t *testing.T) {
	e1 := stderrors.New("e1")
	e2 := stderrors.New("e2")
	cases := []struct {
		format string
		args   []any
	}{
		{"%w", []any{e1}},
		{"%w", []any{e1, e2}},
		{"a %w b %w c", []any{e1, e2}},
		{"%s: %w", []any{"ctx", e1}},
		{"%d: %w", []any{42, e1}},
		{"%v: %w", []any{true, e1}},
		{"%q %w", []any{"q", e1}},
		{"%x %w", []any{255, e1}},
		{"%T %w", []any{1, e1}},
		{"%08.3f %w", []any{1.5, e1}},
		{"%-8s| %w", []any{"ab", e1}},
		{"%[2]w %[1]s", []any{"ctx", e1}},
		{"%[1]w %[2]s", []any{e1, "ctx"}},
		{"%[2]s %[1]w", []any{e1, "ctx"}},
		{"100%% %w", []any{e1}},
		{"%*d %w", []any{5, 7, e1}},
		{"%.*f %w", []any{2, 1.239, e1}},
		{"%s %s %s %w", []any{"a", "b", "c", e1}},
	}
	for _, c := range cases {
		want := fmt.Errorf(c.format, c.args...)
		got := Errorf(c.format, c.args...)
		if got == nil || want == nil {
			t.Fatalf("nil 不一致：format=%q", c.format)
		}
		if got.Error() != want.Error() {
			t.Fatalf("消息与 stdlib 不一致：format=%q got=%q want=%q", c.format, got.Error(), want.Error())
		}
		// 包装链判定：被包装的错误应能被 errors.Is 命中。
		for _, target := range []error{e1, e2} {
			if Is(want, target) != Is(got, target) {
				t.Fatalf("errors.Is 判定与 stdlib 不一致：format=%q target=%v stdlib=%v ours=%v",
					c.format, target, Is(want, target), Is(got, target))
			}
		}
	}
}
