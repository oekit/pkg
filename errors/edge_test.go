package errors

import (
	stderrors "errors"
	"fmt"
	"testing"
)

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
