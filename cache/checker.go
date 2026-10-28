// Package cache 进程内检查缓存原语（stdlib-only）：TTL 缓存 + 刷新进行中
// 并发合并（inflight singleflight）+ 失败短 TTL。收编自消费方健康检查器的
// 双份手写形态——/health 轮询会放大昂贵的 refresh（子进程 fan-out/远程探测），
// 无合并则 N 个观察者并发拉起 N 轮全量刷新。
package cache

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// refreshCall 一次进行中的刷新：err 先写后 close(done)——等待者经 <-done 的
// happens-before 安全读取；close 是广播，任意多个等待者都能拿到同一结果
// （channel 直发只能唤醒一个，第二个等待者会永久挂起）。
type refreshCall struct {
	done chan struct{}
	err  error
}

// Checker 缓存一次错误型检查的结果：成功缓存 ttl，失败缓存 failTTL（失败多为
// 瞬时，恢复后不该被旧失败多压一整个正常周期）；刷新进行中后来者合并等待同一
// 次刷新（ctx 感知，不叠加刷新轮次）。并发安全。
type Checker struct {
	refresh func(ctx context.Context) error
	ttl     time.Duration
	failTTL time.Duration
	now     func() time.Time

	mu         sync.Mutex
	validUntil time.Time
	cachedErr  error
	inflight   *refreshCall
}

// Option Checker 可选项。
type Option func(*Checker)

// WithClock 注入时钟（测试用；缺省 time.Now）。
func WithClock(now func() time.Time) Option {
	return func(c *Checker) { c.now = now }
}

// NewChecker 构造；failTTL<=0 时与 ttl 同值。
func NewChecker(refresh func(ctx context.Context) error, ttl, failTTL time.Duration, opts ...Option) *Checker {
	if failTTL <= 0 {
		failTTL = ttl
	}
	c := &Checker{
		refresh: refresh,
		ttl:     ttl,
		failTTL: failTTL,
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Check 返回缓存有效期内的结果；过期则触发一次刷新（并发调用合并为一次）。
func (c *Checker) Check(ctx context.Context) error {
	c.mu.Lock()
	now := c.now()
	if now.Before(c.validUntil) {
		err := c.cachedErr
		c.mu.Unlock()
		return err
	}
	if c.inflight != nil {
		// 刷新进行中：等待同一次刷新的结果，不叠加刷新轮次。
		call := c.inflight
		c.mu.Unlock()
		select {
		case <-call.done:
			return call.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	call := &refreshCall{done: make(chan struct{})}
	c.inflight = call
	c.mu.Unlock()

	// 清理必须在 defer 里，且必须覆盖 panic 路径。
	//
	// 此前清理写在 refresh 之后：refresh 一旦 panic，`c.inflight = nil` 与
	// `close(call.done)` 都不会执行 ⇒ inflight 永远挂着、done 永远不关 ⇒
	// 后续每一次 Check 都卡在 `<-call.done` 上（context.Background() 下是
	// **永久挂起**，不是报错）——一次 refresh 的 panic 把 Checker 彻底废掉。
	//
	// panic 处理与 internal/singleflight 同型：先把 panic 转成 error 交给
	// 等待者（他们不该跟着崩，但需要知道「这次刷新失败了」），再把 panic
	// 原样抛给首个调用方（保留原始调用栈）。
	defer func() {
		if r := recover(); r != nil {
			perr := fmt.Errorf("cache: refresh panic: %v", r)
			c.mu.Lock()
			c.cachedErr = perr
			c.validUntil = c.now().Add(c.failTTL)
			c.inflight = nil
			c.mu.Unlock()
			call.err = perr
			close(call.done)
			panic(r)
		}
	}()

	err := c.refresh(ctx)

	c.mu.Lock()
	c.cachedErr = err
	if err != nil {
		c.validUntil = c.now().Add(c.failTTL)
	} else {
		c.validUntil = c.now().Add(c.ttl)
	}
	c.inflight = nil
	c.mu.Unlock()
	call.err = err
	close(call.done)
	return err
}
