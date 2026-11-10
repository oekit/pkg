package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/oekit/pkg/errors"
	"github.com/oekit/pkg/internal/singleflight"
)

// noMissLoader 是包内能力接口：回源加载且**不计入读计数**（pkg#90、#250）。
//
// 存在的理由：装饰器的前置检查（Peek）已经把这次逻辑读计入内层计数
// （未命中计一次 miss），内层加载路径再计任何一侧都会把一次读计成两次：
//   - pkg#90：加载路径再计一次 miss ⇒ 未命中双计（实测 90% 的真实命中率
//     被报成 81.8%）；
//   - #250：前置检查与内层自检之间该 key 被并发写入时，加载路径的自检
//     分支命中并补计一次 hit ⇒ 同一次读 miss+hit 双计，命中率被系统性
//     抬高。
//
// 口径：未命中与命中计数都由前置检查负责，加载路径一律不重复计。内层
// 不实现该能力时，装饰器退回内层原有口径（能力缺失是内层的事实），
// 计数偏差按内层语义解释。
//
// 不进公共 API：这是「内层实现方与本包装饰器」之间的包内约定，
// 外部后端无从实现（也无需实现——只有本包的 LocalCache 暴露计数面）。
type noMissLoader[T any] interface {
	// getOrLoadNoMiss 按 spec 回源加载，不计入读计数（命中与未命中都不计）。
	getOrLoadNoMiss(ctx context.Context, key string, spec loadSpec[T]) (*T, error)
}

// SingleFlightCache 是带 singleflight 保护的缓存装饰器。
// 同一个 key 的并发请求只会有一个实际调用 Source，其他请求等待共享结果。
// 防止缓存击穿（热点 key 过期时的雷群效应）。
//
// 可以包装任何 CacheAside[T] 实现（Redis、本地缓存等）。
type SingleFlightCache[T any] struct {
	inner  CacheAside[T]
	flight *singleflight.Flight[*T]
}

// NewSingleFlightCache 创建带 singleflight 保护的缓存装饰器。
// inner 可以是 RedisCache、LocalCache 或任何实现 CacheAside[T] 的缓存。
func NewSingleFlightCache[T any](inner CacheAside[T]) *SingleFlightCache[T] {
	return &SingleFlightCache[T]{
		inner:  inner,
		flight: singleflight.New[*T](),
	}
}

// Get 获取缓存（纯查询，不触发 Source）。
func (sf *SingleFlightCache[T]) Get(ctx context.Context, key string) (*T, error) {
	return sf.inner.Get(ctx, key)
}

// Peek 非删除式读取（pkg#72）：读到过期条目时**不删除**它，只是不命中。
//
// 装饰器必须透明：内层支持 cache.PeekGetter 时原样委托；否则退化为 Get
// 语义（内层可能惰性删除过期条目——回收口径由内层决定）。内层不支持时
// 仍返回本方法，是为了让「装饰器不丢能力」这条约束成立：外层再包一层
// 装饰器时，前置检查仍能走非删除式读路径，不会再次废掉 stale-if-error
// 兜底（内层的 Peek 缺失时，兜底本就无从谈起——退化是能力缺失，不是
// 装饰器引入的偏差）。
//
// 命中语义与内层的 Peek 一致：hit=false 表示未命中（不存在/已过期/null
// marker）；hit=true 时返回值可能为 nil（命中但值为 nil）。
func (sf *SingleFlightCache[T]) Peek(ctx context.Context, key string) (*T, bool, error) {
	return sf.peek(ctx, key)
}

// Set 设置缓存。
func (sf *SingleFlightCache[T]) Set(ctx context.Context, key string, value *T) error {
	return sf.inner.Set(ctx, key, value)
}

// SetWithTTL 设置缓存（自定义 TTL）。
func (sf *SingleFlightCache[T]) SetWithTTL(ctx context.Context, key string, value *T, ttl time.Duration) error {
	return sf.inner.SetWithTTL(ctx, key, value, ttl)
}

// ErrNXUnsupported SetNX 转发时内层不具备 NXSetter 能力（fail-loud，不静默假成功）。
var ErrNXUnsupported = errors.New("cache: inner does not support NX semantics (cache.NXSetter)")

// SetNX 不存在才写入（NX 语义，转发不打折，契约全文见 NXSetter）。
//
// 装饰器必须透明：「不存在」判定与写入的原子性完全由内层的单次锁区间保证
// ——本方法只做能力转发，绝不退化为 Get+Set 两步（并发下会双双判「不存在」
// 而互相覆盖，NX 语义当场作废）。内层不具备 NXSetter 时返回可 errors.Is
// 判定的 ErrNXUnsupported（能力缺失要看得见，不能假装成功）。
func (sf *SingleFlightCache[T]) SetNX(ctx context.Context, key string, value *T, ttl time.Duration) (bool, error) {
	if nx, ok := sf.inner.(NXSetter[T]); ok {
		return nx.SetNX(ctx, key, value, ttl)
	}
	return false, errors.Wrap(ErrNXUnsupported, "SingleFlightCache.SetNX", "inner", fmt.Sprintf("%T", sf.inner))
}

// Del 删除缓存。
func (sf *SingleFlightCache[T]) Del(ctx context.Context, key string) error {
	return sf.inner.Del(ctx, key)
}

// DelBatch 批量删除缓存。
func (sf *SingleFlightCache[T]) DelBatch(ctx context.Context, keys ...string) error {
	return sf.inner.DelBatch(ctx, keys...)
}

// Exists 检查缓存是否存在。
func (sf *SingleFlightCache[T]) Exists(ctx context.Context, key string) (bool, error) {
	return sf.inner.Exists(ctx, key)
}

// sfPath 区分三条加载路径的 SourceInjector 转发形态（pkg#353 重构）：
// 三条路径对内层的委托口径各不相同（构造期形态不走注入面），必须如实分派。
type sfPath int

const (
	sfDefault       sfPath = iota // GetOrLoad：构造期 Source + 默认 TTL
	sfWithSource                  // GetOrLoadWithSource：按调用注入 source
	sfWithSourceTTL               // GetOrLoadWithSourceTTL：按调用注入 source + ttl
)

// loadOnce 一次「直接回源」执行体（与原 flight 闭包的内层分发口径逐分支一致）：
// 内层支持「加载不计读数」时走 noMiss 口径（pkg#90——前置检查已计过同一次读），
// 其次按路径形态转发 SourceInjector（pkg#77），内层两样都不支持时退回内层
// GetOrLoad。GetOrLoad 经 flight 包它；按调用注入的路径不经 flight 直呼
// （pkg#353——source/函数不可比较，合并会让等待者拿走先到者的回源结果，
// 自身注入的 source/TTL 被静默丢弃）。
func (sf *SingleFlightCache[T]) loadOnce(ctx context.Context, key string, path sfPath, source SourceFunc[T], sourceTTL SourceFuncWithTTL[T], ttl time.Duration) (*T, error) {
	if nl, ok := sf.inner.(noMissLoader[T]); ok {
		var spec loadSpec[T]
		switch path {
		case sfWithSource:
			spec.source = source
		case sfWithSourceTTL:
			spec.sourceTTL = sourceTTL
			spec.ttl = ttl
		}
		return nl.getOrLoadNoMiss(ctx, key, spec)
	}
	switch path {
	case sfWithSource:
		if si, ok := sf.inner.(SourceInjector[T]); ok {
			return si.GetOrLoadWithSource(ctx, key, source)
		}
	case sfWithSourceTTL:
		if si, ok := sf.inner.(SourceInjector[T]); ok {
			return si.GetOrLoadWithSourceTTL(ctx, key, sourceTTL, ttl)
		}
	}
	return sf.inner.GetOrLoad(ctx, key)
}

// GetOrLoad 获取缓存，带 singleflight 保护。
// 同一个 key 的并发请求只会有一个实际调用 Source。
//
// 流程：
//  1. 查缓存，命中则直接返回
//  2. 未命中时通过 singleflight 去重，只有一个 goroutine 调用 Source
//  3. Source 返回后回填缓存
//  4. 其他 goroutine 共享同一份结果
//
// ⚠️ 前置检查走**非删除式**读路径（Peek，pkg#72）：走 Get 会命中 LocalCache
// 的惰性删除路径，把 stale-if-error 兜底需要的过期真值删掉 ⇒ 包装 LocalCache
// 后 StaleIfError 形同虚设（回源失败直接抛错，不再兜底历史真值）。
//
// 命中即早退（#203）：hit=true 时**无条件**返回（值可能为 nil——显式存入的
// nil 真值条目，见包级契约「(nil, nil) 三种情形」）。此前的条件是
// hit && value != nil，nil 值命中会落入 flight.Do → 加载路径，而加载路径的
// 命中分支会**再计一次 hit** ⇒ 一次读被计成两次命中，命中率被系统性抬高
// （违背 pkg#90「一次读恰好计入命中或未命中之一」）。
//
// 合并语义（pkg#353）：本方法全体调用方语义同一（构造期 Source + 默认 TTL），
// 按 key 合并安全。
func (sf *SingleFlightCache[T]) GetOrLoad(ctx context.Context, key string) (*T, error) {
	// 1. 先查缓存（命中即返——含值为 nil 的真值命中，#203）
	value, hit, err := sf.peek(ctx, key)
	if err != nil {
		return nil, err
	}
	if hit {
		return value, nil
	}

	// 2. singleflight 去重，调用 inner 的 GetOrLoad
	//    （pkg#90：内层支持「加载不计未命中」时走该路径——前置检查已计过同一次
	//    未命中，加载路径再计一次会把一次未命中计成两次，命中率指标失真）
	val, err := sf.flight.Do(ctx, key, func(ctx context.Context) (*T, error) {
		return sf.loadOnce(ctx, key, sfDefault, nil, nil, 0)
	})
	if err != nil {
		return nil, err
	}
	return val, nil
}

// peek 是前置检查的读路径：内层支持非删除式读（PeekGetter）时用它——读到
// 过期条目时不删除它（stale-if-error 兜底需要那份值留在表内，pkg#72）；
// 内层不支持时退回 Get（历史行为：由内层自行决定过期条目的回收口径）。
//
// 命中口径：内层支持 PeekGetter 时 hit 由内层给出（值为 nil 的真值命中同样
// hit=true，调用方命中即早退——#203）；退回 Get 的路径上拿到非 nil 值才算
// 命中（null marker 与「命中但值为 nil」都按未命中走，由 inner.GetOrLoad
// 给出最终结果——该路径无法区分 nil 真值，不做早退，代价是该口径下 nil
// 值命中可能被内层按其自身语义计一次命中）。
func (sf *SingleFlightCache[T]) peek(ctx context.Context, key string) (*T, bool, error) {
	if pg, ok := sf.inner.(PeekGetter[T]); ok {
		return pg.Peek(ctx, key)
	}
	v, err := sf.inner.Get(ctx, key)
	if err != nil {
		return nil, false, err
	}
	return v, v != nil, nil
}

// GetOrLoadWithSource 按调用注入的 source 回源（pkg#77）。
//
// 与 GetOrLoad 的差别：回源函数由**调用方在调用点**给出，而不是构造期固定
// ——面向「回源依赖 key / 依赖调用上下文」的形态（token / 凭据 / 租户级缓存）。
//
// ⚠️ 合并语义（pkg#353）：**本路径不进 singleflight**。source 函数不可比较，
// 按 key 合并会让等待者共享先到者的回源结果——自身注入的 source 被静默丢弃
// （同 key 不同调用方注入不同回源时，可能拿到别的调用方凭据取回的值）。
// 并发未命中会各自回源（雷群防护只在构造期 Source 的 GetOrLoad 与 Refresh 上）；
// 需要对同 key 去重时用构造期 Source 的 GetOrLoad。
//
// 内层不支持按调用注入（未实现 cache.SourceInjector）时退回内层的构造期
// Source——能力缺失是内层的事实，不是装饰器引入的偏差。
func (sf *SingleFlightCache[T]) GetOrLoadWithSource(ctx context.Context, key string, source SourceFunc[T]) (*T, error) {
	// 1. 前置检查走非删除式读路径（同 GetOrLoad——pkg#72；命中即返含
	//    nil 值真值命中，#203）
	value, hit, err := sf.peek(ctx, key)
	if err != nil {
		return nil, err
	}
	if hit {
		return value, nil
	}

	// 2. 直接回源，不经 flight（pkg#353：合并会串味）
	return sf.loadOnce(ctx, key, sfWithSource, source, nil, 0)
}

// GetOrLoadWithSourceTTL 按调用注入的 source 回源并按 ttl 回填（pkg#77）。
//
// 与 GetOrLoadWithSource 的差别：回源函数可携带动态 TTL（SourceFuncWithTTL），
// 且 ttl > 0 时覆盖回源给出的 TTL（ttl <= 0 时按回源给出的 TTL，仍无则
// 退回构造期默认 TTL）。
//
// ⚠️ 合并语义（pkg#353）：同 GetOrLoadWithSource——**本路径不进 singleflight**，
// 并发未命中各自回源；注入的 source 与 TTL 不被并发合并吞掉。
//
// 内层不支持按调用注入（未实现 cache.SourceInjector）时退回内层的构造期
// 回源；此时 ttl > 0 仍生效（覆盖构造期回源给出的 TTL）。
func (sf *SingleFlightCache[T]) GetOrLoadWithSourceTTL(ctx context.Context, key string, source SourceFuncWithTTL[T], ttl time.Duration) (*T, error) {
	// 1. 前置检查走非删除式读路径（同 GetOrLoad——pkg#72；命中即返含
	//    nil 值真值命中，#203）
	value, hit, err := sf.peek(ctx, key)
	if err != nil {
		return nil, err
	}
	if hit {
		return value, nil
	}

	// 2. 直接回源，不经 flight（pkg#353：合并会串味/丢 TTL）
	return sf.loadOnce(ctx, key, sfWithSourceTTL, nil, source, ttl)
}

// Stats 返回内层缓存的观测计数快照（pkg#76）。
//
// 装饰器不得丢能力：内层实现 cache.StatsProvider 时原样委托；否则返回零值
// 快照——能力缺失是内层的事实（同 MGet/Peek 的退化口径）。
//
// 计数口径（pkg#90 + #250）：装饰器的前置检查计入内层计数，回源加载路径
// 不再重复计任何一侧（未命中与命中都不计）⇒ 一次读恰好计入命中或未命中
// 之一（与内层直连一致）。
func (sf *SingleFlightCache[T]) Stats() Stats {
	if sp, ok := sf.inner.(StatsProvider); ok {
		return sp.Stats()
	}
	return Stats{}
}

// MGet 批量获取：内层具备批量能力时直接委托（装饰器不得退化批量）；
// 内层无批量能力时退化为逐键 Get。
func (sf *SingleFlightCache[T]) MGet(ctx context.Context, keys ...string) (map[string]*T, error) {
	if bg, ok := sf.inner.(BatchGetter[T]); ok {
		return bg.MGet(ctx, keys...)
	}
	out := make(map[string]*T, len(keys))
	for _, key := range keys {
		v, err := sf.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		if v != nil {
			out[key] = v
		}
	}
	return out, nil
}

// refreshFlightKeyPrefix Refresh 路径的 flight 键命名空间前缀（pkg#353）：
// 与 load 路径分键——「强刷」是删除重建，与进行中的普通加载合并会把强刷
// 退化成「加入当前加载」，等待者可能拿回刷新请求之前的旧加载结果。
const refreshFlightKeyPrefix = "\x00refresh:"

// Refresh 刷新缓存（带 singleflight 保护）。
//
// 合并语义（pkg#353）：Refresh 只与同 key 的其他 Refresh 合并（语义同一：
// 都是删除重建）；与进行中的 GetOrLoad 不合并——否则强刷退化为加入当前
// 加载，返回旧值。Refresh 只对构造期 Source 的形态有意义（与
// GetOrLoadWithSource 不同，这里无逐调用参数，按 key 合并安全）。
func (sf *SingleFlightCache[T]) Refresh(ctx context.Context, key string) (*T, error) {
	val, err := sf.flight.Do(ctx, refreshFlightKeyPrefix+key, func(ctx context.Context) (*T, error) {
		return sf.inner.Refresh(ctx, key)
	})
	if err != nil {
		return nil, err
	}
	return val, nil
}

// 编译期能力契约：SingleFlightCache 具备观测计数能力（pkg#76）、按调用
// 注入回源的能力（pkg#77）与 NX 写入能力（NXSetter）——装饰器不丢能力。
var (
	_ StatsProvider       = (*SingleFlightCache[int])(nil)
	_ SourceInjector[int] = (*SingleFlightCache[int])(nil)
	_ NXSetter[int]       = (*SingleFlightCache[int])(nil)
)
