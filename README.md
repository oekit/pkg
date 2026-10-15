# pkg

> oekit 微工具集——16 个零依赖（或近零依赖）的基础工具包，合为一个 module 按需 import。
> 🤖 AI Agent：本仓规范与硬规则见 [AGENTS.md](AGENTS.md)；机器可读索引见 [llms.txt](https://oekit.github.io/llms.txt)。

[errors](https://github.com/oekit/pkg/tree/main/errors) · [validator](https://github.com/oekit/pkg/tree/main/validator) · [stringx](https://github.com/oekit/pkg/tree/main/stringx) · [slicex](https://github.com/oekit/pkg/tree/main/slicex) · [enumx](https://github.com/oekit/pkg/tree/main/enumx) · [convert](https://github.com/oekit/pkg/tree/main/convert) · [encoding](https://github.com/oekit/pkg/tree/main/encoding) · [jsonrepair](https://github.com/oekit/pkg/tree/main/jsonrepair) · [httpjson](https://github.com/oekit/pkg/tree/main/httpjson) · [mathx](https://github.com/oekit/pkg/tree/main/mathx) · [pathx](https://github.com/oekit/pkg/tree/main/pathx) · [pagination](https://github.com/oekit/pkg/tree/main/pagination) · [ptrx](https://github.com/oekit/pkg/tree/main/ptrx) · [timex](https://github.com/oekit/pkg/tree/main/timex) · [uidgen](https://github.com/oekit/pkg/tree/main/uidgen) · [cache](https://github.com/oekit/pkg/tree/main/cache)

## 安装

```bash
go get github.com/oekit/pkg@latest
```

```go
import (
    "github.com/oekit/pkg/errors"
    "github.com/oekit/pkg/stringx"
)
```

> Go 按包编译：import 一个子包不会引入其他子包的代码；module require 只有一条。

## 能做什么

- **错误处理**：`Wrap`/`Cause` 链、`BizError` 业务码/HTTP 码双轨、命名工厂
- **参数校验**：go-playground 封装，`ValidationError` 自动映射 400
- **文本与集合**：字符串模板/复数化/ID、切片二分组、枚举解析、宽容类型转换
- **JSON**：宽容修复链（截断/尾逗号/单引号都能救）、带重试的 HTTP+JSON
- **基础设施件**：时间/cron、雪花与随机 ID、分页、指针、glob 匹配、刷新缓存

## 使用示例

### 错误链与业务码
```go
import "github.com/oekit/pkg/errors"

err := errors.Wrap(sql.ErrNoRows, "query user", "user_id", uid)
biz := errors.NewNotFoundError("order not found") // HTTPCode 自动推导 404
_ = errors.WrapBizError(40010, "库存不足", cause)   // 带因业务错误
```
### 模板与 ID
```go
import (
    "github.com/oekit/pkg/stringx"
    "github.com/oekit/pkg/uidgen"
)

msg := stringx.FormatTemplate("user {uid} not in {room}", map[string]any{"uid": 42, "room": "r1"})
id := uidgen.RandomHex(16)
```
### JSON 修复（LLM 输出救星）
```go
import "github.com/oekit/pkg/jsonrepair"

var v map[string]any
err := jsonrepair.Unmarshal(brokenJSON, &v) // 截断/尾逗号/单引号都能救；修不好回退原文
```
### 刷新缓存
```go
import "github.com/oekit/pkg/cache"

ck := cache.NewChecker(refreshFn, time.Minute, 10*time.Second) // TTL/失败TTL/并发合并
_ = ck.Check(ctx)

> ⚠️ **红线**：同一进程只允许一套 errors 包（`oekit/pkg/errors`），混用旧路径会让 BizError 判定断裂。
## 包清单

| 包 | 说明 | 三方依赖 |
|---|---|---|
| `errors` | Wrap/Cause 链、BizError（业务码/HTTP 码双轨）、sentinel | — |
| `validator` | go-playground 封装，ValidationError→BizError(400) | validator/v10 |
| `stringx` | ID 生成、关键词匹配、缩进、复数化、模板替换 | — |
| `slicex` | Partition 二分组、GroupBy | — |
| `enumx` | 确定性顺序 All/Values | — |
| `convert` | 字符串/数字/布尔/切片宽容转换 | — |
| `encoding` | JSON/msgpack 序列化助手 | msgpack |
| `jsonrepair` | 宽容修复链（永不失败，回退原文） | — |
| `httpjson` | DoJSON 带重试语义与 Retry-After | — |
| `mathx` | 安全随机、评测统计 | — |
| `pathx` | glob 匹配（`vendor/**`） | — |
| `pagination` | PageRequest/PageResponse | — |
| `ptrx` | To/From/安全解引用 | — |
| `timex`（+`cron`） | 时间格式化/解析 + cron 表达式 | — |
| `uidgen` | UUID/雪花/随机串 | uuid |
| `cache` | 刷新检查器（TTL/failTTL/inflight 合并） | — |

内部互引仅两条：`validator → errors`、`httpjson → stringx`，其余全部只依赖标准库。

## 历史

原为 16 个独立微库（oekit/errors、oekit/stringx…），2026-10 合并为本 module——
旧仓已删除；已发布的旧版本在 Go module proxy 缓存中仍可解析。
import 路径迁移：`github.com/oekit/<pkg>` → `github.com/oekit/pkg/<pkg>`。

## License

MIT — 见 [LICENSE](LICENSE)

## 矩阵总览（23 活跃模块，自动生成）

| 模块 | 内容 | 矩阵内依赖 |
|---|---|---|
| [pkg](https://github.com/oekit/pkg) | 微工具集 16 合 1（errors/validator/stringx/slicex/enumx/convert/encoding/jsonrepair/httpjson/mathx/pathx/pagination/ptrx/timex/uidgen/cache） | — |
| [concurrency](https://github.com/oekit/concurrency) | 并发与流控原语（async/group/singleflight/progress/syncx/ratelimit/retry） | — |
| [logx](https://github.com/oekit/logx) | 统一日志门面（多后端可替换 + Sink + caller 归属）+ 优雅关停 | — |
| [core](https://github.com/oekit/core) | 应用基建件（governance 治理 schema / lifecycle 契约 / network IP 工具 / version） | — |
| [istio](https://github.com/oekit/istio) | Istio Proxyless——Kitex xDS 动态熔断 | — |
| [metrics](https://github.com/oekit/metrics) | Prometheus 门面 + 弹性指标族 | [core](https://github.com/oekit/core) |
| [tracing](https://github.com/oekit/tracing) | OpenTelemetry 追踪 + trace_id 真相源 | [logx](https://github.com/oekit/logx) |
| [registry](https://github.com/oekit/registry) | 注册发现核心 + Nacos 实现 + 配置加载 + sdbridge | [core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx) |
| [data](https://github.com/oekit/data) | 缓存检查器（TTL/failTTL/inflight 合并）+ LocalCache | [concurrency](https://github.com/oekit/concurrency)、[logx](https://github.com/oekit/logx) |
| [gorm](https://github.com/oekit/gorm) | SQL/GORM 全家——连接池增强、框架无关 CRUD Service、仓储 | [logx](https://github.com/oekit/logx)、[pkg](https://github.com/oekit/pkg) |
| [redis](https://github.com/oekit/redis) | Redis 全家——客户端封装、缓存后端、分布式锁 | [concurrency](https://github.com/oekit/concurrency)、[data](https://github.com/oekit/data)、[logx](https://github.com/oekit/logx) |
| [httpx](https://github.com/oekit/httpx) | HTTP 客户端 + 负载均衡——服务发现/熔断/重试/指标/OTel 透传 | [concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[registry](https://github.com/oekit/registry)、[tracing](https://github.com/oekit/tracing) |
| [network](https://github.com/oekit/network) | 轻量网络客户端——WebSocket/NATS/MQ 抽象、连接与流工具、Octopus 推送 | [concurrency](https://github.com/oekit/concurrency)、[logx](https://github.com/oekit/logx)、[tracing](https://github.com/oekit/tracing) |
| [timertask](https://github.com/oekit/timertask) | 分片定时任务（FNV-1a、心跳续约） | [concurrency](https://github.com/oekit/concurrency)、[logx](https://github.com/oekit/logx) |
| [middleware](https://github.com/oekit/middleware) | Hertz 自研标准中间件栈 + contrib 桥接 + hmw | [concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[pkg](https://github.com/oekit/pkg)、[registry](https://github.com/oekit/registry)、[tracing](https://github.com/oekit/tracing) |
| [nns](https://github.com/oekit/nns) | NNS 服务治理客户端——provider 无关发现/配置/实例缓存 | [concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx)、[registry](https://github.com/oekit/registry) |
| [nns-endpoint](https://github.com/oekit/nns-endpoint) | NNS 端点工厂——per-service 熔断、换实例重试、gRPC 池 | [concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[httpx](https://github.com/oekit/httpx)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[nns](https://github.com/oekit/nns)、[registry](https://github.com/oekit/registry) |
| [bootstrap](https://github.com/oekit/bootstrap) | 应用生命周期引导——bootstrap.Run 骨架 + healthz | [concurrency](https://github.com/oekit/concurrency)、[logx](https://github.com/oekit/logx) |
| [handler](https://github.com/oekit/handler) | HTTP handler 层——通用 CRUD Handler、参数绑定、版本端点 | [concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[gorm](https://github.com/oekit/gorm)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[middleware](https://github.com/oekit/middleware)、[pkg](https://github.com/oekit/pkg)、[registry](https://github.com/oekit/registry)、[tracing](https://github.com/oekit/tracing) |
| [hertz](https://github.com/oekit/hertz) | Hertz 运行时集成——日志桥接、生命周期、SSE/health/metrics、适配器、OTel | [bootstrap](https://github.com/oekit/bootstrap)、[concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[nns](https://github.com/oekit/nns)、[registry](https://github.com/oekit/registry)、[tracing](https://github.com/oekit/tracing) |
| [kitex](https://github.com/oekit/kitex) | Kitex 生态——日志桥接、治理热更、生命周期、BizError 桥接、kitexotel | [bootstrap](https://github.com/oekit/bootstrap)、[concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[nns](https://github.com/oekit/nns)、[pkg](https://github.com/oekit/pkg)、[registry](https://github.com/oekit/registry) |
| [workflow](https://github.com/oekit/workflow) | 组合式流程编排——串行/并行/异步/条件分支 + Hook AOP | [concurrency](https://github.com/oekit/concurrency)、[logx](https://github.com/oekit/logx) |
| [kitex-examples](https://github.com/oekit/kitex-examples) | Kitex echo 示例——client/server + IDL + 治理配置 | [bootstrap](https://github.com/oekit/bootstrap)、[concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[kitex](https://github.com/oekit/kitex)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[nns](https://github.com/oekit/nns)、[pkg](https://github.com/oekit/pkg)、[registry](https://github.com/oekit/registry)、[tracing](https://github.com/oekit/tracing) |