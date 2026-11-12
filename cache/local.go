package cache

import (
	"container/list"
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oekit/pkg/internal/logkv"
)

// cacheLogger 取当前默认 slog logger（带 component 标签）。
//
// **必须在调用时取**，不能是包级 var：包级 var 在 init 阶段求值，早于任何
// main() 里的 slog.SetDefault ⇒ 消费方设的默认 logger 永远汇不到缓存的
// 日志里（注释原先声称「消费方服务可通过 slog.SetDefault 汇流」，实测
// 不成立——缓存日志始终走进程启动时的默认 handler）。
//
// pkg 保持零 oekit 内部依赖，不用 logx 门面；消费方服务仍可通过
// slog.SetDefault 汇流。
func cacheLogger() *slog.Logger {
	return slog.Default().With("component", "cache")
}

// goSafe pkg 内联的安全 goroutine：panic 被 recover 记日志，不带上抛。
// pkg 是最底层模块（零内部依赖），不能引 concurrency/async，故自带最小实现。
func goSafe(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				cacheLogger().Error("panic recovered", "panic", r, "stack", string(debug.Stack()))
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
	// TTL=0 永不过期条目无 stale 语义（不会过期）。**过期真值条目在本路径下
	// 保留于表内**——既不被后台清扫回收，也不被读路径的惰性删除回收，
	// 可反复兜底直至上游恢复（上游恢复后同 key 被新值覆盖，不额外占空间）。
	//
	// 代价：开启后过期真值会留在表内直到被覆盖或被 Del/Clear 清掉
	// （与「缓存条目无上界」的既有口径一致；要上界用 MaxSize）。
	//
	// 选择性 stale（#172）：Source 在被调用期间可取消本次兜底——对本 key
	// 调 Del（兜底资格在回源之后重新捕获，Del 即取消），或返回 unwrap 命中
	// cache.ErrNoStale 的错误（原样回传）。此前 stale 标志先于 Source 捕获，
	// Source 内 Del 也取消不了兜底，消费方只能写毒标记绕行。
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
// 写后失效保障（#200）：写路径（Set/SetWithTTL/SetNX/Del/DelBatch 与回填）
// 推进 per-key 世代号；回源路径在 Source 返回后、回填前复查世代，期间该
// key 被 Del/覆盖写入则**放弃回填**——在途回源不会把失效前的旧值重新灌回
// 缓存击穿整个 TTL 窗口。
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

	// 世代号（#200）：genSeq 是全局单调序列（永不复用——Clear 后新条目的
	// 世代不与在途回源捕获过的旧值碰撞），gens 记录每个 key 的最新世代。
	// 写路径推进、回源路径捕获/复查：Source 执行期间世代被推进 ⇒ 回填放弃。
	//
	// 内存口径：gens 与 data 同键空间；Del 不即时回收 gens 条目（墓碑语义
	// ——首次加载期间被 Del 的 key 也需要可识别的世代推进）。高基数写失效
	// 场景（write-invalidate 模式每次写先 Del）下墓碑集与历史 key 总量线性，
	// 由清扫路径的墓碑压缩回收（compactGensLocked，#396）：无在途回源时把
	// 不在 data 里的 gens 条目整批删除——在途回源已捕获的世代 > 0 时，删除
	// 只会让复查按「世代已变」保守放弃回填（宁缺不错）；唯一危险形态是
	// 「捕获 0 → Del 造墓碑 → 压缩删墓碑 → 复查见 0 误判未失效」，由 loading
	// 在途计数闸住（捕获世代前登记，压缩在写锁内检查）。
	genSeq atomic.Uint64
	gens   map[string]uint64

	// loading 是在途回源计数（#396 墓碑压缩的安全闸）：捕获世代前 Add(1)、
	// 回填复查后 Add(-1)（load 的 defer 覆盖回源全程）。压缩在写锁内读它
	// ——seq-cst 语义保证「捕获世代前必已在册」，压缩时 >0 即整轮跳过
	// （保守：下轮清扫再收）。
	loading atomic.Int64

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
		gens:     make(map[string]uint64),
		done:     make(chan struct{}),
	}

	// 启动过期清理（NoBackgroundSweep 时不起后台件——pkg#25：
	// 短命 CLI/宿主内嵌场景不该为缓存常驻清扫协程）
	//
	// 条件是「**存在会过期的条目来源**」而不是「TTL > 0」：null marker 按
	// NullTTL 过期（缺省 1 分钟），TTL=0（真值永不过期）时 null marker 照样
	// 会过期。此前只看 TTL>0 ⇒ 这类配置下过期 null marker 永远没人回收
	// （惰性删除只覆盖 Get/GetEx/Exists，回源路径是非删除式的）⇒ 无界增长。
	if c.sweepNeeded() && !cfg.NoBackgroundSweep {
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
	v, _ := c.getHit(key)
	return v, nil
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
	v, hit := c.getHit(key)
	return v, hit, nil
}

// getHit Get/GetEx 的共同读内核（#373 收敛三胞胎）：getEntry → miss/hit
// 计数 → cloneOnRead。返回 (值, 是否命中真值条目)；Get 与 GetEx 只差返回
// 签名（3 行适配层），同一处判据改动不再需要同步两份。
// Peek 是**非删除式**读（见其注释），不走本内核。
func (c *LocalCache[T]) getHit(key string) (*T, bool) {
	entry, ok := c.getEntry(key)
	if !ok || entry.isNull {
		c.misses.Add(1)
		return nil, false
	}
	c.hits.Add(1)
	return c.cloneOnRead(entry.value), true
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
//
// StaleIfError 开启时，过期真值条目**不删**——留给 stale 兜底用（见
// retainForStale）。
func (c *LocalCache[T]) getEntry(key string) (*localEntry[T], bool) {
	c.mu.RLock()
	entry, ok := c.data[key]
	c.mu.RUnlock()

	if !ok {
		return nil, false
	}

	if entry.isExpired(c.now()) {
		c.lazyDeleteExpired(key, entry)
		return nil, false
	}

	// LRU：读命中提升顺序（先释放读锁，再取写锁）
	c.promoteOne(key, entry)
	return entry, true
}

// lazyDeleteExpired 惰性删除过期条目（#373 收敛单源）：getEntry 与 Exists
// 各持一段逐字相同的「双检删除」——抽出共用。retainForStale 为 true 的
// 过期真值保留（stale-if-error 兜底要用）；删除前二次检查条目仍是同一个
// （在 RUnlock 和 Lock 之间可能有别的 goroutine Set 了新值，只删仍然是
// 同一个过期 entry 的情况，避免误删新值）。调用方需已释放读锁。
func (c *LocalCache[T]) lazyDeleteExpired(key string, entry *localEntry[T]) {
	if c.retainForStale(entry) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cur, exists := c.data[key]; exists && cur == entry {
		delete(c.data, key)
		c.removeFromOrder(key)
		c.expirations.Add(1)
	}
}

// Set 设置缓存（使用默认 TTL）。
func (c *LocalCache[T]) Set(ctx context.Context, key string, value *T) error {
	return c.SetWithTTL(ctx, key, value, c.cfg.TTL)
}

// SetWithTTL 设置缓存（自定义 TTL）。写入推进 key 世代（#200）。
func (c *LocalCache[T]) SetWithTTL(ctx context.Context, key string, value *T, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeEntryLocked(key, value, ttl)
	return nil
}

// SetNX 仅当 key 不存在时写入（NX 语义，判定与写入在同一次写锁区间内原子
// 完成——同 Redis SET NX，契约全文见 NXSetter）。
//
// 存在性判定：未过期真值条目才算「已存在」（值为 nil 的真值条目同样占位）；
// 过期条目（含 StaleIfError 保留的过期真值）与 null marker 一律按不存在
// 处理并被本次写入覆盖。失败（false）是纯无操作：不覆盖值、不动 TTL、
// 不刷新淘汰顺序。ttl 与 SetWithTTL 同口径（<=0 不过期）；写路径不计 Stats。
// 成功写入推进 key 世代（#200）。
func (c *LocalCache[T]) SetNX(ctx context.Context, key string, value *T, ttl time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.data[key]; ok && !e.isExpired(c.now()) && !e.isNull {
		return false, nil
	}

	c.writeEntryLocked(key, value, ttl)
	return true, nil
}

// Del 删除缓存。删除推进 key 世代（#200）：写后失效（write-invalidate）模式
// 下，正在回源途中的加载会在 Source 返回后复查世代并放弃回填——失效语义
// 不被在途回源用旧值击穿整个 TTL 窗口。
func (c *LocalCache[T]) Del(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.data, key)
	c.removeFromOrder(key)
	c.bumpGen(key)
	return nil
}

// DelBatch 批量删除缓存。同 Del 推进各 key 世代（#200）。
func (c *LocalCache[T]) DelBatch(ctx context.Context, keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, key := range keys {
		delete(c.data, key)
		c.removeFromOrder(key)
		c.bumpGen(key)
	}
	return nil
}

// mcollect 批量读取的共用收集路径（MGet / MGetEx）：单次读锁区间取回一批
// 键的命中条目，锁区间之外完成 LRU 提升与命中/未命中计数。命中口径与单键
// 读一致：不存在、已过期与 null marker 一律缺席（计数为未命中）。
func (c *LocalCache[T]) mcollect(keys []string) []readHit[T] {
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
	return items
}

// MGet 批量获取：单次读锁区间取回一批键（契约见 BatchGetter——缺席语义
// 与 (nil, nil) 双关一致：不存在与 null marker 一律缺席；纯读不回填，
// 已过期条目按缺席处理，回收留给 Get/Exists 的双检路径——TTL=0 时无
// cleanup goroutine，过期条目仅在这些路径被惰性回收）。
//
// CloneOnRead 开启时按配置返回副本（pkg#25）；EvictLRU 策略下批量命中同样
// 提升顺序（读锁区间之外一次性提升，避免逐键取写锁）。
func (c *LocalCache[T]) MGet(ctx context.Context, keys ...string) (map[string]*T, error) {
	items := c.mcollect(keys)
	out := make(map[string]*T, len(items))
	for _, it := range items {
		// 命中真值但值为 nil 的条目同样进 map（键在场，值为 nil）
		out[it.key] = c.cloneOnRead(it.entry.value)
	}
	return out, nil
}

// MGetEx 批量获取并区分「nil 真值命中」与「缺席」（pkg#238）——GetEx 的
// 批量形态。MGet 返回的 map[string]*T 里，v, ok := m[k] 的 ok 对 nil 值
// 命中也为 true，与缺席的区分只能靠键存在性，消费方极易误用（v == nil
// 判缺席 ⇒ nil 真值条目被误判、触发不必要的回源；不判 nil 直接解引用 ⇒
// panic）。
//
// 返回 map 对**每个请求键**各给一条 BatchEntry：
//   - Hit=true：命中真值条目——Value 可能为 nil（调用方显式存了 nil 值）；
//   - Hit=false：key 不存在、已过期，或命中 null marker（缺席语义与
//     MGet 一致，见 BatchGetter 契约）。
//
// 其余行为与 MGet 同口径：纯读不回填、CloneOnRead 副本、EvictLRU 批量
// 提升、Stats 逐键计数。
func (c *LocalCache[T]) MGetEx(ctx context.Context, keys ...string) (map[string]BatchEntry[T], error) {
	items := c.mcollect(keys)
	out := make(map[string]BatchEntry[T], len(keys))
	for _, key := range keys {
		out[key] = BatchEntry[T]{} // 缺席基线（命中键在下方覆盖）
	}
	for _, it := range items {
		out[it.key] = BatchEntry[T]{Value: c.cloneOnRead(it.entry.value), Hit: true}
	}
	return out, nil
}

// 编译期能力契约：LocalCache 具备批量读取能力。
var _ BatchGetter[int] = (*LocalCache[int])(nil)

// 编译期能力契约：LocalCache 具备区分命中的批量读取能力（pkg#238）。
var _ BatchHitGetter[int] = (*LocalCache[int])(nil)

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
		c.lazyDeleteExpired(key, entry)
		return false, nil
	}

	return true, nil
}

// Refresh 刷新缓存（删除后重新从 Source 加载）。
func (c *LocalCache[T]) Refresh(ctx context.Context, key string) (*T, error) {
	if e := c.Del(ctx, key); e != nil {
		cacheLogger().Warn("local cache refresh del failed", "key", key, logkv.ErrKey, e)
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

// Clear 清空缓存。世代表一并重置（#200）：捕获过旧世代（>0）的在途回源
// 复查时不相等 ⇒ 放弃回填；从未写过的 key 世代本就是 0，不受影响。
// genSeq 不重置——序列单调递增保证 Clear 之后的新世代不与旧捕获值碰撞。
func (c *LocalCache[T]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]*localEntry[T])
	c.order.Init()
	c.orderIdx = make(map[string]*list.Element)
	c.gens = make(map[string]uint64)
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
