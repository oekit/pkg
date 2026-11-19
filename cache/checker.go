// Package cache 进程内检查缓存原语（stdlib-only）：TTL 缓存 + 刷新进行中
// 并发合并（inflight singleflight）+ 失败短 TTL。收编自消费方健康检查器的
// 双份手写形态——/health 轮询会放大昂贵的 refresh（子进程 fan-out/远程探测），
// 无合并则 N 个观察者并发拉起 N 轮全量刷新。
package cache

import (
	"context"
	"sync"
	"time"

	"github.com/oekit/pkg/contextx"
	"github.com/oekit/pkg/errors"
	"github.com/oekit/pkg/internal/singleflight"
)

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
	// flight 共享刷新的并发合并（pkg#537，固定键）：与 SingleFlightCache
	// 同一套 internal/singleflight 实现——inflight 登记、done 广播唤醒、
	// 「先摘键后唤醒」「panic 转 error 给等待者再原样抛给首个调用方」
	// 全部单源，本包不再手写第二份。
	flight singleflight.Flight[error]
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
//
// 并发合并与 panic 面由 internal/singleflight.Flight 单源提供（pkg#537，
// 收敛此前本文件手写的 ~75 行 inflight call + done channel + panic 收编
// 样板——与 SingleFlightCache 同一套实现、同一套契约）：合并的等待者被
// ctx 取消时拿到 ctx.Err()；refresh 的 panic 先转成 error 按 failTTL
// 缓存，再原样抛给首个调用方（等待者拿到 panic 转成的 error，不跟着崩）。
//
// ctx 传 nil 视同 context.Background()（#554，与「nil ctx 兜底」总则一致）。
func (c *Checker) Check(ctx context.Context) error {
	// nil ctx 兜底单源（#529 收割 wave 漏网补齐，#554）：入口的 ctx.Err()
	// 在进 flight **之前**，flight 内部的 OrBackground 兜底救不到这里，
	// 必须先归一再做取消检查。
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	// 快路径：缓存有效直接返回，不进 flight（不为每次读分配 call/channel）。
	c.mu.Lock()
	if c.now().Before(c.validUntil) {
		err := c.cachedErr
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()

	err, doErr := c.flight.Do(ctx, checkerFlightKey, c.sharedRefresh)
	if doErr != nil {
		// 结果在 doErr 而不在结果值里：等待者被 ctx 取消（拿到 ctx.Err()），
		// 或 fn panic 被 Flight 转成 error 交给等待者——两种形态下
		// Flight[error] 的结果值都是零值 nil，不可用。
		return doErr
	}
	return err
}

// checkerFlightKey 共享刷新的固定 flight 键（pkg#537）：每个 Checker 实例
// 一把 flight，合并的单位是「本次 Check 的共享刷新」，键本身无信息量。
const checkerFlightKey = "cache.Checker.refresh"

// sharedRefresh 是共享刷新的执行体（singleflight leader 才运行）。
// 返回 (刷新结果, nil)——Flight[error] 的结果值承载刷新 error，第二个
// error 恒 nil（等待者的取消/panic 转型错误由 Flight 自己经第二返回值
// 上报，见 Check）。
func (c *Checker) sharedRefresh(ctx context.Context) (error, error) {
	// 双检缓存：拿到 leader 身份后再查一次——调用方在快路径 miss 与获得
	// leader 权之间，上一次刷新可能恰好完成并入缓存，直接复用不再多刷一轮
	// （对齐旧实现「缓存检查与 inflight 登记在同一把 mu 下原子完成」的
	// 合并保证：快路径 miss 不构成多刷一轮的理由）。
	c.mu.Lock()
	if c.now().Before(c.validUntil) {
		err := c.cachedErr
		c.mu.Unlock()
		return err, nil
	}
	c.mu.Unlock()
	return c.refreshAndCache(ctx), nil
}

// refreshAndCache 跑一次刷新并按结果口径缓存（leader 专属路径）。
//
// 缓存写入与 panic 面的清理都必须覆盖刷新的**所有**退场路径（pkg#352 前
// 的教训：清理写在 refresh 之后，refresh 一旦 panic 就不执行，inflight
// 永挂、等待者永久挂起）。panic 处理与 internal/singleflight 分工（pkg#537）：
// 这里负责 leader 侧的**缓存面**——先把 panic 转成 error 按 failTTL 缓存
// （后续调用方在失败窗口内拿到同一错误，与普通刷新失败同口径），再原样
// re-panic；Flight 的 defer 负责**等待者面**——把 panic 转成 error 唤醒
// 全体等待者（panic 收编单源 #381，errors.Recovered 保留 error 身份，
// 栈与类型信息由 PanicError 携带），并把 panic 原样抛给首个调用方
// （保留原始调用栈）。
func (c *Checker) refreshAndCache(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			perr := errors.Wrapf(errors.Recovered(r), "cache: refresh panic")
			c.mu.Lock()
			c.cachedErr = perr
			c.validUntil = c.now().Add(c.failTTL)
			c.mu.Unlock()
			panic(r)
		}
	}()

	// pkg#352：刷新跑在 WithoutCancel 上——leader 断开不再把共享刷新整个
	// 拖死；ctx 取值保留，仅剥离取消/超时信号。
	err = c.refresh(context.WithoutCancel(ctx))

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
	c.mu.Unlock()
	return err
}
