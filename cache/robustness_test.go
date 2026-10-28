package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oekit/pkg/internal/singleflight"
)

// TestChecker_RefreshPanic_DoesNotWedge 回归测试：refresh 里的 panic 不得把
// Checker 彻底废掉。
//
// 缺陷成因：清理（`c.inflight = nil` + `close(call.done)`）写在 refresh 之后，
// refresh 一旦 panic 清理就不会执行 ⇒ inflight 永远挂着、done 永远不关 ⇒
// 后续每一次 Check 都卡在 `<-call.done` 上（context.Background() 下是**永久
// 挂起**，不是报错）——一次 refresh 的 panic 让 Checker 只剩第一次能用。
//
// 期望语义（与 internal/singleflight 同型）：panic 转成 error 交给等待者，
// panic 原样抛给首个调用方；Checker 本身保持可用。
func TestChecker_RefreshPanic_DoesNotWedge(t *testing.T) {
	c := NewChecker(func(ctx context.Context) error { panic("refresh boom") }, time.Millisecond, time.Millisecond)

	// 首个调用者：panic 原样传播。
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("refresh 的 panic 应原样传播给首个调用方")
			}
		}()
		_ = c.Check(context.Background())
	}()

	// 后续调用者：不得永久挂起（无 ctx 期限 ⇒ 卡住就是永久挂起）。
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Check(context.Background())
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh panic 后 Checker 永久挂起：inflight 未清理、done 未 close")
	}
}

// TestChecker_RefreshPanic_WaiterGetsError 刷新进行中时来的等待者应拿到
// 错误（而不是跟着挂住），且错误可判定。
func TestChecker_RefreshPanic_WaiterGetsError(t *testing.T) {
	release := make(chan struct{})
	c := NewChecker(func(ctx context.Context) error {
		<-release
		panic("refresh boom")
	}, time.Millisecond, time.Millisecond)

	leaderDone := make(chan struct{})
	go func() {
		defer close(leaderDone)
		defer func() { _ = recover() }()
		_ = c.Check(context.Background())
	}()

	time.Sleep(50 * time.Millisecond) // 让 leader 卡在 refresh 里

	waiterDone := make(chan error, 1)
	go func() { waiterDone <- c.Check(context.Background()) }()

	time.Sleep(50 * time.Millisecond)
	close(release) // 放行 leader，让它 panic

	select {
	case err := <-waiterDone:
		if err == nil {
			t.Fatal("等待者应拿到刷新失败的错误，而不是 nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("等待者永久挂起")
	}
	<-leaderDone
}

// TestChecker_RefreshPanic_ThenSuccess panic 之后 Checker 仍能正常刷新
// （不会被此前的失败状态永久钉住）。
//
// panic 被转成 error 后按 failTTL 缓存（与普通刷新失败同口径）——失败窗口
// 过后应能重新刷新成功。
func TestChecker_RefreshPanic_ThenSuccess(t *testing.T) {
	var shouldPanic = true
	c := NewChecker(func(ctx context.Context) error {
		if shouldPanic {
			shouldPanic = false
			panic("boom")
		}
		return nil
	}, 10*time.Millisecond, 10*time.Millisecond)

	func() {
		defer func() { _ = recover() }()
		_ = c.Check(context.Background())
	}()

	// 失败窗口内：返回缓存的失败（与普通刷新失败同口径）。
	if err := c.Check(context.Background()); err == nil {
		t.Fatal("失败窗口内应返回缓存的失败")
	}

	// 失败窗口过后：重新刷新成功。
	time.Sleep(30 * time.Millisecond)
	if err := c.Check(context.Background()); err != nil {
		t.Fatalf("失败窗口过后应能重新刷新成功，实得 %v", err)
	}
}

// TestSingleFlight_FlightKeyRemovedBeforeWake 回归测试：单飞结束后新到的
// 调用必须**重新执行** fn，而不是拿到上一次的结果。
//
// 缺陷成因：清理顺序是「先 close(ready) 唤醒、后 Delete(key) 摘键」⇒ 两者
// 之间存在一个窗口，此刻新到的调用 LoadOrStore 命中的是**已结束**的那次
// flight ⇒ 直接拿到上一次的陈旧结果、根本不执行 fn。golang.org/x/sync 的
// singleflight 是先摘键再唤醒。
func TestSingleFlight_FlightKeyRemovedBeforeWake(t *testing.T) {
	flight := singleflight.New[string]()
	var calls int
	fn := func(ctx context.Context) (string, error) {
		calls++
		return "v", nil
	}

	if _, err := flight.Do(context.Background(), "k", fn); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if calls != 1 {
		t.Fatalf("首次调用应执行 1 次 fn，实得 %d", calls)
	}

	// 第二次调用：flight 已结束 ⇒ 必须重新执行 fn。
	if _, err := flight.Do(context.Background(), "k", fn); err != nil {
		t.Fatalf("Do second: %v", err)
	}
	if calls != 2 {
		t.Fatalf("flight 结束后新调用应重新执行 fn（实得 %d 次）——拿到了上一次的陈旧结果", calls)
	}
}

// TestSingleFlight_PanicErrorKeepsIdentity 回归测试：fn panic 转成的 error
// 必须保留原始错误的身份（errors.Is 可判定）。
//
// 缺陷成因：用 %v 拼接 panic 值，只留文本、丢掉身份 ⇒ 等待者无法 errors.Is
// 到原始错误。
func TestSingleFlight_PanicErrorKeepsIdentity(t *testing.T) {
	flight := singleflight.New[string]()
	sentinel := errors.New("boom")
	started := make(chan struct{})

	// leader：先注册 flight，再 panic（panic 原样抛给它自己）。
	go func() {
		defer func() { _ = recover() }()
		_, _ = flight.Do(context.Background(), "k", func(ctx context.Context) (string, error) {
			close(started)
			time.Sleep(30 * time.Millisecond)
			panic(sentinel)
		})
	}()

	<-started
	// waiter：拿到的是被 panic 转成的 error。
	_, err := flight.Do(context.Background(), "k", func(ctx context.Context) (string, error) {
		return "should not run", nil
	})
	if err == nil {
		t.Fatal("等待者应拿到错误")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("等待者拿到的错误应保留原始错误身份（errors.Is 可判定），实得 %v", err)
	}
}
