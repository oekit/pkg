package singleflight

import (
	"context"
	"fmt"
	"sync"
)

// Flight 对同一个 key 的并发调用只执行一次 fn。
// 零值可用，无需初始化。
type Flight[T any] struct {
	calls sync.Map
}

// New 创建一个 Flight 实例。
func New[T any]() *Flight[T] {
	return &Flight[T]{}
}

type call[T any] struct {
	ready chan struct{}
	val   T
	err   error
}

// Do 执行 fn，同一个 key 的并发调用共享结果。
//
// 如果多个 goroutine 同时调用 Do 且 key 相同：
//   - 只有第一个 goroutine（caller）实际执行 fn
//   - 其他 goroutine（waiter）阻塞等待结果
//   - caller 完成后所有 waiter 同时获得返回值
//
// panic 处理（与 golang.org/x/sync/singleflight 语义一致）：
//   - 若 fn panic，panic 向首个 caller 自然传播（保留原始调用栈）
//   - waiter 被唤醒并拿到由 panic 转化的 error（而非零值+nil 的伪成功——
//     调用方无法区分"合法零值"与"执行失败"，会静默污染下游缓存/状态）
//   - map 中的 key 记录会被清理，不会因 panic 残留导致后续调用死锁
func (f *Flight[T]) Do(ctx context.Context, key string, fn func(ctx context.Context) (T, error)) (T, error) {
	c := &call[T]{
		ready: make(chan struct{}),
	}

	actual, loaded := f.calls.LoadOrStore(key, c)
	existing := actual.(*call[T])

	if loaded {
		// waiter：等待 caller 完成或 context 取消
		select {
		case <-existing.ready:
			return existing.val, existing.err
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		}
	}

	// caller（首个执行 fn 的 goroutine）
	// defer 保证：无论 fn 正常返回还是 panic，都关闭 ready 唤醒所有 waiter 并清理 map。
	// fn panic 时先把 panic 转成 error 存给 waiter，再 re-panic 传播给首个 caller。
	//
	// 顺序：**先摘键、后唤醒**。反过来（先 close 后 Delete）会留一个窗口：
	// 此刻新到的调用 LoadOrStore 命中的是**已结束**的那次 flight ⇒ 直接拿到
	// 上一次的结果、根本不执行 fn ⇒ 陈旧结果被当成新鲜结果返回。
	// golang.org/x/sync/singleflight 也是先摘键再唤醒。
	defer func() {
		if r := recover(); r != nil {
			// r 是 error 时用 %w 保留身份——waiter 才能 errors.Is/As 到原始错误；
			// %v 只留文本，身份丢了。
			var perr error
			if e, ok := r.(error); ok {
				perr = fmt.Errorf("singleflight: fn panic: %w", e)
			} else {
				perr = fmt.Errorf("singleflight: fn panic: %v", r)
			}
			c.err = perr
			f.calls.Delete(key)
			close(c.ready)
			panic(r)
		}
		f.calls.Delete(key)
		close(c.ready)
	}()

	c.val, c.err = fn(ctx)
	return c.val, c.err
}

// Forget 移除指定 key 的进行中调用记录。
// 之后对该 key 的 Do 调用会发起新的 fn 执行。
func (f *Flight[T]) Forget(key string) {
	f.calls.Delete(key)
}
