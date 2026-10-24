package cache

import (
	"context"
	"time"

	"github.com/oekit/pkg/internal/singleflight"
)

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
func (sf *SingleFlightCache[T]) GetOrLoad(ctx context.Context, key string) (*T, error) {
	// 1. 先查缓存
	value, hit, err := sf.peek(ctx, key)
	if err != nil {
		return nil, err
	}
	if hit && value != nil {
		return value, nil
	}

	// 2. singleflight 去重，调用 inner 的 GetOrLoad
	val, err := sf.flight.Do(ctx, key, func(ctx context.Context) (*T, error) {
		return sf.inner.GetOrLoad(ctx, key)
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
// 命中判定与历史实现一致：拿到非 nil 值才算命中（null marker 与「命中但
// 值为 nil」都按未命中走，由 inner.GetOrLoad 给出最终结果）。
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

// GetOrLoadWithSource 按调用注入的 source 回源（pkg#77，带 singleflight 保护）。
//
// 与 GetOrLoad 的差别只有一处：回源函数由**调用方在调用点**给出，而不是
// 构造期固定——面向「回源依赖 key / 依赖调用上下文」的形态（token / 凭据 /
// 租户级缓存）。单飞语义不变：同一 key 的并发请求只会有一个实际调用 source。
//
// 内层不支持按调用注入（未实现 cache.SourceInjector）时退回内层的构造期
// Source——能力缺失是内层的事实，不是装饰器引入的偏差。
func (sf *SingleFlightCache[T]) GetOrLoadWithSource(ctx context.Context, key string, source SourceFunc[T]) (*T, error) {
	// 1. 前置检查走非删除式读路径（同 GetOrLoad——pkg#72）
	value, hit, err := sf.peek(ctx, key)
	if err != nil {
		return nil, err
	}
	if hit && value != nil {
		return value, nil
	}

	// 2. singleflight 去重，按调用注入的 source 回源
	val, err := sf.flight.Do(ctx, key, func(ctx context.Context) (*T, error) {
		if si, ok := sf.inner.(SourceInjector[T]); ok {
			return si.GetOrLoadWithSource(ctx, key, source)
		}
		return sf.inner.GetOrLoad(ctx, key)
	})
	if err != nil {
		return nil, err
	}
	return val, nil
}

// GetOrLoadWithSourceTTL 按调用注入的 source 回源并按 ttl 回填（pkg#77，
// 带 singleflight 保护）。
//
// 与 GetOrLoadWithSource 的差别：回源函数可携带动态 TTL（SourceFuncWithTTL），
// 且 ttl > 0 时覆盖回源给出的 TTL（ttl <= 0 时按回源给出的 TTL，仍无则
// 退回构造期默认 TTL）。
//
// 内层不支持按调用注入（未实现 cache.SourceInjector）时退回内层的构造期
// 回源；此时 ttl > 0 仍生效（覆盖构造期回源给出的 TTL）。
func (sf *SingleFlightCache[T]) GetOrLoadWithSourceTTL(ctx context.Context, key string, source SourceFuncWithTTL[T], ttl time.Duration) (*T, error) {
	// 1. 前置检查走非删除式读路径（同 GetOrLoad——pkg#72）
	value, hit, err := sf.peek(ctx, key)
	if err != nil {
		return nil, err
	}
	if hit && value != nil {
		return value, nil
	}

	// 2. singleflight 去重，按调用注入的 source 回源
	val, err := sf.flight.Do(ctx, key, func(ctx context.Context) (*T, error) {
		if si, ok := sf.inner.(SourceInjector[T]); ok {
			return si.GetOrLoadWithSourceTTL(ctx, key, source, ttl)
		}
		return sf.inner.GetOrLoad(ctx, key)
	})
	if err != nil {
		return nil, err
	}
	return val, nil
}

// Stats 返回内层缓存的观测计数快照（pkg#76）。
//
// 装饰器不得丢能力：内层实现 cache.StatsProvider 时原样委托；否则返回零值
// 快照——能力缺失是内层的事实（同 MGet/Peek 的退化口径）。
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

// Refresh 刷新缓存（带 singleflight 保护）。
func (sf *SingleFlightCache[T]) Refresh(ctx context.Context, key string) (*T, error) {
	val, err := sf.flight.Do(ctx, key, func(ctx context.Context) (*T, error) {
		return sf.inner.Refresh(ctx, key)
	})
	if err != nil {
		return nil, err
	}
	return val, nil
}

// 编译期能力契约：SingleFlightCache 具备观测计数能力（pkg#76）与按调用
// 注入回源的能力（pkg#77）——装饰器不丢能力。
var (
	_ StatsProvider       = (*SingleFlightCache[int])(nil)
	_ SourceInjector[int] = (*SingleFlightCache[int])(nil)
)
