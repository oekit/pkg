package cache

import (
	"context"
	"time"

	"github.com/oekit/pkg/errors"
)

// singleValueKey 是 SingleValue 内部复用键值形态时的固定键。
// 每个 SingleValue 实例持有独享的 LocalCache/SingleFlightCache，该常量键
// 不与任何其它实例共享——它只是内部实现细节，调用方不感知。
const singleValueKey = "value"

// ErrNoValue 是 SingleValue.Get 的「无真值」哨兵（#213）。
//
// 语义与 GetOrLoad 的 (nil, nil) 对齐（首查无真值 / 命中 null marker /
// 未配置 Source 且缓存为空）：键值形态下调用方拿 (nil, nil) 自行判miss，
// 单值形态没有 key 可言，「无值可返」必须经 error 通道表达——调用方
// errors.Is(err, ErrNoValue) 判定后自行决定兜底值。
var ErrNoValue = errors.New("cache: single value has no value")

// SingleValueSource 单值回源函数：与 SourceFunc 的唯一差别是**没有 key
// 参数**——单值缓存（远程标志位/开关状态/假期日历）的回源不依赖键，
// 借键值形态时被迫写的 `_ string` 哑参正是本形态要消除的胶水。
type SingleValueSource[T any] func(ctx context.Context) (*T, error)

// SingleValueSourceWithTTL 携带动态回填 TTL 的单值回源函数
// （与 SourceFuncWithTTL 同语义：ttl > 0 按其缓存，<= 0 回退 TTL）。
type SingleValueSourceWithTTL[T any] func(ctx context.Context) (*T, time.Duration, error)

// SingleValueConfig 配置单值缓存实例。字段语义与 LocalConfig 同名
// 字段一致（单值形态没有 MaxSize/EvictPolicy——只有一个条目），差别只在
// 回源函数没有 key 参数。
type SingleValueConfig[T any] struct {
	// TTL 缓存过期时间。0 表示不过期。
	TTL time.Duration

	// Source 回源函数（无 key 参数）。返回 (nil, nil) 表示当前无值——
	// 按 NullTTL 缓存 null marker 防穿透，期间 Get 返回 ErrNoValue。
	Source SingleValueSource[T]

	// SourceWithTTL 动态回填 TTL 的回源函数（与 Source 同时配置时本字段
	// 优先，语义同 LocalConfig.SourceWithTTL）。
	SourceWithTTL SingleValueSourceWithTTL[T]

	// NullTTL 空值缓存过期时间（语义同 LocalConfig.NullTTL：
	// 0 = 默认 1 分钟，负值 = 禁用空值缓存）。
	NullTTL time.Duration

	// Now 时钟注入（语义同 LocalConfig.Now；测试注入假时钟可确定性驱动
	// TTL/NullTTL 边界）。
	Now func() time.Time

	// StaleIfError 回源失败时回退已过期的历史真值（语义同 LocalConfig.
	// StaleIfError，含选择性 stale 的 Del/ErrNoStale 取消路径——对本实例
	// 调 Del 即取消）。
	StaleIfError bool

	// OnStale stale 命中回调（语义同 LocalConfig.OnStale）。
	OnStale func(ctx context.Context, stale *T)

	// NoBackgroundSweep 关闭后台清扫 goroutine（语义同 LocalConfig.
	// NoBackgroundSweep；此时由调用方按需 Del/Refresh，单值场景通常
	// 推荐开启——不必为一个条目常驻协程）。
	NoBackgroundSweep bool

	// CloneOnRead 读命中返回副本（语义同 LocalConfig.CloneOnRead）。
	CloneOnRead func(*T) *T
}

// SingleValue 是单值 TTL 缓存（#213）：进程内只有一个值的缓存形态——
// 远程标志位 / 开关状态 / 假期日历这类「单值」消费方不必借键值形态
// 自造常量键 + 忍受无意义的 key 参数。
//
// 内部复用 LocalCache + SingleFlightCache（固定内部键承载，不另造实现）：
// TTL/null marker/stale-if-error/单飞防击穿/观测计数等语义与键值形态
// 完全一致；并发安全，Get 可多 goroutine 并发调用。
//
// 与 GetOrLoad 的零值兜底语义对齐：无真值（首查回源返回 nil / 命中
// null marker / 未配置 Source 且缓存为空）时 Get 返回 ErrNoValue——
// 单值形态没有 key，「无值」经哨兵错误表达，兜底值由调用方决定。
type SingleValue[T any] struct {
	local *LocalCache[T]
	inner *SingleFlightCache[T]
}

// NewSingleValue 创建单值缓存。
func NewSingleValue[T any](cfg SingleValueConfig[T]) *SingleValue[T] {
	localCfg := LocalConfig[T]{
		TTL:               cfg.TTL,
		NullTTL:           cfg.NullTTL,
		Now:               cfg.Now,
		StaleIfError:      cfg.StaleIfError,
		NoBackgroundSweep: cfg.NoBackgroundSweep,
		CloneOnRead:       cfg.CloneOnRead,
	}
	if cfg.OnStale != nil {
		onStale := cfg.OnStale
		localCfg.OnStale = func(ctx context.Context, _ string, stale *T) {
			onStale(ctx, stale)
		}
	}
	// 回源函数按需适配（去掉无意义的 key 哑参）；两个都未配置时保持
	// LocalConfig 的既有口径——无 Source 的缓存，Get 在空缓存上返回
	// ErrNoValue。
	if cfg.Source != nil {
		src := cfg.Source
		localCfg.Source = func(ctx context.Context, _ string) (*T, error) {
			return src(ctx)
		}
	}
	if cfg.SourceWithTTL != nil {
		srcTTL := cfg.SourceWithTTL
		localCfg.SourceWithTTL = func(ctx context.Context, _ string) (*T, time.Duration, error) {
			return srcTTL(ctx)
		}
	}
	local := NewLocalCache[T](localCfg)
	return &SingleValue[T]{
		local: local,
		inner: NewSingleFlightCache[T](local),
	}
}

// Get 取值（GetOrLoad 语义，无 key 参数）：命中直接返回；未命中经
// Source 回源并回填（并发调用单飞合并——只有一个 goroutine 实际回源）。
//
// 无真值时返回 (nil, ErrNoValue)（errors.Is 可判定；见 ErrNoValue）——
// 调用方决定兜底值。回源错误原样返回。
func (sv *SingleValue[T]) Get(ctx context.Context) (*T, error) {
	v, err := sv.inner.GetOrLoad(ctx, singleValueKey)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrNoValue
	}
	return v, nil
}

// Set 直接写入值（使用配置的默认 TTL）。写入后 Get 命中该值直到过期。
// value 为 nil 时等价于写入「无值」——后续 Get 返回 ErrNoValue（单值
// 形态不区分「显式 nil 真值」与「无值」，nil 即无值）。
func (sv *SingleValue[T]) Set(ctx context.Context, value *T) error {
	return sv.inner.Set(ctx, singleValueKey, value)
}

// SetWithTTL 直接写入值并显式指定 TTL。
func (sv *SingleValue[T]) SetWithTTL(ctx context.Context, value *T, ttl time.Duration) error {
	return sv.inner.SetWithTTL(ctx, singleValueKey, value, ttl)
}

// Del 失效当前值。下一次 Get 重新回源（写后失效语义；在途回源不会把
// 失效前的旧值回填——#200 的世代保障同样生效）。
func (sv *SingleValue[T]) Del(ctx context.Context) error {
	return sv.inner.Del(ctx, singleValueKey)
}

// Refresh 刷新：失效当前值并立即重新回源（单飞保护）。
// 无真值时与 Get 同口径返回 ErrNoValue。
func (sv *SingleValue[T]) Refresh(ctx context.Context) (*T, error) {
	v, err := sv.inner.Refresh(ctx, singleValueKey)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrNoValue
	}
	return v, nil
}

// Stats 透出观测计数快照（语义同 LocalCache.Stats）。
func (sv *SingleValue[T]) Stats() Stats {
	return sv.local.Stats()
}

// Close 停止后台清扫 goroutine（未开 NoBackgroundSweep 时构造启动的那个）。
// 幂等，可安全重复调用。
func (sv *SingleValue[T]) Close(ctx context.Context) error {
	return sv.local.Close(ctx)
}

// 编译期能力契约：SingleValue 具备观测计数能力（pkg#76）。
var _ StatsProvider = (*SingleValue[int])(nil)
