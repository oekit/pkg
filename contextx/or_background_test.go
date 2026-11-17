package contextx

import (
	"context"
	"testing"
	"time"
)

// #529：OrBackground 的 nil 归一契约（矩阵 #159 内联写法的收敛目标）。

func TestOrBackground_NilReturnsBackground(t *testing.T) {
	// 用 nil 变量而非 nil 字面量：staticcheck 的 SA1012 只拦 nil 字面量，
	// 而本用例的**目的**就是传 nil（nil_parent_test.go 同款纪律）。
	var nilCtx context.Context
	ctx := OrBackground(nilCtx)
	if ctx == nil {
		t.Fatal("nil ctx 应归一为 context.Background()，不得原样透出")
	}
	// 结果具备 Background 语义：永不取消、无 deadline。
	if ctx.Done() != nil {
		t.Error("Background 的 Done 应为 nil")
	}
	if ctx.Err() != nil {
		t.Error("Background 的 Err 应为 nil")
	}
	if _, ok := ctx.Deadline(); ok {
		t.Error("Background 不应有 deadline")
	}
	if ctx.Value(struct{}{}) != nil {
		t.Error("Background 不应携带任何值")
	}
}

func TestOrBackground_NonNil_PassedThroughVerbatim(t *testing.T) {
	type key struct{}
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "v"))
	defer cancel()

	got := OrBackground(parent)
	// 原样透传：同一 ctx 值（接口相等），值可透传。
	if got != parent {
		t.Fatal("非 nil ctx 应原样返回，不得包装")
	}
	if got.Value(key{}) != "v" {
		t.Errorf("Value 应原样可用, got %v", got.Value(key{}))
	}
}

func TestOrBackground_CancelledCtx_NotRevived(t *testing.T) {
	// ⚠️ 文档承诺的护栏：已取消的 ctx 原样透传，取消语义不被吞掉。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := OrBackground(ctx)
	if got != ctx {
		t.Fatal("已取消 ctx 应原样返回")
	}
	if err := got.Err(); err != context.Canceled {
		t.Fatalf("取消信号必须保持可见, got %v", err)
	}
	select {
	case <-got.Done():
	default:
		t.Fatal("Done 通道应已关闭")
	}
}

func TestOrBackground_DeadlineCtx_NotRevived(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), -time.Second)
	defer cancel()

	got := OrBackground(ctx)
	if got != ctx {
		t.Fatal("已过期 ctx 应原样返回")
	}
	if err := got.Err(); err != context.DeadlineExceeded {
		t.Fatalf("过期信号必须保持可见, got %v", err)
	}
}
