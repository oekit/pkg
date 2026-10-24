package cache

import (
	"container/list"
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// cacheLog 惰性 slog logger：pkg 保持零 oekit 内部依赖，不用 logx 门面
// （消费方服务可通过 slog.SetDefault 汇流）。
var cacheLog = slog.Default().With("component", "cache")

// goSafe pkg 内联的安全 goroutine：panic 被 recover 记日志，不带上抛。
// pkg 是最底层模块（零内部依赖），不能引 concurrency/async，故自带最小实现。
func goSafe(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				cacheLog.Error("panic recovered", "panic", r, "stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
}

// defaultNullTTL 是 NullTTL 未设置（0）时的空值缓存默认 TTL。
// redis 子包的 RedisCache 与 LocalCache 保持一致的空值缓存默认语义。
const defaultNullTTL = 1 * time.Minute

// localEntry 本地缓存条目。
// isNull=true 表示 null marker（数据不存在标记，防穿透），value 恒为 nil。
type localEntry[T any] struct {
	value     *T
	isNull    bool
	expiresAt time.Time
}

// isExpired 检查是否过期（now 由调用方传入——注入时钟的唯一判定口径）。
func (e *localEntry[T]) isExpired(now time.Time) bool {
	return !e.expiresAt.IsZero() && now.After(e.expiresAt)
}

// EvictPolicy 淘汰策略（LocalConfig.EvictPolicy，pkg#25）。
type EvictPolicy int

const (
	// EvictFIFO 写序淘汰（缺省）：FIFO + 重写提升，读命中不提升。
	// 达到 MaxSize 后淘汰「最早写入且未被重写刷新」的条目。
	EvictFIFO EvictPolicy = iota

	// EvictLRU 读序淘汰：读命中同样提升顺序（最近最少使用的条目先淘汰）。
	// 锁代价：读命中需在读锁之外取写锁完成顺序提升（见 promoteOnRead 注释）。
	EvictLRU
)

// LocalConfig 配置本地缓存实例。
type LocalConfig[T any] struct {
	// TTL 缓存过期时间。0 表示不过期。
	TTL time.Duration

	// MaxSize 最大缓存条目数。0 表示不限制。
	MaxSize int

	// Source 缓存未命中时的数据获取函数（Cache-Aside 模式）。
	Source SourceFunc[T]

	// NullTTL 空值缓存过期时间（防穿透）。
	//   - 0（未设置）：使用默认值 1 分钟；
	//   - 负值：显式禁用空值缓存（Source 返回 nil 时不写 null marker，
	//     下次 GetOrLoad 会重新调用 Source）；
	//   - 正值：按指定时长缓存 null marker。
	NullTTL time.Duration

	// Now 时钟注入（缺省 time.Now；nil 视为缺省）。TTL/NullTTL 的全部过期判定
	// 与过期时刻写入走该时钟——测试注入假时钟即可确定性驱动过期/空值窗口，
	// 无 sleep 无 flake（与 memkv 的 s.now、budget 的 WithClock 口径拉齐，#9）。
	Now func() time.Time

	// SourceWithTTL 动态回填 TTL 的回源函数（pkg#11；与 Source 同时配置时本字段
	// 优先）：ttl > 0 按其缓存，<=0 回退 TTL 字段；返回 nil 值仍走 null marker
	// （此时 ttl 不生效，NullTTL 治理）。典型场景：第三方 token 的 expires_in。
	SourceWithTTL SourceFuncWithTTL[T]

	// StaleIfError 回源失败时回退已过期的历史真值（fail-open，pkg#11——防上游
	// 抖动放大为拒绝服务）。只认「TTL 过期的真值」：null marker 不参与 stale，
	// TTL=0 永不过期条目无 stale 语义（不会过期）。过期条目在本路径下保留于表内
	// （回收交给后台清扫），可反复兜底直至上游恢复。
	StaleIfError bool

	// OnStale stale 命中回调（观测：打点/告警；nil = no-op）。
	// value 所有权与缓存值一致（共享引用、调用方只读）。
	OnStale func(ctx context.Context, key string, stale *T)

	// NoBackgroundSweep 关闭后台清扫 goroutine（pkg#25）：置 true 时
	// NewLocalCache 之后进程内不新增任何 goroutine——短命命令行/宿主内嵌
	// 场景不该为缓存常驻一个清扫协程。此时过期条目由惰性删除（Get/Exists
	// 读路径）与调用方显式调用 Sweep 回收。Close 仍安全且幂等。
	NoBackgroundSweep bool

	// CloneOnRead 读命中返回副本（pkg#25；nil = 保持零拷贝）。
	// 开启后 Get/GetOrLoad（命中与 stale 路径）/MGet 返回的是副本，调用方
	// 改写返回值不会污染缓存里的那份。克隆函数返回 nil 时视为不克隆
	// （返回原值）。回源路径（GetOrLoad 未命中时从 Source 取到的值）不克隆
	// ——那是调用方自己的值。OnStale 回调同样不克隆（回调值所有权与缓存值一致）。
	CloneOnRead func(*T) *T

	// EvictPolicy 淘汰策略（pkg#25；缺省 EvictFIFO）。
	//   - EvictFIFO：写序淘汰（FIFO + 重写提升，读命中不提升）——历史行为；
	//   - EvictLRU：读命中也提升顺序（最近最少使用先淘汰）。
	EvictPolicy EvictPolicy
}

// LocalCache 是基于内存的本地缓存实现。
// 适用于单实例场景或作为二级缓存。
//
// 淘汰策略由 LocalConfig.EvictPolicy 决定：
//   - EvictFIFO（缺省）：写序淘汰（FIFO + 重写提升，读命中不提升——非严格 LRU）：
//     达到 MaxSize 后淘汰最早写入（且未被重写刷新）的条目；
//   - EvictLRU：读命中也提升顺序，最近最少使用的条目先淘汰。
//
// 后台清理：TTL>0 时默认起一个后台清扫 goroutine，NoBackgroundSweep 置 true
// 可关闭它（进程内不新增任何 goroutine，过期条目由惰性删除与调用方显式
// Sweep 回收）。使用 Close(ctx) 停止后台清理 goroutine；Close 可安全重复
// 调用，签名对齐仓库硬规则 Close(ctx context.Context) error。
type LocalCache[T any] struct {
	mu        sync.RWMutex
	data      map[string]*localEntry[T]
	cfg       LocalConfig[T]
	now       func() time.Time         // 规范化后的时钟（cfg.Now nil → time.Now）
	order     *list.List               // 顺序队列：front 最旧（先淘汰），back 最新
	orderIdx  map[string]*list.Element // key → order 元素（O(1) 定位）
	done      chan struct{}
	closeOnce sync.Once

	// 观测计数（pkg#76）：读命中/读未命中、容量淘汰、TTL 过期回收。
	// 原子计数——读路径无锁递增，不给热路径加锁竞争。
	hits        atomic.Int64
	misses      atomic.Int64
	evictions   atomic.Int64
	expirations atomic.Int64
}

// NewLocalCache 创建一个本地缓存实例。
func NewLocalCache[T any](cfg LocalConfig[T]) *LocalCache[T] {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	c := &LocalCache[T]{
		data:     make(map[string]*localEntry[T]),
		cfg:      cfg,
		now:      now,
		order:    list.New(),
		orderIdx: make(map[string]*list.Element),
		done:     make(chan struct{}),
	}

	// 启动过期清理（NoBackgroundSweep 时不起后台件——pkg#25：
	// 短命 CLI/宿主内嵌场景不该为缓存常驻清扫协程）
	if cfg.TTL > 0 && !cfg.NoBackgroundSweep {
		goSafe(func() { c.cleanup() })
	}

	return c
}

// Close 停止后台清理 goroutine（TTL>0 且未开 NoBackgroundSweep 时构造启动的那个）。
// sync.Once 保证幂等：重复 Close 不 panic、不返回错误。
// 无后台件时（NoBackgroundSweep=true）Close 同样安全且幂等。
//
// ctx 预留：当前实现同步返回 nil，ctx 不阻塞——本实现无外部资源需要
// 排空，仅关闭内部 done channel 通知 cleanup goroutine 退出。
func (c *LocalCache[T]) Close(ctx context.Context) error {
	c.closeOnce.Do(func() {
		close(c.done)
	})
	return nil
}

// Get 获取缓存。
// 返回 (nil, nil) 表示数据不存在 —— 包括 key 不存在和命中 null marker
// 两种情况；GetOrLoad 会区分二者实现防穿透。
//
// ⚠️ 返回的是缓存内部的 *T（零拷贝，热路径设计）：调用方不得修改指向的
// 值——会污染缓存并与并发读形成 data race。需要可变副本时自行拷贝，
// 或配置 LocalConfig.CloneOnRead 让读命中返回副本（pkg#25）。
// （RedisCache 每次反序列化返回新副本，两实现语义差异见各自身文档。）
func (c *LocalCache[T]) Get(ctx context.Context, key string) (*T, error) {
	entry, ok := c.getEntry(key)
	if !ok || entry.isNull {
		c.misses.Add(1)
		return nil, nil
	}
	c.hits.Add(1)
	return c.cloneOnRead(entry.value), nil
}

// GetEx 获取缓存并区分「不存在」与「有值但值为 nil」（pkg#25）。
//
// 返回 (value, hit, err)：
//   - hit=true：命中真值条目——value 可能为 nil（调用方显式存了 nil 值）；
//   - hit=false：key 不存在、已过期，或命中 null marker（null marker 表示
//     「数据不存在」，按未命中处理——与 Get 返回 (nil, nil) 同口径）。
//
// 既有 Get 签名不变（下游在用），需要区分三种「拿不到值」的情形时用本方法。
func (c *LocalCache[T]) GetEx(ctx context.Context, key string) (*T, bool, error) {
	entry, ok := c.getEntry(key)
	if !ok || entry.isNull {
		c.misses.Add(1)
		return nil, false, nil
	}
	c.hits.Add(1)
	return c.cloneOnRead(entry.value), true, nil
}

// Peek 非删除式读取（pkg#72）：读到过期条目时**不删除**它，只是不命中。
//
// 与 Get 的差别：Get 走 getEntry 的双检路径，命中过期条目时会惰性删除
// （回收过期条目）；Peek 只看不删——过期真值留在表内，stale-if-error 兜底
// （回源失败时回退历史真值）才有值可退。装饰器的前置检查必须走本方法：
// 走 Get 会提前删掉 stale 兜底需要的那份值，使 StaleIfError 形同虚设。
//
// 命中语义与 GetEx 一致：
//   - hit=true：命中真值条目——value 可能为 nil（调用方显式存了 nil 值）；
//   - hit=false：key 不存在、已过期，或命中 null marker。
//
// 命中真值条目时行为与 Get 一致（LRU 顺序提升 + CloneOnRead 返回副本）。
func (c *LocalCache[T]) Peek(ctx context.Context, key string) (*T, bool, error) {
	now := c.now()
	c.mu.RLock()
	entry, ok := c.data[key]
	c.mu.RUnlock()

	if !ok || entry.isNull || entry.isExpired(now) {
		// 过期条目保留在表内（不删除）——stale 兜底要用。
		c.misses.Add(1)
		return nil, false, nil
	}

	// LRU：读命中提升顺序（先释放读锁，再取写锁）
	c.promoteOne(key, entry)
	c.hits.Add(1)
	return c.cloneOnRead(entry.value), true, nil
}

// cloneOnRead 按 CloneOnRead 配置克隆读命中的值（pkg#25）。
//   - 未配置 CloneOnRead：原样返回（零拷贝，保持历史行为）；
//   - 克隆函数返回 nil：视为不克隆，返回原值。
//
// 克隆在锁外调用（用户代码不进临界区）。
func (c *LocalCache[T]) cloneOnRead(v *T) *T {
	if c.cfg.CloneOnRead == nil || v == nil {
		return v
	}
	if cloned := c.cfg.CloneOnRead(v); cloned != nil {
		return cloned
	}
	return v
}

// readHit 记录一次读命中（key + 命中的 entry 指针）。
type readHit[T any] struct {
	key   string
	entry *localEntry[T]
}

// promoteOnRead 读命中提升顺序（仅 EvictLRU；FIFO 下直接返回）。
//
// 锁纪律：读路径**不能**持有读锁再取写锁（Go 的 RWMutex 在写者排队时会
// 死锁同 goroutine 的读锁升级），因此调用方必须先释放读锁再调用本方法。
// 提升前二次检查条目仍是同一个（期间可能被并发删除/替换）。
func (c *LocalCache[T]) promoteOnRead(hits []readHit[T]) {
	if c.cfg.EvictPolicy != EvictLRU || len(hits) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, h := range hits {
		if cur, ok := c.data[h.key]; !ok || cur != h.entry {
			continue
		}
		c.moveToBack(h.key)
	}
}

// promoteOne 单键读命中提升顺序（仅 EvictLRU）。
func (c *LocalCache[T]) promoteOne(key string, entry *localEntry[T]) {
	if c.cfg.EvictPolicy != EvictLRU {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cur, ok := c.data[key]; !ok || cur != entry {
		return
	}
	c.moveToBack(key)
}

// getEntry 内部方法：返回有效 entry 及其是否存在（含 null marker）。
// 过期条目会被顺带删除（双检模式，避免误删并发 Set 的新值）。
func (c *LocalCache[T]) getEntry(key string) (*localEntry[T], bool) {
	c.mu.RLock()
	entry, ok := c.data[key]
	c.mu.RUnlock()

	if !ok {
		return nil, false
	}

	if entry.isExpired(c.now()) {
		c.mu.Lock()
		// 二次检查：在 RUnlock 和 Lock 之间可能有别的 goroutine Set 了新值，
		// 只删除仍然是同一个过期 entry 的情况，避免误删新值。
		if cur, exists := c.data[key]; exists && cur == entry {
			delete(c.data, key)
			c.removeFromOrder(key)
			c.expirations.Add(1)
		}
		c.mu.Unlock()
		return nil, false
	}

	// LRU：读命中提升顺序（先释放读锁，再取写锁）
	c.promoteOne(key, entry)
	return entry, true
}

// GetOrLoad 获取缓存，未命中时通过 Source/SourceWithTTL 加载（Cache-Aside 模式）。
// Source 返回 (nil, nil) 时缓存 null marker（默认 1 分钟，可用 NullTTL
// 调整，负值禁用）；命中 null marker 时直接返回 (nil, nil)，**不会**再调
// Source，避免对不存在的 key 形成 thundering-herd 击穿。
//
// 动态回填 TTL（SourceWithTTL，pkg#11）：回源返回的 ttl > 0 按其缓存，
// <=0 回退配置的 TTL；两者同时配置时 SourceWithTTL 优先。
//
// stale-if-error（StaleIfError，pkg#11）：回源失败且存在已过期的历史真值时
// 返回陈旧值并触发 OnStale 回调（fail-open，语义详见 LocalConfig 字段注释）。
//
// 读路径行为（pkg#25）：
//   - 命中路径与 stale 路径都算「读命中」——EvictLRU 下提升顺序，
//     CloneOnRead 开启时返回副本；
//   - 回源路径（未命中时从 Source 取到的值）不算读命中——那是调用方
//     自己的值，不克隆。
//
// 按调用注入回源见 GetOrLoadWithSource / GetOrLoadWithSourceTTL（pkg#77）。
func (c *LocalCache[T]) GetOrLoad(ctx context.Context, key string) (*T, error) {
	return c.getOrLoad(ctx, key, loadSpec[T]{})
}

// GetOrLoadWithSource 按调用注入的 source 回源（pkg#77）。
//
// 与 GetOrLoad 的差别只有一处：回源函数由**调用方在调用点**给出，而不是
// 构造期固定。面向「回源依赖 key / 依赖调用上下文」的形态（token / 凭据 /
// 租户级缓存）——同一 key 的不同调用方可以给出各自的回源函数，不必在缓存
// 外再包一层 key→source 注册表。
//
// source 为 nil 时退回构造期的 Source/SourceWithTTL（与 GetOrLoad 同口径）。
// 回填 TTL 取构造期默认 TTL（需要按调用指定 TTL 见 GetOrLoadWithSourceTTL）。
//
// 命中路径与 GetOrLoad 完全一致（不注入回源函数时不会被调用）。
func (c *LocalCache[T]) GetOrLoadWithSource(ctx context.Context, key string, source SourceFunc[T]) (*T, error) {
	return c.getOrLoad(ctx, key, loadSpec[T]{source: source})
}

// GetOrLoadWithSourceTTL 按调用注入的 source 回源并按 ttl 回填（pkg#77）。
//
// 与 GetOrLoadWithSource 的差别：回源函数可携带动态 TTL（SourceFuncWithTTL），
// 且 ttl > 0 时**覆盖**回源给出的 TTL（ttl <= 0 时按回源给出的 TTL，
// 仍无则退回构造期默认 TTL）。
//
// source 为 nil 时退回构造期的 Source/SourceWithTTL；此时 ttl > 0 仍生效
// （覆盖构造期回源给出的 TTL）。
//
// 典型场景：第三方 token 的 expires_in 随租户而异——回源函数按 key 取凭据、
// 动态 TTL 与调用点注入的凭据来源一并给出。
func (c *LocalCache[T]) GetOrLoadWithSourceTTL(ctx context.Context, key string, source SourceFuncWithTTL[T], ttl time.Duration) (*T, error) {
	return c.getOrLoad(ctx, key, loadSpec[T]{sourceTTL: source, ttl: ttl})
}

// loadSpec 描述一次回源用哪个函数、按什么 TTL 回填（pkg#77）。
// 全零值 = 构造期口径（SourceWithTTL 优先于 Source）。
type loadSpec[T any] struct {
	source    SourceFunc[T]        // 按调用注入的回源函数（nil = 退回构造期）
	sourceTTL SourceFuncWithTTL[T] // 按调用注入的动态 TTL 回源函数（nil = 退回构造期）
	ttl       time.Duration        // 回填 TTL 覆盖（<= 0 = 按回源给出的 / 构造期默认）
}

// getOrLoad 是 GetOrLoad / GetOrLoadWithSource / GetOrLoadWithSourceTTL 的
// 共同实现（pkg#77）：回源函数与回填 TTL 由 spec 指定，spec 为零值时
// 行为与历史 GetOrLoad 完全一致。
func (c *LocalCache[T]) getOrLoad(ctx context.Context, key string, spec loadSpec[T]) (*T, error) {
	return c.load(ctx, key, spec, true)
}

// getOrLoadNoMiss 与 getOrLoad 同流程，但**不计入未命中计数**（pkg#90）。
//
// 用途：装饰器（SingleFlightCache）的前置探测已计过同一次未命中，加载路径
// 再计一次会把一次未命中计成两次——命中率指标失真（实测 90% 真实命中率被
// 报成 81.8%）。本方法让「前置探测计一次、加载路径不再计」成立，一次读
// 恰好计入命中或未命中之一。
//
// **命中计数不受影响**：stale 兜底等读命中仍照常计入（与内层直连口径一致——
// 内层直连的 stale 兜底路径同样是「一次未命中 + 一次读命中」）。
//
// 只供包内装饰器使用（能力随实现走，不进公共 API）：内层实现方不认这个
// 能力时，装饰器退回内层原有口径（能力缺失是内层的事实）。
func (c *LocalCache[T]) getOrLoadNoMiss(ctx context.Context, key string, spec loadSpec[T]) (*T, error) {
	return c.load(ctx, key, spec, false)
}

// load 是 getOrLoad 的实现：countMiss=false 时不计入未命中计数（pkg#90，
// 见 getOrLoadNoMiss），其余行为与计数口径完全一致。
func (c *LocalCache[T]) load(ctx context.Context, key string, spec loadSpec[T], countMiss bool) (*T, error) {
	// 1. 非删除式查看：未过期直接返回（null marker 命中 → (nil, nil) 防穿透）。
	//    过期条目保留在表内——StaleIfError 需要过期真值在回源失败时仍可读
	//    （惰性删除在此让位，回收交给后台清扫与其他读路径）
	now := c.now()
	c.mu.RLock()
	entry, exists := c.data[key]
	c.mu.RUnlock()

	stale := false
	if exists {
		if !entry.isExpired(now) {
			if entry.isNull {
				if countMiss {
					c.misses.Add(1)
				}
				return nil, nil
			}
			// 读命中：LRU 提升顺序 + 按 CloneOnRead 决定返回副本还是共享引用
			c.promoteOne(key, entry)
			c.hits.Add(1)
			return c.cloneOnRead(entry.value), nil
		}
		stale = !entry.isNull && !entry.expiresAt.IsZero()
	}
	if countMiss {
		c.misses.Add(1)
	}

	// 2. 回源（按调用注入优先于构造期；SourceWithTTL 优先于 Source）
	var (
		val *T
		ttl time.Duration
		err error
	)
	switch {
	case spec.sourceTTL != nil:
		val, ttl, err = spec.sourceTTL(ctx, key)
	case spec.source != nil:
		val, err = spec.source(ctx, key)
		ttl = c.cfg.TTL
	case c.cfg.SourceWithTTL != nil:
		val, ttl, err = c.cfg.SourceWithTTL(ctx, key)
	case c.cfg.Source != nil:
		val, err = c.cfg.Source(ctx, key)
		ttl = c.cfg.TTL
	default:
		return nil, nil
	}
	if err != nil {
		// stale-if-error：回源失败时回退已过期真值（fail-open）
		if c.cfg.StaleIfError && stale {
			if c.cfg.OnStale != nil {
				c.cfg.OnStale(ctx, key, entry.value)
			}
			// stale 兜底同样是读命中：提升顺序 + 按 CloneOnRead 决定返回值
			c.promoteOne(key, entry)
			c.hits.Add(1)
			return c.cloneOnRead(entry.value), nil
		}
		return nil, err
	}

	// 3. 回填缓存（best-effort，失败仅记录不影响主流程）
	if val == nil {
		// 缓存空值防穿透
		if nullTTL := c.effectiveNullTTL(); nullTTL > 0 {
			c.setNullMarker(key, nullTTL)
		}
		return nil, nil
	}

	if ttl <= 0 {
		ttl = c.cfg.TTL
	}
	// 按调用指定的 TTL 覆盖（pkg#77——ttl > 0 时生效）。
	if spec.ttl > 0 {
		ttl = spec.ttl
	}
	if e := c.SetWithTTL(ctx, key, val, ttl); e != nil {
		cacheLog.Warn("local cache backfill failed", "key", key, "err", e)
	}

	return val, nil
}

// effectiveNullTTL 返回生效的空值缓存 TTL：
// 0（未设置）→ 默认 1 分钟；负值 → 原样返回（<=0 表示禁用）。
func (c *LocalCache[T]) effectiveNullTTL() time.Duration {
	if c.cfg.NullTTL == 0 {
		return defaultNullTTL
	}
	return c.cfg.NullTTL
}

// setNullMarker 写入 null marker 条目（与 Set 相同的锁纪律与淘汰逻辑）。
func (c *LocalCache[T]) setNullMarker(key string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = c.now().Add(ttl)
	}

	// 检查是否需要淘汰（同 SetWithTTL）
	if c.cfg.MaxSize > 0 && len(c.data) >= c.cfg.MaxSize {
		if _, exists := c.data[key]; !exists {
			c.evictOldest()
		}
	}

	c.data[key] = &localEntry[T]{
		isNull:    true,
		expiresAt: expiresAt,
	}
	c.moveToBack(key)
}

// Set 设置缓存（使用默认 TTL）。
func (c *LocalCache[T]) Set(ctx context.Context, key string, value *T) error {
	return c.SetWithTTL(ctx, key, value, c.cfg.TTL)
}

// SetWithTTL 设置缓存（自定义 TTL）。
func (c *LocalCache[T]) SetWithTTL(ctx context.Context, key string, value *T, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = c.now().Add(ttl)
	}

	// 检查是否需要淘汰
	if c.cfg.MaxSize > 0 && len(c.data) >= c.cfg.MaxSize {
		if _, exists := c.data[key]; !exists {
			c.evictOldest()
		}
	}

	c.data[key] = &localEntry[T]{
		value:     value,
		expiresAt: expiresAt,
	}

	// 更新访问顺序
	c.moveToBack(key)
	return nil
}

// Del 删除缓存。
func (c *LocalCache[T]) Del(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.data, key)
	c.removeFromOrder(key)
	return nil
}

// DelBatch 批量删除缓存。
func (c *LocalCache[T]) DelBatch(ctx context.Context, keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, key := range keys {
		delete(c.data, key)
		c.removeFromOrder(key)
	}
	return nil
}

// MGet 批量获取：单次读锁区间取回一批键（契约见 BatchGetter——缺席语义
// 与 (nil, nil) 双关一致：不存在与 null marker 一律缺席；纯读不回填，
// 已过期条目按缺席处理，回收留给 Get/Exists 的双检路径——TTL=0 时无
// cleanup goroutine，过期条目仅在这些路径被惰性回收）。
//
// CloneOnRead 开启时按配置返回副本（pkg#25）；EvictLRU 策略下批量命中同样
// 提升顺序（读锁区间之外一次性提升，避免逐键取写锁）。
func (c *LocalCache[T]) MGet(ctx context.Context, keys ...string) (map[string]*T, error) {
	now := c.now()
	c.mu.RLock()
	items := make([]readHit[T], 0, len(keys))
	for _, key := range keys {
		entry, ok := c.data[key]
		if !ok || entry.isNull || entry.isExpired(now) {
			c.misses.Add(1)
			continue
		}
		items = append(items, readHit[T]{key: key, entry: entry})
	}
	c.mu.RUnlock()

	// LRU：批量读命中提升顺序（写锁一次）
	c.promoteOnRead(items)

	// 批量读命中逐键计数（口径与单键读一致：命中真值条目计命中）。
	c.hits.Add(int64(len(items)))

	out := make(map[string]*T, len(items))
	for _, it := range items {
		// 命中真值但值为 nil 的条目同样进 map（键在场，值为 nil）
		out[it.key] = c.cloneOnRead(it.entry.value)
	}
	return out, nil
}

// 编译期能力契约：LocalCache 具备批量读取能力。
var _ BatchGetter[int] = (*LocalCache[int])(nil)

// 编译期能力契约：LocalCache 具备非删除式读取能力（pkg#72）。
var _ PeekGetter[int] = (*LocalCache[int])(nil)

// Exists 检查缓存是否存在。
func (c *LocalCache[T]) Exists(ctx context.Context, key string) (bool, error) {
	c.mu.RLock()
	entry, ok := c.data[key]
	c.mu.RUnlock()

	if !ok {
		return false, nil
	}

	if entry.isExpired(c.now()) {
		c.mu.Lock()
		// 二次检查：同 Get，避免误删并发 Set 的新值。
		if cur, exists := c.data[key]; exists && cur == entry {
			delete(c.data, key)
			c.removeFromOrder(key)
			c.expirations.Add(1)
		}
		c.mu.Unlock()
		return false, nil
	}

	return true, nil
}

// Refresh 刷新缓存（删除后重新从 Source 加载）。
func (c *LocalCache[T]) Refresh(ctx context.Context, key string) (*T, error) {
	if e := c.Del(ctx, key); e != nil {
		cacheLog.Warn("local cache refresh del failed", "key", key, "err", e)
	}
	return c.GetOrLoad(ctx, key)
}

// Size 返回当前缓存条目数。
func (c *LocalCache[T]) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

// Stats 返回当前观测计数快照（pkg#76）。
//
// 口径见 cache.Stats：Hits/Misses 是读路径的命中与未命中计数，Evictions 是
// 容量淘汰次数，Expirations 是 TTL 过期回收次数，Size 是条目数。
// 各计数单调不减；快照为近似一致（并发下各字段可能来自略有差别的时刻）。
func (c *LocalCache[T]) Stats() Stats {
	c.mu.RLock()
	size := int64(len(c.data))
	c.mu.RUnlock()
	return Stats{
		Hits:        c.hits.Load(),
		Misses:      c.misses.Load(),
		Evictions:   c.evictions.Load(),
		Expirations: c.expirations.Load(),
		Size:        size,
	}
}

// 编译期能力契约：LocalCache 具备观测计数能力（pkg#76）。
var _ StatsProvider = (*LocalCache[int])(nil)

// Clear 清空缓存。
func (c *LocalCache[T]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]*localEntry[T])
	c.order.Init()
	c.orderIdx = make(map[string]*list.Element)
}

// Sweep 手动清扫过期条目，返回本次清除的条目数（pkg#25）。
//
// 供「不起后台清扫协程」模式（LocalConfig.NoBackgroundSweep）下由调用方
// 在合适时机回收过期条目——短命 CLI/宿主内嵌场景不必常驻清扫协程。
// 过期判定走注入时钟（LocalConfig.Now），与后台清扫同一口径。
//
// ctx 已取消时直接返回 0（不执行清扫）。可与后台清扫并存——两者各自
// 独立判定，重复清扫不会重复计数（已被回收的条目不在表内）。
func (c *LocalCache[T]) Sweep(ctx context.Context) int {
	if ctx != nil && ctx.Err() != nil {
		return 0
	}
	return c.sweepExpired(c.now())
}

// cleanup 定期清理过期条目。
// ticker 只定节奏，过期判定一律走注入时钟——假时钟下真实 ticker 的时间戳
// 不可比（直接比较会把全部条目误判为过期而清空）。
func (c *LocalCache[T]) cleanup() {
	// TTL/2 可能整除为 0（如 TTL=1ns），time.NewTicker(0) 会 panic
	// （同型问题 KeyedLimiter 已修，此处补齐）；下限 1s 保底。
	interval := c.cfg.TTL / 2
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.sweepExpired(c.now())
		}
	}
}

// sweepExpired 清扫过期条目，返回实际清除的条目数。
// 分两阶段：读锁收集过期 key，写锁批量删除，减少写锁持有时间。
func (c *LocalCache[T]) sweepExpired(now time.Time) int {
	c.mu.RLock()
	expired := make([]string, 0, 8)
	for key, entry := range c.data {
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			expired = append(expired, key)
		}
	}
	c.mu.RUnlock()

	if len(expired) == 0 {
		return 0
	}

	c.mu.Lock()
	removed := 0
	for _, key := range expired {
		// 二次检查：可能已被其他 goroutine 更新
		if entry, ok := c.data[key]; ok && entry.isExpired(now) {
			delete(c.data, key)
			c.removeFromOrder(key)
			removed++
		}
	}
	c.mu.Unlock()
	c.expirations.Add(int64(removed))
	return removed
}

// evictOldest 淘汰最旧的条目（调用者需持有锁）。
// FIFO 下队头是最早写入且未被重写刷新的条目；LRU 下队头是最久未被读到的条目。
func (c *LocalCache[T]) evictOldest() {
	elem := c.order.Front()
	if elem == nil {
		return
	}
	key, ok := elem.Value.(string)
	if !ok {
		c.order.Remove(elem)
		return
	}
	c.order.Remove(elem)
	delete(c.orderIdx, key)
	if _, existed := c.data[key]; existed {
		delete(c.data, key)
		// 容量淘汰计数（pkg#76）——与 TTL 过期回收（Expirations）分开计。
		c.evictions.Add(1)
	}
}

// moveToBack 把 key 移到顺序队列尾部（最新，调用者需持有锁）。
// FIFO 策略下仅写入路径调用（重写提升）；LRU 策略下读命中也会调用。
func (c *LocalCache[T]) moveToBack(key string) {
	if elem, ok := c.orderIdx[key]; ok {
		c.order.MoveToBack(elem)
		return
	}
	c.orderIdx[key] = c.order.PushBack(key)
}

// removeFromOrder 从顺序队列中移除 key（O(1)，调用者需持有锁）。
func (c *LocalCache[T]) removeFromOrder(key string) {
	elem, ok := c.orderIdx[key]
	if !ok {
		return
	}
	c.order.Remove(elem)
	delete(c.orderIdx, key)
}
