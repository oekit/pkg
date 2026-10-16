# pkg

> 微工具集 21 合 1——errors/bloomx/contextx/validator/stringx/convert/timex/uidgen/cache/bufiox/strmap 等基础工具包

> 🌐 项目主页：**https://oekit.github.io/** · 名称由来：**O**pen **E**asy **Kit**（开源 · 易用 · 工具集合）
> 🤖 AI Agent：本仓规范与硬规则见 [AGENTS.md](AGENTS.md)；机器可读索引见 [llms.txt](https://oekit.github.io/llms.txt)（接口签名以源码为准）。

## 安装

```bash
go get github.com/oekit/pkg@v0.5.0
```

## 这个仓库做什么

`pkg` 是 oekit 的**基础工具集**：20 个经过生产验证的小工具包合成一个 module，
覆盖错误处理、参数校验、字符串/切片/类型转换、时间与 cron、ID 生成、JSON 修复、
HTTP JSON 调用、分页、glob 匹配、进程内缓存、零拷贝缓冲 IO、GC 友好只读字典
等日常高频需求。

设计原则：

- **按包编译**：import 哪个子包用哪个，go.mod 只多一条 require，二进制零冗余
- **零依赖或近零依赖**：绝大多数子包只依赖标准库；重依赖（validator/v10）隔离在对应子包
- **生产验证**：全部代码源自 enjoydream 后端服务多年共用积累，带完整单元测试（21/21 包）

## 包清单

| 包 | 做什么 |
|---|---|
| `errors` | 错误链 `Wrap`/`Cause`（nil-safe）、`BizError`（业务码/HTTP 码双轨）、`Chain`/`In` 组合工具、sentinel 错误。整个矩阵的错误处理基座 |
| `bloomx` | 本地布隆过滤器：FNV-1a 双哈希、RWMutex 并发安全，缓存穿透防护/去重预检（非法入参归一默认值） |
| `contextx` | `ValueOnlyFrom` 只传值轻量 ctx：剥离 Done/Err/Deadline，把请求值交给后台任务而不继承取消/超时 |
| `validator` | go-playground/validator 封装，`ValidationError` 自动映射 400 |
| `stringx` | ID 生成、关键词包含匹配、缩进、英文复数化、`{key}` 模板替换 |
| `slicex` | `Partition` 二分组（保序）、GroupBy 等切片操作 |
| `enumx` | 枚举：确定性顺序的 All/Values/解析（map 遍历乱序问题的解法） |
| `convert` | 字符串/数字/布尔/切片的宽容转换（脏输入友好） |
| `encoding` | JSON 序列化助手（`UnmarshalUseNumber` 系列保留 json.Number，防 >2^53 float 精度丢失）+ FNV-1a/SHA-256 摘要与 Base64 |
| `jsonrepair` | LLM 输出等脏 JSON 的宽容修复链（截断/尾逗号/单引号），修复永不失败——异常时回退原文 |
| `httpjson` | 带 JSON 解析的 HTTP 调用助手 |
| `mathx` | 安全随机、平均值等统计助手 + `Unstable` 抖动值（缓存 TTL 打散防集中失效） |
| `pathx` | glob 路径匹配（`vendor/**`、`*.test`） |
| `pagination` | `PageRequest`/`PageResponse` 分页换算 |
| `ptrx` | 指针 To/From 与安全解引用 |
| `timex`（+`cron`） | 时间格式化/解析/时间戳 + cron 表达式 |
| `uidgen` | UUID/雪花/随机串 ID 生成 |
| `cache` | 刷新检查器：TTL 后台刷新、失败按 failTTL 降级、并发请求合并为一次刷新 |
| `bufiox` | 面向协议编解码的零拷贝缓冲 IO：读侧 Next/Peek 返回缓冲视图，写侧 Malloc/WriteBinary 攒批一次 Flush（`bytes` 与 `io.Reader/Writer` 双实现） |
| `strmap` | GC 友好的只读字符串 map：键打包 + int32 哈希表 + maphash，海量静态映射（header/字典表）零 GC 扫描开销 |

## 快速使用

```go
import (
    "github.com/oekit/pkg/errors"
    "github.com/oekit/pkg/jsonrepair"
    "github.com/oekit/pkg/stringx"
)

// 错误链：nil-safe 包装 + kv 关联字段，Is/As 语义保持
err := errors.Wrap(io.ErrUnexpectedEOF, "read config", "path", "/etc/app.yaml")

// 脏 JSON 修复（LLM 场景）
fixed := jsonrepair.Repair(`{"name": "oekit",) `)

fmt.Println(stringx.Pluralize(3, "module")) // 3 modules
```

```go
import (
    "github.com/oekit/pkg/bufiox"
    "github.com/oekit/pkg/strmap"
)

// 零拷贝解析：Peek 定长头 → Next 出负载视图，全程无字段级拷贝
r := bufiox.NewDefaultReader(conn)
hdr, _ := r.Peek(4)                  // 零拷贝视图
_ = r.Skip(4)
payload, _ := r.Next(int(binary.BigEndian.Uint32(hdr)))
defer r.Release(nil)                 // Release 后视图失效

// GC 友好只读字典：构建后只读，Get 无 map 开销
var canon = strmap.NewStr2StrFromMap(map[string]string{
    "content-type": "Content-Type",
})
name, ok := canon.Get("content-type") // "Content-Type", true
```

## 边界

- 需要并发原语（safe-go/重试/限流）→ [concurrency](https://github.com/oekit/concurrency)
- 需要日志 → [logx](https://github.com/oekit/logx)；需要数据库 → [gormx](https://github.com/oekit/gormx)
- 本仓不依赖矩阵内任何其他模块（validator→errors、httpjson→stringx、bloomx→encoding 三条内部边除外）


## 矩阵总览（21 活跃模块，自动生成）

| 模块 | 内容 | 矩阵内依赖 |
|---|---|---|
| [pkg](https://github.com/oekit/pkg) | 微工具集 21 合 1（errors/bloomx/contextx/collectionx/validator/stringx/slicex/enumx/convert/encoding/jsonrepair/httpjson/mathx/pathx/pagination/ptrx/timex/uidgen/cache/bufiox/strmap）；cache（v0.4.0 起）含缓存接口 CacheAside + LocalCache/SingleFlightCache + cachetest 契约 + Checker；collectionx（v0.5.0 起）泛型容器 Set/Ring/Queue/SafeMap | — |
| [concurrency](https://github.com/oekit/concurrency) | 并发与流控原语（async/gopool/group/singleflight/progress/syncx/ratelimit/retry）+ window 滑动窗口 / breaker 自适应熔断（规范哨兵 ErrCircuitOpen）/ shedding CPU 自适应降载 / batch 攒批执行器（满条数/满字节/窗口去重）/ timewheel 单级时间轮 | — |
| [logx](https://github.com/oekit/logx) | 统一日志门面（多后端可替换 + Sink + caller 归属）+ 优雅关停；桥接子包 hlog/hertzlog/kitexlog 为嵌套模块按需引入 | — |
| [core](https://github.com/oekit/core) | 应用基建件（governance 治理 schema / config 配置加载 + default/env/range/options tag 校验 / lifecycle 契约 / network IP 工具 / version） | [logx](https://github.com/oekit/logx) |
| [metrics](https://github.com/oekit/metrics) | Prometheus 门面 + 弹性指标族 | [core](https://github.com/oekit/core) |
| [tracing](https://github.com/oekit/tracing) | OpenTelemetry 追踪 + trace_id 真相源 | [logx](https://github.com/oekit/logx) |
| [registry](https://github.com/oekit/registry) | 注册发现核心 + Nacos 实现 + Nacos 配置源 + sdbridge | [core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx) |
| [data](https://github.com/oekit/data) | 数据访问门面——复刻 ekit data/ 能力面：cache/rediscache/crud/distlock/gormx/redisx/timertask/cachetest 八子包 re-export（类型同一，实现住 pkg 与各子仓）+ cached 缓存读写编排（写后失效/事务提交后失效/查询缓存版本号失效/GetByIDs/bloom 防穿透）+ twolevel 两级缓存与跨实例失效广播（v0.7.0 起本仓唯二非 re-export 实现）；all/ 聚合模块已废弃 retract | [concurrency](https://github.com/oekit/concurrency)、[gormx](https://github.com/oekit/gormx)、[logx](https://github.com/oekit/logx)、[pkg](https://github.com/oekit/pkg)、[redisx](https://github.com/oekit/redisx)、[timertask](https://github.com/oekit/timertask) |
| [gormx](https://github.com/oekit/gormx) | SQL/GORM 全家——连接池增强、框架无关 CRUD Service、仓储（原 oekit/gorm，v0.3.0 更名） | [logx](https://github.com/oekit/logx)、[pkg](https://github.com/oekit/pkg) |
| [redisx](https://github.com/oekit/redisx) | Redis 全家——客户端封装（根包）、分布式缓存 cache/、分布式锁 distlock、分布式限流 ratelimit（Lua 三态 + 故障本地兜底） | [concurrency](https://github.com/oekit/concurrency)、[logx](https://github.com/oekit/logx)、[pkg](https://github.com/oekit/pkg) |
| [httpx](https://github.com/oekit/httpx) | HTTP 客户端 + 负载均衡——服务发现/熔断（规范哨兵 ErrCircuitOpen）/重试/指标/OTel 透传/一致性哈希（权重感知虚拟副本） | [concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[registry](https://github.com/oekit/registry)、[tracing](https://github.com/oekit/tracing) |
| [network](https://github.com/oekit/network) | 轻量网络客户端——WebSocket/NATS/MQ 抽象、连接与流工具、Octopus 推送 | [concurrency](https://github.com/oekit/concurrency)、[logx](https://github.com/oekit/logx)、[tracing](https://github.com/oekit/tracing) |
| [timertask](https://github.com/oekit/timertask) | 分片定时任务（FNV-1a、心跳续约） | [concurrency](https://github.com/oekit/concurrency)、[logx](https://github.com/oekit/logx) |
| [middleware](https://github.com/oekit/middleware) | Hertz 自研标准中间件栈 + contrib 桥接 + hmw + SheddingMiddleware 自适应降载 | [concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[pkg](https://github.com/oekit/pkg)、[registry](https://github.com/oekit/registry)、[tracing](https://github.com/oekit/tracing) |
| [nns](https://github.com/oekit/nns) | NNS 服务治理客户端——provider 无关发现/配置/实例缓存 | [concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx)、[registry](https://github.com/oekit/registry) |
| [nns-endpoint](https://github.com/oekit/nns-endpoint) | NNS 端点工厂——per-service 熔断、换实例重试、gRPC 池 | [concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[httpx](https://github.com/oekit/httpx)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[nns](https://github.com/oekit/nns)、[registry](https://github.com/oekit/registry) |
| [bootstrap](https://github.com/oekit/bootstrap) | 应用生命周期引导——bootstrap.Run 骨架 + healthz | [concurrency](https://github.com/oekit/concurrency)、[logx](https://github.com/oekit/logx) |
| [hertz](https://github.com/oekit/hertz) | Hertz 运行时集成——日志桥接、生命周期、SSE/health/metrics、适配器、OTel、crudh/validatorh/versionh（原 handler 微库收编） | [bootstrap](https://github.com/oekit/bootstrap)、[concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[gormx](https://github.com/oekit/gormx)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[middleware](https://github.com/oekit/middleware)、[nns](https://github.com/oekit/nns)、[pkg](https://github.com/oekit/pkg)、[registry](https://github.com/oekit/registry)、[tracing](https://github.com/oekit/tracing) |
| [kitex](https://github.com/oekit/kitex) | Kitex 生态——日志桥接、治理热更、生命周期、BizError 桥接、kitexotel、istio（xDS 动态熔断，原 istio 微库收编） | [bootstrap](https://github.com/oekit/bootstrap)、[concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[nns](https://github.com/oekit/nns)、[pkg](https://github.com/oekit/pkg)、[registry](https://github.com/oekit/registry) |
| [workflow](https://github.com/oekit/workflow) | 组合式流程编排——串行/并行/异步/条件分支 + Hook AOP | [concurrency](https://github.com/oekit/concurrency)、[logx](https://github.com/oekit/logx) |
| [kitex-examples](https://github.com/oekit/kitex-examples) | Kitex echo 示例——client/server + IDL + 治理配置 | [bootstrap](https://github.com/oekit/bootstrap)、[concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[kitex](https://github.com/oekit/kitex)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[nns](https://github.com/oekit/nns)、[pkg](https://github.com/oekit/pkg)、[registry](https://github.com/oekit/registry)、[tracing](https://github.com/oekit/tracing) |
> 依赖严格自底向上；微库已合并为 pkg，旧微库仓已删除。

> 本仓在矩阵中的位置与依赖见上表「矩阵内依赖」列。历史沿革：源自 ekit 单体库
> （git.enjoye.top/enjoydream/ekit v0.34.0，冻结维护），2026-10 拆分为独立 module。

## License

MIT — 见 [LICENSE](LICENSE)
