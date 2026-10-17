package singleflighttest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Flight 是被测 singleflight 的最小能力面，与
// concurrency/singleflight.Flight 和 pkg/internal/singleflight.Flight 对齐。
type Flight[T any] interface {
	Do(ctx context.Context, key string, fn func(ctx context.Context) (T, error)) (T, error)
	Forget(key string)
}

// Run 对 factory 构造出的 Flight 跑完整语义契约。factory 每个子测试调用一次。
//
// 契约清单：
//   - 同 key 并发 fn 只执行一次
//   - waiter 独立 ctx 取消不连坐（不拖垮 caller/其他 waiter）
//   - lead panic：waiter 得 error（非伪成功零值），caller 收到 panic，key 清理
//   - Forget 后可重入新 fn
//   - 不同 key 互不阻塞
//   - fn error 原样透传给全部调用方
func Run[T any](t *testing.T, factory func() Flight[T]) {
	t.Helper()
	t.Run("DedupSameKey", func(t *testing.T) { runDedup(t, factory()) })
	t.Run("WaiterCtxCancelIndependent", func(t *testing.T) { runWaiterCtxCancel(t, factory()) })
	t.Run("PanicPropagatesAndCleansKey", func(t *testing.T) { runPanic(t, factory()) })
	t.Run("ForgetAllowsRerun", func(t *testing.T) { runForget(t, factory()) })
	t.Run("DifferentKeysParallel", func(t *testing.T) { runDifferentKeys(t, factory()) })
	t.Run("ErrorPropagates", func(t *testing.T) { runError(t, factory()) })
}

func runDedup[T any](t *testing.T, f Flight[T]) {
	t.Helper()
	var calls atomic.Int32
	const n = 16
	start := make(chan struct{})
	errs := make([]error, n)

	fn := func(ctx context.Context) (T, error) {
		calls.Add(1)
		time.Sleep(30 * time.Millisecond) // 让迟到者进入 waiter
		var zero T
		return zero, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = f.Do(context.Background(), "k", fn)
		}(i)
	}
	close(start)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("fn called %d times, want 1", got)
	}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("caller %d err = %v", i, errs[i])
		}
	}
}

func runWaiterCtxCancel[T any](t *testing.T, f Flight[T]) {
	t.Helper()
	entered := make(chan struct{})
	block := make(chan struct{})

	callerDone := make(chan struct{})
	go func() {
		defer close(callerDone)
		_, _ = f.Do(context.Background(), "k", func(ctx context.Context) (T, error) {
			close(entered)
			<-block
			var zero T
			return zero, nil
		})
	}()
	<-entered // caller 已占住 key

	// waiter：短 ctx，应独立超时退出，不影响 caller
	wctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := f.Do(wctx, "k", func(ctx context.Context) (T, error) {
		t.Error("waiter 不应执行 fn")
		var zero T
		return zero, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		close(block)
		t.Fatalf("waiter err = %v, want DeadlineExceeded", err)
	}
	if time.Since(start) > 500*time.Millisecond {
		close(block)
		t.Fatalf("waiter 等待过久，可能被 caller 连坐")
	}

	// 放行 caller，确认其未被 waiter 取消连坐
	close(block)
	select {
	case <-callerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("caller 被 waiter 取消连坐")
	}
}

func runPanic[T any](t *testing.T, f Flight[T]) {
	t.Helper()
	const n = 8
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once

	fn := func(ctx context.Context) (T, error) {
		once.Do(func() { close(entered) })
		<-release
		panic("boom")
	}

	// caller panic
	panicCh := make(chan any, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicCh <- r
			}
		}()
		_, _ = f.Do(context.Background(), "k", fn)
	}()

	<-entered

	// 先让 waiter 挂到同一 call 上，再放行 panic
	waiterErrs := make(chan error, n)
	joined := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			joined <- struct{}{}
			_, err := f.Do(context.Background(), "k", func(ctx context.Context) (T, error) {
				t.Error("waiter 不应执行 fn")
				var zero T
				return zero, nil
			})
			waiterErrs <- err
		}()
	}
	for i := 0; i < n; i++ {
		<-joined
	}
	time.Sleep(20 * time.Millisecond) // 等待进入 LoadOrStore waiter 分支
	close(release)

	select {
	case r := <-panicCh:
		if r != "boom" {
			t.Fatalf("caller recover = %v, want boom", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("caller 未收到 panic")
	}

	for i := 0; i < n; i++ {
		select {
		case err := <-waiterErrs:
			if err == nil {
				t.Fatal("waiter 不应拿到 nil error（伪成功）")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("waiter 未被唤醒")
		}
	}

	// key 必须已清理，后续 Do 可再次执行
	var again atomic.Int32
	if _, err := f.Do(context.Background(), "k", func(ctx context.Context) (T, error) {
		again.Add(1)
		var zero T
		return zero, nil
	}); err != nil {
		t.Fatalf("panic 后 Do = %v", err)
	}
	if again.Load() != 1 {
		t.Fatalf("panic 后 key 未清理，fn 未重入 (calls=%d)", again.Load())
	}
}

func runForget[T any](t *testing.T, f Flight[T]) {
	t.Helper()
	var calls atomic.Int32
	gate := make(chan struct{})

	go func() {
		_, _ = f.Do(context.Background(), "k", func(ctx context.Context) (T, error) {
			calls.Add(1)
			<-gate
			var zero T
			return zero, nil
		})
	}()
	// 等 caller 进入
	for calls.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	f.Forget("k")

	// Forget 后新调用应能触发新 fn（不与旧 call 共享）
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = f.Do(context.Background(), "k", func(ctx context.Context) (T, error) {
			calls.Add(1)
			var zero T
			return zero, nil
		})
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Forget 后 Do 被旧 call 阻塞")
	}
	if calls.Load() < 2 {
		t.Fatalf("Forget 后未重入新 fn, calls=%d", calls.Load())
	}
	close(gate)
}

func runDifferentKeys[T any](t *testing.T, f Flight[T]) {
	t.Helper()
	var running atomic.Int32
	var max atomic.Int32
	release := make(chan struct{})

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("k%d", i)
			_, _ = f.Do(context.Background(), key, func(ctx context.Context) (T, error) {
				cur := running.Add(1)
				for {
					old := max.Load()
					if cur <= old || max.CompareAndSwap(old, cur) {
						break
					}
				}
				<-release
				running.Add(-1)
				var zero T
				return zero, nil
			})
		}(i)
	}
	// 等两个 fn 都进入
	for running.Load() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	wg.Wait()
	if max.Load() != 2 {
		t.Fatalf("不同 key 应并行执行, max concurrent=%d", max.Load())
	}
}

func runError[T any](t *testing.T, f Flight[T]) {
	t.Helper()
	want := errors.New("fn failed")
	const n = 4
	start := make(chan struct{})
	errs := make([]error, n)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = f.Do(context.Background(), "k", func(ctx context.Context) (T, error) {
				var zero T
				return zero, want
			})
		}(i)
	}
	close(start)
	wg.Wait()
	for i := 0; i < n; i++ {
		if !errors.Is(errs[i], want) {
			t.Fatalf("caller %d err = %v, want %v", i, errs[i], want)
		}
	}
}
