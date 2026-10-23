package cache

import (
	"context"
	"time"
)

// Cache 是泛型缓存接口，面向接口编程。
// 只定义通用的缓存操作，不绑定具体实现或模式。
//
// # 实现契约（所有 Cache[T]/CacheAside[T] 实现必须遵守）
//
// 值语义（别名警告）：
//
//	实现**可以**返回缓存内部共享引用（零拷贝热路径，如 LocalCache），
//	调用方**禁止改写**返回的 *T 指向的值——改写会直接污染缓存条目，
//	并与并发读构成 data race；需要可变副本时调用方自行拷贝。
//	序列化型实现（如 Redis 后端）每次反序列化返回新副本，天然无别名。
//	同一接口允许两种别名语义，因此调用方一律按"只读"对待返回值。
//	Set/SetWithTTL 传入的 value 同理：实现不拷贝时调用方此后不得改写。
//	实现可以额外提供「读命中返回副本」的可选开关（LocalCache：
//	LocalConfig.CloneOnRead）——开启后读命中返回副本，调用方可改写
//	返回值而不污染缓存条目；未开启时按上面的共享引用口径。
//
// (nil, nil) 双关：
//
//	Get/GetOrLoad 返回 (nil, nil) 表示"数据不存在"——key miss 或命中
//	null marker（见 SourceFunc）二者之一；error 非 nil 时值无效。
//	miss 不是 error，不应记错误日志。
//
// 回填 best-effort：
//
//	GetOrLoad/Refresh 从 Source 取到数据后的回填写入失败**不阻断**返回
//	已取到的数据（实现自行记日志）；下次请求会重新回源。
//
// TTL 来源：
//
//   - Set：使用实现构造时配置的默认 TTL；
//   - SetWithTTL：显式传参；
//   - GetOrLoad 回填：默认 TTL；实现若支持动态回填 TTL（SourceFuncWithTTL，
//     pkg#11——如第三方 token 的 expires_in），按回源结果的 TTL 覆盖，<=0 回退默认。
//
// TTL<=0 的语义（0 = 不过期还是实现默认值）由各实现的配置文档定义
// （LocalCache：0 = 不过期；Redis 后端见各自 Config）。
//
// 生命周期：
//
//	接口不含 Close——生命周期属于具体实现的资源管理，不由接口约束。
//	需要停止后台资源的实现自行提供 Close，并遵循仓库硬规则签名
//	Close(ctx context.Context) error + sync.Once 幂等。
type Cache[T any] interface {
	// Get 获取缓存。
	// 返回 (nil, nil) 表示数据不存在（见包级"实现契约"）。
	Get(ctx context.Context, key string) (*T, error)

	// Set 设置缓存（使用实现构造时的默认 TTL）。
	Set(ctx context.Context, key string, value *T) error

	// SetWithTTL 设置缓存（显式 TTL）。
	SetWithTTL(ctx context.Context, key string, value *T, ttl time.Duration) error

	// Del 删除缓存。key 不存在时不得报错（幂等）。
	Del(ctx context.Context, key string) error

	// DelBatch 批量删除缓存。同 Del 幂等。
	DelBatch(ctx context.Context, keys ...string) error

	// Exists 检查缓存是否存在。
	// 对 null marker 返回 true（"缓存知道该 key 不存在"也是一种存在）。
	Exists(ctx context.Context, key string) (bool, error)
}

// SourceFunc 缓存未命中时的数据获取函数。
//
// 返回 (nil, nil) 表示数据不存在——实现会缓存 null marker 防穿透：
// 后续 GetOrLoad 命中 null marker 时直接返回 (nil, nil)，不再调用 Source；
// Exists 对 null marker 返回 true。
//
// null 标记的 TTL 语义由各实现的配置定义
// （LocalCache：NullTTL 0 = 默认 1 分钟，负值 = 显式禁用空值缓存）。
//
// Source 返回的 *T 所有权随值语义契约：实现回填后调用方与实现共享引用
// 或各持副本，取决于实现（同"实现契约"的值语义，调用方一律只读对待）。
type SourceFunc[T any] func(ctx context.Context, key string) (*T, error)

// SourceFuncWithTTL 携带动态回填 TTL 的数据获取函数（pkg#11）。
// ttl > 0 按其缓存；ttl <= 0 回退实现配置的默认 TTL；
// 返回 (nil, ttl, nil) 仍走 null marker 防穿透（此时 ttl 不生效，null marker
// 的 TTL 由实现配置治理——LocalCache 为 NullTTL）。
// 典型场景：第三方 token 的 expires_in、上游按内容给出的缓存窗口。
type SourceFuncWithTTL[T any] func(ctx context.Context, key string) (*T, time.Duration, error)

// StaleIfError 语义（pkg#11，由支持它的实现提供——LocalCache 经 LocalConfig
// 开启）：回源失败且存在已过期的历史真值时返回陈旧值（fail-open——防上游
// 抖动放大为拒绝服务）；null marker 不参与 stale，TTL=0 永不过期条目无
// stale 语义（它们不会过期）。命中时实现的 OnStale 回调（若配置）可观测。

// CacheAside 是 Cache-Aside 模式扩展接口。
// 在 Cache 基础上增加数据源回填、刷新等能力。
//
// GetOrLoad/Refresh 遵循包级"实现契约"：miss 非 error、回填 best-effort、
// null marker 防穿透。
//
// 新增后端（etcd/consul/...）必须实现本接口并通过
// github.com/oekit/data/cachetest 契约测试套件（Run 全绿）方可接入。
type CacheAside[T any] interface {
	Cache[T]

	// GetOrLoad 获取缓存，未命中时通过 Source 加载并回填。
	// Source 返回 (nil, nil) 时缓存 null marker（防穿透，TTL 见 SourceFunc）；
	// 后续调用命中 null marker 直接返回 (nil, nil)，不再调用 Source。
	GetOrLoad(ctx context.Context, key string) (*T, error)

	// Refresh 刷新缓存（删除后重新从 Source 加载）。
	Refresh(ctx context.Context, key string) (*T, error)
}

// BatchGetter 是批量读取的可选能力接口：实现方在单次网络往返（或单次
// 锁区间）内取回一批键，消费方经类型断言探测选用（缓存装饰器应向下
// 透传：内层具备批量能力时不得在外层退化为逐键）。
//
// 实现契约（与包级 (nil, nil) 双关一致的键缺席语义）：
//
//   - 返回 map 只含命中真值的键；key 不存在与命中 null marker 一律缺席。
//     缺席键的消费方应回退 GetOrLoad——真 miss 回源回填，marker 命中在
//     后端短路，两种情况都不会重复回源；
//   - MGet 是纯读：不回源、不回填、不产生 null marker；
//   - 批量中的反序列化失败按整批错误处理（与单键 Get 同口径）。
type BatchGetter[T any] interface {
	// MGet 批量获取。keys 为空时返回空 map。
	MGet(ctx context.Context, keys ...string) (map[string]*T, error)
}

// Sweeper 是可选能力接口：支持手动清扫过期条目（LocalCache 实现）。
//
// 供「不起后台清扫协程」的部署形态使用——短命命令行/宿主内嵌场景
// 由调用方在合适时机调用 Sweep 回收过期条目，避免为缓存常驻后台
// goroutine。实现须保证 Sweep 可重复调用（幂等：已回收的条目不再计数）。
type Sweeper interface {
	// Sweep 清扫过期条目，返回本次清除的条目数。
	Sweep(ctx context.Context) int
}

// HitGetter 是可选能力接口：取值时可区分「未命中」与「命中但值为 nil」。
//
// 既有 Cache[T].Get 以 (nil, nil) 同时表达「key 不存在」「命中 null
// marker」「命中但值为 nil」三种情况；实现本接口的方法额外给出命中
// 标志位（LocalCache：GetEx）。既有 Get 签名不变——本接口是可选能力，
// 不加入 Cache[T]（避免破坏其他后端实现）。
type HitGetter[T any] interface {
	// GetEx 获取缓存，第二个返回值为是否命中真值条目。
	// hit=false 表示未命中（不存在/已过期/null marker）；hit=true 时
	// 返回值可能为 nil（命中但值为 nil）。
	GetEx(ctx context.Context, key string) (*T, bool, error)
}

// PeekGetter 是可选能力接口：非删除式读取（LocalCache 实现：Peek）。
//
// 与 Get 的唯一差别是**读到过期条目时不删除它**：Get 走惰性删除路径
// （读路径顺带回收过期条目），而 stale-if-error 兜底（回源失败回退已过期
// 的历史真值）需要那份值仍留在表内。装饰器的前置检查若走 Get，会提前把
// 它删掉，使 StaleIfError 形同虚设（pkg#72）。
//
// 命中语义与 HitGetter 一致：hit=false 表示未命中（不存在/已过期/null
// marker）；hit=true 时返回值可能为 nil（命中但值为 nil）。
type PeekGetter[T any] interface {
	// Peek 非删除式获取缓存，第二个返回值为是否命中真值条目。
	// 命中真值条目时行为与 Get 一致（含读命中顺序提升与 CloneOnRead）；
	// 过期条目只算不命中，且**留在表内不被删除**。
	Peek(ctx context.Context, key string) (*T, bool, error)
}
