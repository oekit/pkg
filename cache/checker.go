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

	"github.com/oekit/pkg/errors"
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

// CheckerOption Checker 可选项（pkg#355：禁裸 Option——多包 co-import 冲突）。
type CheckerOption func(*Checker)

// WithClock 注入时钟（测试用；缺省 time.Now）。nil 按仓规总则②显式回退默认值，
// 不把 nil 存进实例——nil 一路存到 Check 的 c.now() 调用点是构造期之外的
// 远端 SIGSEGV（pkg#356，与同包 LocalConfig.Now「nil 视为缺省」口径对齐）。
func WithClock(now func() time.Time) CheckerOption {
	return func(c *Checker) {
		if now == nil {
			return
		}
		c.now = now
	}
}

// NewChecker 构造；failTTL<=0 时与 ttl 同值。refresh 是必需回调、无默认可回退，
// nil 属构造期非法值，按仓规总则① fail-fast（pkg#356）——推迟到 Check 的调用点
// 才炸（且炸在 defer recover 注册之前）违反「构造期发现」纪律。
func NewChecker(refresh func(ctx context.Context) error, ttl, failTTL time.Duration, opts ...CheckerOption) *Checker {
	if refresh == nil {
		panic("cache: NewChecker: refresh is nil")
	}
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
//
// 刷新语义（pkg#352）：刷新是一次**共享的**缓存回填，执行在
// context.WithoutCancel(ctx) 上——单个观察者断开/超时不中止刷新本身，也
// 不把它的取消缓存成失败结果传染所有等待者。ctx 携带的取值（鉴权等）原样
// 保留，只剥离取消信号。已取消的调用方在入口直接返回，不触发共享刷新。
// 刷新自身返回 context.Canceled（回调内部取消）视为瞬态：如实分发给本次
// 全体调用方，但不写入缓存（不发 failTTL），下次 Check 立即重试。
func (c *Checker) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
			// panic 收编单源（#381，errors.Recovered）：error 身份保留
			// （waiter 可 errors.Is/As 到 refresh 回调 panic 出去的原始错误），
			// goroutine 栈与 panic 值类型信息由 PanicError 携带——此前 %v
			// 把身份拍平成文本。
			perr := fmt.Errorf("cache: refresh panic: %w", errors.Recovered(r))
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

	// pkg#352：刷新跑在 WithoutCancel 上——leader 断开不再把共享刷新整个
	// 拖死；ctx 取值保留，仅剥离取消/超时信号。
	err := c.refresh(context.WithoutCancel(ctx))

	c.mu.Lock()
	c.cachedErr = err
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// 瞬态取消不缓存：不坐 failTTL，下次 Check 立即重试
			// （DeadlineExceeded 是真实失败——超时多数是慢依赖，照常短缓存）。
			c.validUntil = time.Time{}
		} else {
			c.validUntil = c.now().Add(c.failTTL)
		}
	} else {
		c.validUntil = c.now().Add(c.ttl)
	}
	c.inflight = nil
	c.mu.Unlock()
	call.err = err
	close(call.done)
	return err
}
