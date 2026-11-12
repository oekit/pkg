package errors

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestAppend_NilLeft(t *testing.T) {
	err := Append(nil, errors.New("right"))
	if err == nil {
		t.Error("expected non-nil")
	}
	if err.Error() != "right" {
		t.Errorf("expected 'right', got %q", err.Error())
	}
}

func TestAppend_NilRight(t *testing.T) {
	err := Append(errors.New("left"), nil)
	if err == nil {
		t.Error("expected non-nil")
	}
	if err.Error() != "left" {
		t.Errorf("expected 'left', got %q", err.Error())
	}
}

func TestAppend_BothNil(t *testing.T) {
	err := Append(nil, nil)
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestAppend_TwoErrors(t *testing.T) {
	err := Append(errors.New("a"), errors.New("b"))
	if err == nil {
		t.Fatal("expected non-nil")
	}
	if err.Error() != "a; b" {
		t.Errorf("expected 'a; b', got %q", err.Error())
	}
}

func TestAppend_Multiple(t *testing.T) {
	var err error
	err = Append(err, errors.New("a"))
	err = Append(err, errors.New("b"))
	err = Append(err, errors.New("c"))

	if err.Error() != "a; b; c" {
		t.Errorf("expected 'a; b; c', got %q", err.Error())
	}
}

func TestMultiError_Is(t *testing.T) {
	target := errors.New("target")
	err := Append(errors.New("other"), target)

	if !errors.Is(err, target) {
		t.Error("expected Is to find target")
	}
}

func TestMultiError_Is_Nil(t *testing.T) {
	var err *MultiError
	if errors.Is(err, errors.New("any")) {
		t.Error("expected false for nil MultiError")
	}
}

func TestMultiError_Errors(t *testing.T) {
	err := Append(errors.New("a"), errors.New("b"))
	merr, ok := err.(*MultiError)
	if !ok {
		t.Fatal("expected *MultiError")
	}
	errs := merr.Errors()
	if len(errs) != 2 {
		t.Errorf("expected 2 errors, got %d", len(errs))
	}
}

func TestMultiError_Error_Nil(t *testing.T) {
	var err *MultiError
	if err.Error() != "" {
		t.Errorf("expected empty string, got %q", err.Error())
	}
}

// TestAppend_ConcurrentSameMultiError 并发 Append 同一 *MultiError（pkg#237）：
// COW 路径此前直接 append(l.errors, right)——backing array 有余量时写进共享
// 数组，与并发 fromSlice（嵌套展开读同一数组）构成数据竞态。修复后 COW 路径
// 先 slices.Clone 再 append，每次写新数组；本测试在 -race 下并发 Append 同一
// 多错误实例（seed 刻意留出大量余量，保证 COW 路径命中共享数组的写场景），
// 并校验每条链的错误数量完整。
//
// 常规 go test 与 go test -race 均须通过。
func TestAppend_ConcurrentSameMultiError(t *testing.T) {
	const goroutines = 8
	const each = 200

	// 刻意构造有余量的 backing array：cap 远大于 len，append 不会触发重新
	// 分配——修复前 COW 路径的写入全部落在这个共享数组里。
	seed := make([]error, 1, 1+goroutines*each)
	seed[0] = errors.New("seed")
	base := &MultiError{errors: seed}

	results := make([]error, goroutines)
	var wg sync.WaitGroup
	for i := range results {
		wg.Go(func() {
			acc := error(base)
			for j := range each {
				acc = Append(acc, fmt.Errorf("g%d-e%d", i, j))
			}
			results[i] = acc
		})
	}
	wg.Wait()

	for i, acc := range results {
		merr, ok := acc.(*MultiError)
		if !ok {
			t.Fatalf("goroutine %d: 结果应为 *MultiError，got %T", i, acc)
		}
		errs := merr.Errors()
		if len(errs) != each+1 {
			t.Fatalf("goroutine %d: 错误数 = %d, want %d（seed + 每轮追加）", i, len(errs), each+1)
		}
		if errs[0].Error() != "seed" {
			t.Fatalf("goroutine %d: 首错误应为 seed，got %v", i, errs[0])
		}
		// 逐链校验无错位/丢错：Error() 应包含本链全部错误标记
		want := fmt.Sprintf("g%d-e%d", i, each-1)
		if !containsErrorMessage(merr, want) {
			t.Fatalf("goroutine %d: 聚合结果缺少末尾错误 %q：%v", i, want, merr)
		}
	}
}

// containsErrorMessage 判断聚合错误中是否有子错误的文案等于 msg。
func containsErrorMessage(merr *MultiError, msg string) bool {
	for _, e := range merr.Errors() {
		if e.Error() == msg {
			return true
		}
	}
	return false
}
