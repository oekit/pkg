package contextx

import (
	"context"
	"testing"
)

// TestValueOnlyFrom_NilParent_Panics 回归测试：nil parent 必须**立即** panic。
//
// 缺陷成因：ValueOnlyFrom 不校验 parent ⇒ nil 被塞进 valueOnly ⇒ 后面某次
// Value 调用里 `v.ctx.Value(key)` 触发 nil 接口解引用 ⇒ 崩在离「传错了」
// 很远的地方，堆栈看不出根因。标准库 context.WithValue 对 nil parent 是
// 当场 panic 的。
func TestValueOnlyFrom_NilParent_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("nil parent 应立即 panic")
		}
	}()
	// 用 nil 变量而非 nil 字面量：staticcheck 的 SA1012 只拦 nil 字面量，
	// 而本用例的**目的**就是传 nil。
	var nilParent context.Context
	_ = ValueOnlyFrom(nilParent)
}

// TestValueOnlyFrom_StripsCancel 回归护栏：非 nil parent 时，Done/Err/Deadline
// 被剥离、Value 透传。
func TestValueOnlyFrom_StripsCancel(t *testing.T) {
	type key struct{}
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "v"))
	cancel()

	ctx := ValueOnlyFrom(parent)
	if ctx.Done() != nil {
		t.Error("Done 应被剥离")
	}
	if ctx.Err() != nil {
		t.Error("Err 应被剥离")
	}
	if _, ok := ctx.Deadline(); ok {
		t.Error("Deadline 应被剥离")
	}
	if got := ctx.Value(key{}); got != "v" {
		t.Errorf("Value 应透传，实得 %v", got)
	}
}
