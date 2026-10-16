# Changelog — oekit/pkg

由 ekit v0.34.0（git.enjoye.top/enjoydream/ekit，冻结维护）拆分而来。所有 notable 变更记录如下。


## [v0.5.0] — 2026-10-16

- feat(collectionx): 新增泛型容器微件——`Set`（集合代数并/交/差，非并发安全）、`Ring`
  （定长环形缓冲：溢出覆盖最旧且前移读指针保序 FIFO，保留最新 n 条，优于 go-zero 错位
  乱序语义）、`Queue`（无界阻塞队列：cond 实现、Close 幂等并唤醒全部等待者、关闭后 Put
  panic 对齐 channel 语义）、`SafeMap`（RWMutex 并发映射，GetOrSet 原子）；全部零依赖

## [v0.4.0] — 2026-10-16

- feat(cache): 收编数据访问缓存域（自 oekit/data/cache 整体下沉，data 转门面前置手术）——
  `Cache[T]`/`CacheAside[T]`/`SourceFunc[T]` 接口 + 缓存键构造（Key/HashKey/UserKey/
  TenantKey）+ `LocalCache`（TTL/容量/空值防穿透/LRU 逐出）+ `SingleFlightCache`
  （防击穿装饰器）+ `cachetest` 契约套件；实现瘦身保 pkg 零 oekit 内部依赖
  （logx→log/slog、async.GoSafe→包内 goSafe、singleflight fork 至 internal/singleflight）

## [v0.3.0] — 2026-10-16

- 新增 `bloomx`：本地布隆过滤器——`New(estElements, fpRate)` 按公式算 m/k（非法入参归一默认值，不返回 error）、Kirsch-Mitzenmacher 双哈希（FNV-1a 源 + finalizer 补低位雪崩）、`sync.RWMutex` 并发安全；缓存穿透防护/去重预检用
- 新增 `contextx`：`ValueOnlyFrom`——剥离 Done/Err/Deadline 的只传值轻量 ctx，把请求携带的值交给后台任务而不继承取消/超时
- `errors`：新增 `Chain`（顺序执行、首错即停）与 `In`（errors.Is 任一命中）
- `mathx`：新增 `Unstable` 抖动值（`NewUnstable` deviation 夹到 [0,1]，0 恒等返回；`Around`/`AroundDuration`/`AroundFloat64`，缓存 TTL 打散防集中失效）
- `encoding`：新增 `UnmarshalUseNumber`/`UnmarshalFromStringUseNumber`（数字解析为 json.Number，防 >2^53 float 精度丢失）

## [v0.2.0] — 2026-10-16

- feat: 吸收 cloudwego/gopkg——`bufiox` 零拷贝缓冲 IO（Reader/Writer 接口 + Bytes/Default 双实现，
  Next/Peek 零拷贝视图 + Malloc/WriteBinary 攒批 + net.Buffers writev）与 `strmap` GC 友好只读
  字符串 map（键打包 + int32 哈希表 + maphash，StrMap[V]/Str2Str）；均为 stdlib-only 独立重写
- 矩阵 DESC 同步：pkg 16 合 1 → 18 合 1

## [v0.1.3] — 2026-10-15

- go 版本降级至 1.24（矩阵分层终态）

## [v0.1.2] — 2026-10-15

- org 迁移收尾 patch（yi-nology → openekit → oekit，module path 统一 github.com/oekit/pkg）

## [v0.1.1] — 2026-10-15

- 16 微库归一为单 module（pkg 16 合 1）

## [v0.1.0] — 2026-10-15

- 首次发布：errors/validator/stringx/slicex/enumx/convert/encoding/jsonrepair/httpjson/mathx/pathx/pagination/ptrx/timex+cron/uidgen/cache
