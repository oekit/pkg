# Changelog — oekit/pkg

由 ekit v0.34.0（git.enjoye.top/enjoydream/ekit，冻结维护）拆分而来。所有 notable 变更记录如下。

## [v0.13.0] — 2026-10-18

- **feat(cache)**: `LocalCache` 时钟注入——`LocalConfig` 新增 `Now func() time.Time`
  （缺省 time.Now），TTL/NullTTL 的全部过期判定（Get/GetOrLoad/Exists/MGet）与过期
  时刻写入（Set/SetWithTTL/null marker）及后台清扫一并路由注入时钟——测试注入假时钟
  即可确定性驱动过期与空值窗口，无 sleep 无 flake（[#9](https://github.com/oekit/pkg/issues/9)；
  与 memkv 的 s.now、concurrency budget 的 WithClock 口径拉齐）。
  注意：清扫 ticker 只定节奏、判定走注入时钟——假时钟下真实 ticker 时间戳不可比，
  直接比较会全量误删（实现时已规避）
- **feat(cachetest)**: 新增 `FakeClock` + `RunWithClock` 假时钟 TTL 套件（4 场景）：
  过期闭边界（恰到期仍命中、越过 1ns miss）、TTL=0 永不过期、null marker 过窗重源、
  重写刷新过期时刻——支持时钟注入的实现经 `ClockFactory` 接入，既有 `Run` 契约不动

## [v0.12.0] — 2026-10-18

- **feat(errors)**: `New`/`As`/`Is`/`Unwrap` 与 stdlib 同型透传——下游哨兵错误不再需要双 import，「类型同一性红线」可完整执行（[#8](https://github.com/oekit/pkg/issues/8)）
- build: 零第三方依赖硬规则四层强制——Makefile `zero-dep` 目标（`GOWORK=off go list -m all` 仅标准库 1 行）+ CI `Zero Third-Party Deps` job + AGENTS/README 明文规则 + main 分支保护

## [v0.11.0] — 2026-10-17

- **feat(mathx)**: `Unstable` 新增单边向上抖动 `Upward`/`UpwardDuration`/`UpwardFloat64`——结果 ∈ [v, v(1+dev)]。重试退避的「不得比基准更早」语义此前无法用对称抖动表达（会把退避压短成更密集的重试风暴）（[#6](https://github.com/oekit/pkg/issues/6)）
- **feat(jsonrepair)**: 新增宽松解析入口 `UnmarshalLenient`（返回 `partial` 截断标记）与 `UnmarshalWithOpts`/`WithAllowPartialArray`（XxxOption 形态）——尽力闭合截断的数组/对象：补悬空字符串引号（含 EOF 悬挂转义）、键位截断补 `:null`（`{"Host` 结尾）、键位串闭合后冒号缺失补 `:null`（`"c"` 结尾）、丢悬空逗号、悬空冒号补 null、按栈闭括号；顶层散串不救，纯垃圾仍报错。保守面零漂移（`UnmarshalWithOpts` 无选项 ≡ `Unmarshal`）。此前 `Repair` 只补括号，截断在字符串中段（`"HostName":"trunc` 结尾）无解（[#7](https://github.com/oekit/pkg/issues/7)）

## [v0.10.0] — 2026-10-17

- **BREAKING** refactor!: `uidgen`、`validator` 两包拆分至新仓 [kitx](https://github.com/oekit/kitx)——机械替换 import 即可迁移：`github.com/oekit/pkg/uidgen` → `github.com/oekit/kitx/uidgen`、`github.com/oekit/pkg/validator` → `github.com/oekit/kitx/validator`（包名不变）；行为契约不变（`ValidationError` 仍 Unwrap 为 BizError 400，uidgen 的 UUID 输出语义逐字节一致，且其 google/uuid 依赖已改标准库手写 v4）
- build: **第三方依赖归零**——go.mod 不再有任何 require，go.sum 清空（v0.9.3 拆出前有 validator/go-playground 全链 9 个模块）

## [v0.9.3] — 2026-10-17

- build: go 最低版本实际降至 1.24.0——锁定 go1.24 兼容版依赖：golang.org/x/sys v0.41.0、x/crypto v0.48.0、x/text v0.34.0、validator/v10 v10.30.1（各取声明 go≤1.24 的最高版）；语言面无变更

- 注：v0.9.2 因 go 行未实际降级（发布事故）已撤版删除，不作为可解析版本存在

## [v0.9.1] — 2026-10-17

- **fix(errors)**：`MultiError.Errors()` 改为返回内部切片的**防御性拷贝**
  （`slices.Clone`）——此前直接暴露内部引用，调用方改元素/append 会静默
  改动聚合错误状态，并绕开 `Append` 的 copy-on-write 保护前提；聚合错误量
  通常个位数，拷贝成本可忽略。`Unwrap() []error` 保持原样（进 stdlib
  errors.Is/As 只读遍历路径，无破坏面）（[#5](https://github.com/oekit/pkg/issues/5)）

## [v0.9.0] — 2026-10-17

- **BREAKING** feat(container): 新增泛型容器包 `container`，整体收编原 `collectionx`
  子包——`Set`/`Ring`/`Queue`/`SafeMap` 类型名与方法签名不变，仅 import 路径由
  `github.com/oekit/pkg/collectionx` 改为 `github.com/oekit/pkg/container`（机械替换即可）；
  `collectionx` 子包同步删除，容器唯一家为 `container`
- feat(container): 参考 go-kratos/kit container 新增两个并发安全类型——`Map[K,V]`
  （sync.Map 背书：Load/Store/LoadOrStore/Delete/Range/Len/Clear，读多写少、key 集合
  稳定时优于 SafeMap）、`Slice[T]`（RWMutex 背书：Append/Get/Set/Delete/Snapshot/Range/
  Clear，Range 在独立快照上进行不受并发写干扰）

## [v0.8.0] — 2026-10-17

- **fix(errors)**：`formatKV` 奇数个 kv 不再静默丢弃最后一个——保留并渲染为
  `!BADKEY=<value>`（对齐 `log/slog` 约定），漏传 value 的编程错误在输出里
  留下痕迹
  （[#3](https://github.com/oekit/pkg/issues/3)）
- **feat(singleflighttest)**：新增共享契约测试套件 `singleflighttest.Run`
  （先例 `cache/cachetest`）。固化 waiter 独立取消 / panic 转 error 且清 key /
  Forget 重入 / 同 key 去重 / 不同 key 并行 / error 透传；`pkg/internal/singleflight`
  与 `concurrency/singleflight` 双侧引用，语义漂移双红
  （[#4](https://github.com/oekit/pkg/issues/4)）

## [v0.7.0] — 2026-10-16

- feat(timex): 新增时间段边界函数——`BeginningOfX`/`EndOfX` 七对
  （分/时/日/周/月/季/年），纯函数保持时区不变；`EndOfX` 取"下一周期
  起点 - 1ns"，`[Beginning, End]` 恰好覆盖整周期不与相邻重叠；边界按
  墙钟 `time.Date` 重建（规避 `Truncate` 在非整小时偏移时区的 UTC 对
  齐陷阱），月末/季末以月初为基准走 `AddDate`（规避大小月与闰月归一
  化陷阱，跨夏令时日按墙钟日界）；`BeginningOfWeek`/`EndOfWeek` 周起
  始日显式传参（无全局状态）
- feat(stringx): 新增 trie 多模式替换器——`NewReplacer(patterns, repls)`
  rune 级扫描、同起点恒取最长匹配、命中后整体跳过不回溯；pattern 空
  串/非法 UTF-8 显式报错；`Replace` 按 rune 边界迭代，未命中 rune 原
  字节保留（非法 UTF-8 文本不被改写为 U+FFFD）；构造后只读可并发；
  另附 `NewMask(words, mask)` 敏感词遮罩薄壳（等 rune 数逐字遮罩）

## [v0.6.0] — 2026-10-16

- feat(cache): 新增 `BatchGetter[T]` 批量读取可选能力接口——`MGet` 单次网络
  往返（或单次锁区间）取回一批键；缺席语义与 (nil, nil) 双关一致（不存在
  与 null marker 一律缺席，缺席键由消费方回退 GetOrLoad 享受 marker 短路）；
  纯读不回源不回填不产生 marker；`LocalCache` 单读锁批量实现、
  `SingleFlightCache` 委托内层批量能力（内层无批量时退化为逐键）；装饰器
  透传规则与消费方探测用法固化在接口 godoc

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

- org 命名与 module path 收敛至 oekit（github.com/oekit/pkg）

## [v0.1.0] — 2026-10-15

自 ekit v0.34.0（git.enjoye.top/enjoydream/ekit，冻结维护）拆分而来的初始公开版本，完成拆分稳定化：模块路径统一为 `github.com/oekit/pkg`，多轮拆分/合并收敛至当前模块边界，组织基建（共享 CI workflow、golangci v2、Dependabot）就位。