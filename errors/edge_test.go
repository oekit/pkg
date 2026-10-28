package errors

import (
	stderrors "errors"
	"fmt"
	"testing"
)

// emptyMessageError 是报文为空串的错误（errors.New("") 或消息由空变量拼出的
// 自定义错误）。这类错误真实存在：消息由配置/变量拼装时可能是空的。
type emptyMessageError struct{}

func (emptyMessageError) Error() string { return "" }

// TestErrorf_WrappedEmptyMessage_NotNil 回归测试：被包装错误的报文为空时，
// Errorf **不得**返回 nil。
//
// 缺陷成因：Errorf 先算 msg，再判 `if msg == "" { return nil }`——「格式化结果
// 为空串返回 nil」的本意是给 `Errorf("")` 一个方便取向，但 %w 的消息来自
// 被包装错误本身，报文为空时整条消息渲染成空串 ⇒ 返回 nil ⇒ 被包装的错误
// 连同它的整条链一起消失 ⇒ 调用方的 `if err != nil` 分支整个跳过，
// 失败被静默吞成成功。
func TestErrorf_WrappedEmptyMessage_NotNil(t *testing.T) {
	cause := emptyMessageError{}
	err := Errorf("%w", cause)
	if err == nil {
		t.Fatal("Errorf(\"%w\", 空报文错误) 返回了 nil：失败被静默吞成成功")
	}
	// 包装链必须仍然可判定。
	if !Is(err, cause) {
		t.Fatal("errors.Is 应能命中被包装的错误")
	}
}

// TestErrorf_WrappedEmptyMessage_MatchesStdlib 与 stdlib fmt.Errorf 对齐：
// 同输入同 nil 性。
func TestErrorf_WrappedEmptyMessage_MatchesStdlib(t *testing.T) {
	cases := []struct {
		format string
		args   []any
	}{
		{"%w", []any{emptyMessageError{}}},
		{"%w", []any{stderrors.New("")}},
		{"ctx: %w", []any{emptyMessageError{}}},
		{"%w and %w", []any{emptyMessageError{}, emptyMessageError{}}},
	}
	for _, c := range cases {
		got := Errorf(c.format, c.args...)
		want := fmt.Errorf(c.format, c.args...)
		if (got == nil) != (want == nil) {
			t.Errorf("format=%q：nil 性与 stdlib 不一致（ours=%v stdlib=%v）", c.format, got == nil, want == nil)
		}
	}
}

// TestErrorf_EmptyFormatStillNil 「无 %w 且消息为空 ⇒ 返回 nil」的原有取向
// 保持不变（这是被既有测试钉住的行为，不在本次修复范围内）。
func TestErrorf_EmptyFormatStillNil(t *testing.T) {
	if err := Errorf(""); err != nil {
		t.Fatalf("Errorf(\"\") 应返回 nil，实得 %v", err)
	}
	// %v 渲染 nil 得到 "<nil>"——非空串 ⇒ 返回错误（与 fmt.Errorf 同口径）。
	if err := Errorf("%v", nil); err == nil {
		t.Fatal("Errorf 渲染 nil 得到非空消息，应返回非 nil 错误")
	}
}

// TestBizError_TypedNil_NoPanic 回归测试：typed-nil（*BizError 赋给 error
// 接口）不得让 Error()/String() 崩，也不得让 AsBizError 报「命中」。
//
// 缺陷成因：Error() 直接解引用 e.Cause（nil 接收者 ⇒ 空指针崩溃）；
// AsBizError 只看 stderrors.As 的返回值——As 对 typed-nil 返回 true，
// 于是 AsBizError 返回 (nil, true)，按文档用法 `if ok { use b.BizCode }`
// 就是一次空指针解引用。
func TestBizError_TypedNil_NoPanic(t *testing.T) {
	var be *BizError
	var err error = be

	if err.Error() == "" {
		t.Fatal("typed-nil 的 Error() 不应返回空串")
	}
	_ = be.String()

	if biz, ok := AsBizError(err); ok {
		t.Fatalf("AsBizError 对 typed-nil 不应报命中（实得 biz=%v ok=%v）", biz, ok)
	}
	if IsBizError(err) {
		t.Fatal("IsBizError 对 typed-nil 不应报 true")
	}
}

// TestBizError_TypedNil_InChain typed-nil 包在真实错误链里时，同样不应崩、
// 不应被当成有效 BizError。
func TestBizError_TypedNil_InChain(t *testing.T) {
	var be *BizError
	chained := fmt.Errorf("layer: %w", error(be))

	if got := chained.Error(); got == "" {
		t.Fatal("链上 typed-nil 的 Error() 不应返回空串")
	}
	if biz, ok := AsBizError(chained); ok {
		t.Fatalf("链上 typed-nil 的 AsBizError 不应报命中（实得 biz=%v ok=%v）", biz, ok)
	}
}

// TestBizError_Normal_StillWorks 正常路径不受影响。
func TestBizError_Normal_StillWorks(t *testing.T) {
	cause := stderrors.New("root cause")
	be := WrapBizError(40404, "项目不存在", cause)

	biz, ok := AsBizError(error(be))
	if !ok || biz == nil {
		t.Fatal("正常的 BizError 应能被提取")
	}
	if biz.BizCode != 40404 {
		t.Fatalf("BizCode 不符：%d", biz.BizCode)
	}
	if !IsBizError(error(be)) {
		t.Fatal("IsBizError 对正常 BizError 应为 true")
	}
	if !Is(error(be), cause) {
		t.Fatal("包装链应保留 cause")
	}
}

// TestMultiError_Unwrap_ReturnsCopy 回归测试：Unwrap() 不得把内部切片直接
// 交出去——调用方改写它就绕过了 Append 的 copy-on-write 保护。
func TestMultiError_Unwrap_ReturnsCopy(t *testing.T) {
	a := stderrors.New("a")
	b := stderrors.New("b")
	merr := &MultiError{errors: []error{a, b}}

	unwrapped := merr.Unwrap()
	if len(unwrapped) != 2 {
		t.Fatalf("Unwrap 长度不符：%d", len(unwrapped))
	}
	unwrapped[0] = stderrors.New("篡改")

	if merr.Errors()[0] != a {
		t.Fatal("Unwrap 交出了内部切片：改写返回值污染了 MultiError 内部状态")
	}
}
