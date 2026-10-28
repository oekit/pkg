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
//   - **一次飞行结束后，新调用必须重新执行 fn**（key 已释放，不得复用上次的
//     结果）
//   - **Forget 之后，旧 flight 的收尾不得删掉同 key 的新 call**（否则
//     「同 key 只执行一次 fn」的承诺被打破，且两次 fn 并发跑）
func Run[T any](t *testing.T, factory func() Flight[T]) {
	t.Helper()
	t.Run("DedupSameKey", func(t *testing.T) { runDedup(t, factory()) })
	t.Run("WaiterCtxCancelIndependent", func(t *testing.T) { runWaiterCtxCancel(t, factory()) })
	t.Run("PanicPropagatesAndCleansKey", func(t *testing.T) { runPanic(t, factory()) })
	t.Run("ForgetAllowsRerun", func(t *testing.T) { runForget(t, factory()) })
	t.Run("ForgetDoesNotDeleteNewerCall", func(t *testing.T) { runForgetDoesNotDeleteNewerCall(t, factory()) })
	t.Run("DifferentKeysParallel", func(t *testing.T) { runDifferentKeys(t, factory()) })
	t.Run("ErrorPropagates", func(t *testing.T) { runError(t, factory()) })
	t.Run("AfterFlightNewCallRerunsFn", func(t *testing.T) { runAfterFlightReruns(t, factory()) })
}

// runForgetDoesNotDeleteNewerCall 契约：**Forget 之后，旧 flight 收尾时不得把
// 同 key 的新 call 一起删掉**。
//
// 缺陷成因（无条件 Delete）：序列
//
//	G1 注册 c1（慢 fn）→ Forget("k") 删掉 c1 → G2 注册 c2 → G1 结束 →
//
// Delete("k") 把 c2 一起删掉 → G3 于是又起了第三次 fn ⇒「同 key 只执行
// 一次 fn」的承诺被打破，且两次 fn 并发跑。
//
// 为什么这条要进**共享**契约套件：矩阵里两套同源实现
// （pkg/internal/singleflight 与 concurrency/singleflight）此前都栽在这里，
// 而旧的 runForget 只覆盖「Forget → 单次新 Do」⇒ 这个时序在套件里
// **不可观测** ⇒ 两套实现都能通过，缺陷却各自留存。
func runForgetDoesNotDeleteNewerCall[T any](t *testing.T, f Flight[T]) {
	t.Helper()
	var calls atomic.Int32
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})

	// G1：慢 fn，注册 c1。
	go func() {
		_, _ = f.Do(context.Background(), "k", func(ctx context.Context) (T, error) {
			calls.Add(1)
			close(firstStarted)
			<-releaseFirst
			var zero T
			return zero, nil
		})
	}()
	<-firstStarted

	// Forget：c1 从表里摘掉。
	f.Forget("k")

	// G2：注册 c2（同 key，**仍在飞行中**的慢 fn）。
	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		_, _ = f.Do(context.Background(), "k", func(ctx context.Context) (T, error) {
			calls.Add(1)
			close(secondStarted)
			<-releaseSecond
			var zero T
			return zero, nil
		})
	}()
	<-secondStarted

	// G1 结束：它的收尾**不得**删掉还在飞行中的 c2。
	close(releaseFirst)
	time.Sleep(50 * time.Millisecond)

	// G3 到达：c2 仍在飞行中 ⇒ 应**加入**它，而不是另起一次 fn。
	thirdDone := make(chan struct{})
	go func() {
		defer close(thirdDone)
		_, _ = f.Do(context.Background(), "k", func(ctx context.Context) (T, error) {
			calls.Add(1)
			var zero T
			return zero, nil
		})
	}()
	select {
	case <-thirdDone:
		t.Fatal("G3 立即返回了：它没有加入仍在飞行中的 c2（旧 flight 的收尾误删了新 call）")
	case <-time.After(200 * time.Millisecond):
	}

	if got := calls.Load(); got != 2 {
		t.Fatalf("fn 执行次数 = %d，期望 2（旧 flight 的收尾误删了仍在飞行中的新 call ⇒ G3 又起了一次）", got)
	}
	close(releaseSecond)
	<-secondDone
	<-thirdDone
}

func runDedup[T any](t *testing.T, f Flight[T]) {
	t.Helper()
	var calls atomic.Int32
	const n = 16
	start := make(chan struct{})
	errs := make([]error, n)

	fn := func(_ context.Context) (T, error) { //nolint:unparam // 契约桩恒返回零值——聚焦同 key 去重语义
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
	// Do 放在 goroutine 里 + **有界等待**：实现若完全忽略 ctx，Do 会一直阻塞，
	// 而 block 只在 Do 返回后才关 ⇒ 死锁（测试挂到 go test 的 10 分钟超时，
	// 而不是干脆地失败）。
	errCh := make(chan error, 1)
	go func() {
		_, err := f.Do(wctx, "k", func(ctx context.Context) (T, error) {
			t.Error("waiter 不应执行 fn")
			var zero T
			return zero, nil
		})
		errCh <- err
	}()
	var err error
	select {
	case err = <-errCh:
	case <-time.After(2 * time.Second):
		close(block)
		t.Fatal("waiter 未响应 ctx 取消：实现忽略了调用方的 ctx（不应挂死）")
	}
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

	fn := func(_ context.Context) (T, error) {
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
	// 等 caller 进入（**有界**：实现若永远不调 fn，这里必须干脆地失败而不是
	// 无限轮询挂到 go test 的 10 分钟超时）。
	waitFor(t, "caller 未进入 fn", func() bool { return calls.Load() > 0 })
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
	// 等两个 fn 都进入（**有界**：实现若把不同 key 串行化了，这里必须干脆地
	// 失败而不是无限轮询挂死）。
	waitFor(t, "不同 key 未并行执行：第二个 fn 未进入", func() bool { return running.Load() >= 2 })
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

// runAfterFlightReruns 钉住「一次飞行结束后，新调用必须重新执行 fn」。
//
// 这是 key 释放语义的**可观测**部分：飞行结束后 key 必须已从实现里摘掉，
// 否则后续调用会复用上一次的 call、拿到**陈旧结果**而不执行 fn。
//
// 注：实现内部「先摘键后唤醒」与「先唤醒后摘键」的差别落在飞行完成与唤醒之间
// 的一个极窄窗口里，**通过公开 API 无法确定观测**（并发到达的调用方拿到上一次
// 结果是**合法的去重**，与陈旧结果不可区分）⇒ 本用例只钉住可观测的性质；
// 窄窗口由实现仓自己的用例守住。
func runAfterFlightReruns[T any](t *testing.T, f Flight[T]) {
	t.Helper()
	var calls atomic.Int32
	fn := func(_ context.Context) (T, error) {
		calls.Add(1)
		var zero T
		return zero, nil
	}
	for i := 0; i < 3; i++ {
		if _, err := f.Do(context.Background(), "k", fn); err != nil {
			t.Fatalf("第 %d 次 Do = %v", i+1, err)
		}
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("飞行结束后新调用未重新执行 fn (calls=%d，期望 3)——key 未释放", got)
	}
}

// waitFor 有界等待 cond 成立；超时则 t.Fatal（并给出原因）。
//
// 契约套件里所有「等实现进入某个状态」的等待都必须有界：实现有缺陷时，
// 正确表现是**干脆地失败**，而不是把 CI 挂到 go test 的 10 分钟超时。
func waitFor(t *testing.T, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(2 * time.Millisecond)
	}
}
