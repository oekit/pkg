// Package cache 提供进程内缓存原语与泛型缓存门面（stdlib-only，零 oekit
// 内部依赖）。
//
// 组成：
//   - Cache[T] / CacheAside[T] / SourceFunc[T] 接口：实现契约统一收拢在
//     interface.go 的接口 godoc（值语义、(nil, nil) 双关、回填 best-effort、
//     TTL 来源、生命周期、null marker）
//   - LocalCache[T]：基于内存的本地缓存（MaxSize、TTL、空值防穿透；
//     淘汰策略 EvictFIFO（缺省，写序）/ EvictLRU（读命中也提升顺序）；
//     NoBackgroundSweep 关闭后台清扫协程（改由调用方 Sweep 手动回收）；
//     CloneOnRead 让读命中返回副本；GetEx 区分「不存在」与「有值但值为 nil」）
//   - SingleFlightCache[T]：装饰器，并发回源合并防击穿（内部 singleflight
//     fork 于 concurrency/singleflight，见 internal/singleflight）
//   - Checker：进程内检查缓存原语（TTL 缓存 + 刷新合并 + 失败短 TTL）
//   - 缓存键构造：Key / HashKey / UserKey / TenantKey
//   - cachetest 子包：CacheAside 实现的契约测试套件
//
// 分布式后端 RedisCache[T] 在独立模块 github.com/oekit/redisx（import
// github.com/oekit/redisx/cache）——本包零 Redis 编译依赖，用 LocalCache
// 的服务无需引入 Redis SDK。
//
// 新增后端（etcd/consul/...）实现 CacheAside[T] 后必须通过
// github.com/oekit/pkg/cache/cachetest 契约测试套件（Run 全绿，含 -race）
// 方可接入。
package cache
