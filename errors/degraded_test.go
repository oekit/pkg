package errors

import (
	stderrors "errors"
	"fmt"
	"testing"
)

// #533：DegradedError 软依赖降级载体的行为契约（语义对齐 hertz/health）。

func TestDegraded_WithErr(t *testing.T) {
	sentinel := stderrors.New("cache ping failed")
	err := Degraded(sentinel)
	if err == nil {
		t.Fatal("Degraded(err) 应返回非 nil error")
	}
	if got := err.Error(); got != "cache ping failed" {
		t.Fatalf("Error() 应透传底层错误文本, got %q", got)
	}
	// error 身份不丢：errors.Is 穿透 DegradedError 直达底层错误。
	if !Is(err, sentinel) {
		t.Fatal("errors.Is 应穿透 DegradedError 命中底层错误")
	}
	var de *DegradedError
	if !As(err, &de) {
		t.Fatal("errors.As 应能取回 *DegradedError 载体")
	}
	if !Is(de.Err, sentinel) {
		t.Fatalf("载体 Err 字段应保有底层错误, got %v", de.Err)
	}
}

func TestDegraded_NilErr_FallbackText(t *testing.T) {
	err := Degraded(nil)
	if err == nil {
		t.Fatal("Degraded(nil) 应返回非 nil error（标记降级本身是事实）")
	}
	if got := err.Error(); got != "degraded" {
		t.Fatalf("nil Err 时 Error() 应回退 %q, got %q", "degraded", got)
	}
	if Unwrap(err) != nil {
		t.Fatalf("nil Err 时 Unwrap 应为 nil, got %v", Unwrap(err))
	}
	// nil Err 的 DegradedError 不应误判到任何哨兵。
	if Is(err, stderrors.New("anything")) {
		t.Fatal("nil Err 的 DegradedError 不应被 Is 判到无关错误")
	}
}

func TestDegradedError_AsWithinWrappedChain(t *testing.T) {
	sentinel := stderrors.New("cache down")
	// 典型形态：上层经 %w 再包一层后，As/Is 仍穿透到 DegradedError 与底层。
	wrapped := fmt.Errorf("health check: %w", Degraded(sentinel))

	var de *DegradedError
	if !As(wrapped, &de) {
		t.Fatal("包装链上 errors.As 应命中 *DegradedError")
	}
	if !Is(wrapped, sentinel) {
		t.Fatal("包装链上 errors.Is 应命中底层错误")
	}
}

func TestDegradedError_TypedNilInInterface(t *testing.T) {
	// Degraded(nil) 返回的是装着 *DegradedError（Err=nil）的非 nil 接口值
	// ——不是 nil 接口：调用方 `return Degraded(err)` 的结果必须始终能被
	// `err != nil` 看到（health.Degraded(nil) 若返回 nil 接口会静默丢降级标记）。
	err := Degraded(nil)
	if err == nil {
		t.Fatal("Degraded(nil) 不得返回 nil 接口值")
	}
	var de *DegradedError
	if !As(err, &de) || de.Err != nil {
		t.Fatalf("应能取回 Err=nil 的 *DegradedError, got %v", err)
	}
}

func TestDegradedError_MultiError_Interop(t *testing.T) {
	// 与包内 MultiError 互操作：聚合进 MultiError 的降级错误仍可 As/Is 判定。
	sentinel := stderrors.New("bypass service down")
	agg := Append(nil, Degraded(sentinel))
	var de *DegradedError
	if !As(agg, &de) {
		t.Fatal("MultiError 聚合后 errors.As 仍应命中 *DegradedError")
	}
	if !Is(agg, sentinel) {
		t.Fatal("MultiError 聚合后 errors.Is 仍应命中底层错误")
	}
}
