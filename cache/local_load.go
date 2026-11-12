package cache

import (
	"context"
	"time"

	"github.com/oekit/pkg/errors"
)

// 本文件是 LocalCache 的回源（Cache-Aside）与世代写入路径（#374 拆分）：
// GetOrLoad 家族、stale-if-error、per-key 世代（#200）与「复查世代 + 写入」
// 的原子回填。读写热路径见 local.go，清扫见 local_sweep.go。

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

// getOrLoadNoMiss 与 getOrLoad 同流程，但**不计入读计数**——未命中与命中
// 都不计（pkg#90、#250）。
//
// 用途：装饰器（SingleFlightCache）的前置探测已把同一次逻辑读计入内层
// 计数（未命中计一次 miss），加载路径再计任何一侧都会把一次读计成两次：
//   - pkg#90：加载路径再计一次 miss ⇒ 未命中双计（实测 90% 真实命中率被
//     报成 81.8%）；
//   - #250：前置探测与内层自检之间该 key 被并发写入（Set/刷新/回填）时，
//     加载路径的自检分支命中并补计一次 hit ⇒ 同一次读 miss+hit 双计，
//     命中率被系统性抬高。
//
// 本方法让「前置探测计一次、加载路径不再计」成立——无论加载路径自检的
// 结论是命中还是未命中，一次读恰好计入命中或未命中之一。
//
// 只供包内装饰器使用（能力随实现走，不进公共 API）：内层实现方不认这个
// 能力时，装饰器退回内层原有口径（能力缺失是内层的事实）。
func (c *LocalCache[T]) getOrLoadNoMiss(ctx context.Context, key string, spec loadSpec[T]) (*T, error) {
	return c.load(ctx, key, spec, false)
}

// load 是 getOrLoad 的实现：countMiss=false 时不计入读计数（未命中与命中
// 都不计，pkg#90 + #250，见 getOrLoadNoMiss），其余行为与计数口径完全一致。
func (c *LocalCache[T]) load(ctx context.Context, key string, spec loadSpec[T], countMiss bool) (*T, error) {
	// 1. 非删除式查看：未过期直接返回（null marker 命中 → (nil, nil) 防穿透）。
	//    过期条目保留在表内——StaleIfError 需要过期真值在回源失败时仍可读
	//    （惰性删除在此让位，回收交给后台清扫与其他读路径）
	now := c.now()
	c.mu.RLock()
	entry, exists := c.data[key]
	c.mu.RUnlock()

	if exists {
		if !entry.isExpired(now) {
			if entry.isNull {
				if countMiss {
					c.misses.Add(1)
				}
				return nil, nil
			}
			// 读命中：LRU 提升顺序 + 按 CloneOnRead 决定返回副本还是共享引用。
			// countMiss=false（装饰器前置探测已计过这次读）时命中同样不计
			// （#250）——否则前置探测（miss）与本分支（hit）把一次读计成两次。
			c.promoteOne(key, entry)
			if countMiss {
				c.hits.Add(1)
			}
			return c.cloneOnRead(entry.value), nil
		}
	}
	if countMiss {
		c.misses.Add(1)
	}

	// 2. 回源前捕获 key 世代（#200）：Source 执行期间该 key 被 Del / 覆盖
	//    写入（写后失效）时世代推进，回填阶段据此放弃——在途回源不能把
	//    失效前的旧值重新灌回缓存。
	//
	//    先登记在途（#396 墓碑压缩的安全闸）：gens 压缩只在「无在途回源」
	//    时执行，捕获世代前必须已在册——否则存在「捕获 0 → Del 造墓碑 →
	//    压缩删墓碑 → 复查见 0 误判未失效」的窗口。defer 保证覆盖回源全程
	//    （含错误/stale 分支——那些分支不复查世代，更长的在途窗口只会让
	//    压缩多等一轮，方向保守）。
	c.loading.Add(1)
	defer c.loading.Add(-1)
	gen := c.generation(key)

	// 回源（按调用注入优先于构造期；SourceWithTTL 优先于 Source）
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
		// stale-if-error：回源失败时回退已过期真值（fail-open）。
		//
		// 选择性 stale（#172）：兜底资格在**回源之后**重新捕获（不再是调
		// Source 前的快照），且错误 unwrap 命中 ErrNoStale 时原样回传——
		//   - Source 在被调用期间对本 key 调 Del 即取消本次兜底（此前
		//     stale 标志先于 Source 捕获，Source 内 Del 表达不了「此错误
		//     不走 stale」，消费方只能写毒标记绕行）；
		//   - 返回 cache.ErrNoStale 包装错误按错误类型取消（如上游明确
		//     拒绝 401/403——不得借旧缓存旁路认证）。
		if c.cfg.StaleIfError && !errors.Is(err, ErrNoStale) {
			staleEnt, ok := c.staleEntryNow(key)
			if ok {
				if c.cfg.OnStale != nil {
					c.cfg.OnStale(ctx, key, staleEnt.value)
				}
				// stale 兜底**不计读命中**：Stats 的口径是「读到**未过期的**真值
				// 条目」才算命中，过期真值是「已过期」⇒ 计入未命中。
				// 此前这条路径既计未命中又计命中 ⇒ 一次读被计成两次（命中+未命中）
				// ⇒ 命中率虚高（与 interface.go「一次读恰好计入命中或未命中之一」
				// 的承诺矛盾）。
				// 顺序提升仍要做（stale 也是一次使用）。
				c.promoteOne(key, staleEnt)
				return c.cloneOnRead(staleEnt.value), nil
			}
		}
		return nil, err
	}

	// 3. 回填缓存（best-effort，失败仅记录不影响主流程）。
	//    回填前先复查世代（#200）：Source 执行期间该 key 被 Del / 覆盖写入
	//    ⇒ 本次回填放弃——写后失效语义不能被在途回源击穿整个 TTL 窗口。
	//    取到的值仍返回给调用方（读已发生，值是它发起回源拿到的）。
	if val == nil {
		// 缓存空值防穿透（同样受世代复查约束：失效后的 null marker 会把
		// 下一次读挡在 NullTTL 窗口外）
		if nullTTL := c.effectiveNullTTL(); nullTTL > 0 {
			if !c.setNullMarkerIfGen(key, nullTTL, gen) {
				cacheLogger().Debug("local cache null-marker backfill skipped: key invalidated during load", "key", key)
			}
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
	// 回填：**存一份副本**，把调用方自己的值还给调用方。
	//
	// 此前直接把 val 存进缓存再把同一个指针返回给调用方——回填之后这个
	// 指针已经是缓存的条目了，调用方一改写就污染缓存（CloneOnRead 开启也
	// 挡不住：下次读命中克隆的是**已被改写**的那份）。LocalConfig.CloneOnRead
	// 的注释说「回源路径不克隆——那是调用方自己的值」，在回填前成立，
	// 回填后不成立。
	//
	// 未配置 CloneOnRead 时保持零拷贝（既有口径：调用方此后不得改写）。
	stored := val
	if c.cfg.CloneOnRead != nil {
		if cloned := c.cfg.CloneOnRead(val); cloned != nil {
			stored = cloned
		}
	}
	if !c.setWithTTLIfGen(key, stored, ttl, gen) {
		cacheLogger().Debug("local cache backfill skipped: key invalidated during load", "key", key)
	}

	return val, nil
}

// generation 读取 key 的当前世代（#200）。
// 世代由写路径推进（见 bumpGen）；从未写过的 key 世代为 0。
func (c *LocalCache[T]) generation(key string) uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.gens[key]
}

// bumpGen 推进 key 的世代（#200；调用方需持写锁）。
// 世代取值来自全局单调序列 genSeq——永不复用，Clear 之后新写入的世代
// 不会与任何在途回源捕获过的旧世代相等（相等是「可以回填」的判据，
// 复用会把误判变成静默击穿）。
func (c *LocalCache[T]) bumpGen(key string) {
	c.gens[key] = c.genSeq.Add(1)
}

// expiry 按 ttl 计算条目过期时刻（ttl <= 0 = 永不过期，零值 time.Time）。
// 注入时钟统一走 c.now——TTL/NullTTL 全路径的同一判定口径。
func (c *LocalCache[T]) expiry(ttl time.Duration) time.Time {
	if ttl > 0 {
		return c.now().Add(ttl)
	}
	return time.Time{}
}

// putEntryLocked 写入条目 + 淘汰检查 + 顺序提升 + 推进世代（#200/#374 的
// 世代写入路径单源；调用方需持写锁）。writeEntryLocked / writeNullEntryLocked
// 是仅差 entry 形态的两个入口——此前「真值写入」与「null marker 写入」
// 各持一份近似实现（Lock→gen 复查→淘汰检查→写 entry→moveToBack→bumpGen），
// 判据改动要同步两处。
func (c *LocalCache[T]) putEntryLocked(key string, entry *localEntry[T]) {
	// 检查是否需要淘汰
	if c.cfg.MaxSize > 0 && len(c.data) >= c.cfg.MaxSize {
		if _, exists := c.data[key]; !exists {
			c.evictOldest()
		}
	}

	c.data[key] = entry
	c.moveToBack(key)
	c.bumpGen(key)
}

// writeEntryLocked 写入真值条目并推进世代（#200；调用方需持写锁）。
// SetWithTTL / SetNX / setWithTTLIfGen 的共同写路径。
func (c *LocalCache[T]) writeEntryLocked(key string, value *T, ttl time.Duration) {
	c.putEntryLocked(key, &localEntry[T]{value: value, expiresAt: c.expiry(ttl)})
}

// writeNullEntryLocked 写入 null marker 条目并推进世代（#200/#374；调用方
// 需持写锁）。setNullMarkerIfGen 的写入体——与真值写入共用 putEntryLocked
// 骨架，仅 isNull 标记不同。
func (c *LocalCache[T]) writeNullEntryLocked(key string, ttl time.Duration) {
	c.putEntryLocked(key, &localEntry[T]{isNull: true, expiresAt: c.expiry(ttl)})
}

// setWithTTLIfGen 世代未变时写入真值条目（#200）——「复查世代 + 回填」在
// 同一写锁区间内原子完成，不存在「复查通过、写入前又被 Del」的窗口。
// 世代已变（Source 执行期间该 key 被 Del/覆盖写入）返回 false 且不写入。
func (c *LocalCache[T]) setWithTTLIfGen(key string, value *T, ttl time.Duration, gen uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gens[key] != gen {
		return false
	}
	c.writeEntryLocked(key, value, ttl)
	return true
}

// setNullMarkerIfGen 世代未变时写入 null marker 条目（#200——回源路径的
// null marker 回填与 setWithTTLIfGen 同纪律：「复查世代 + 写入」原子完成，
// 失效后的 null marker 不写入）。世代已变返回 false 且不写入。
// 写入体复用 writeNullEntryLocked（#374：世代写入路径单源）。
func (c *LocalCache[T]) setNullMarkerIfGen(key string, ttl time.Duration, gen uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gens[key] != gen {
		return false
	}
	c.writeNullEntryLocked(key, ttl)
	return true
}

// effectiveNullTTL 返回生效的空值缓存 TTL：
// 0（未设置）→ 默认 1 分钟；负值 → 原样返回（<=0 表示禁用）。
func (c *LocalCache[T]) effectiveNullTTL() time.Duration {
	if c.cfg.NullTTL == 0 {
		return defaultNullTTL
	}
	return c.cfg.NullTTL
}

// staleEntryNow 回源之后重新捕获 stale 兜底资格（#172）：此刻表内仍存在
// 「TTL 过期的真值条目」时返回它，否则报告无值可兜。
//
// 只看当下状态，正是「选择性 stale」的取消机制：Source 在被调用期间
// Del（或覆盖写入）该 key，兜底资格即刻消失——修复前 stale 标志在调 Source
// 之前捕获，Source 内 Del 也取消不了本次兜底。
func (c *LocalCache[T]) staleEntryNow(key string) (*localEntry[T], bool) {
	now := c.now()
	c.mu.RLock()
	entry, ok := c.data[key]
	c.mu.RUnlock()
	if !ok || !entry.isExpired(now) || !c.retainForStale(entry) {
		return nil, false
	}
	return entry, true
}

// retainForStale 报告该条目是否要保留给 stale-if-error 兜底（不参与回收）。
//
// 只认「TTL 过期的**真值**条目」：null marker 表示数据不存在，没有可兜底的
// 历史真值；TTL=0 的条目永不过期，没有 stale 语义。
//
// StaleIfError 开启时，过期真值条目**不被回收**——LocalConfig.StaleIfError
// 的承诺是「过期条目在本路径下保留于表内……可反复兜底直至上游恢复」。此前
// 后台清扫（缺省开启、TTL/2 一跳）会把过期真值一并删掉，惰性删除
// （Get/GetEx/Exists）也是⇒ 兜底只在「[过期, 下次清扫)」这个窗口里有效，
// 文档承诺落空（实测：TTL=100ms 时，清扫在 50ms 后就把兜底值删了）。
func (c *LocalCache[T]) retainForStale(entry *localEntry[T]) bool {
	if !c.cfg.StaleIfError || entry == nil {
		return false
	}
	return !entry.isNull && !entry.expiresAt.IsZero()
}
