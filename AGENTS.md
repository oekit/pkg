# AGENTS.md — oekit/pkg

> 微工具集 20 合 1（errors/bloomx/contextx/container/stringx/slicex/enumx/convert/encoding/jsonrepair/httpjson/mathx/pathx/pagination/ptrx/timex/cache/bufiox/strmap/singleflighttest；v0.10.0 起 uidgen/validator 拆至 kitx；v0.12.0 起 errors New/As/Is/Unwrap 与 stdlib 同型透传）；cache（v0.4.0 起）含缓存接口 CacheAside + BatchGetter 批量能力（MGet）+ LocalCache/SingleFlightCache + cachetest 契约 + Checker；container（v0.9.0 起，收编原 collectionx）泛型容器 Set/Ring/Queue/SafeMap + Map（sync.Map 背书）/Slice（并发安全）；timex/stringx（v0.7.0 起）时间段边界 BeginningOf/EndOf 七对（分/时/日/周/月/季/年，周起始显式参数）+ trie 多模式替换器 Replacer（最长匹配）/ NewMask 敏感词遮罩；singleflighttest（v0.8.0 起）singleflight 共享契约测试套件（internal 与 concurrency 双侧引用）
> 当前版本：`v0.13.0` · module：`github.com/oekit/pkg` · org：[oekit = Open Easy Kit](https://oekit.github.io)

## 本仓包含
`bloomx/`、`bufiox/`、`cache/`、`cache/cachetest/`、`container/`、`contextx/`、`convert/`、`encoding/`、`enumx/`、`errors/`、`httpjson/`、`internal/singleflight/`、`jsonrepair/`、`mathx/`、`pagination/`、`pathx/`、`ptrx/`、`singleflighttest/`、`slicex/`、`stringx/`、`strmap/`、`timex/`、`timex/cron/`

## 零第三方依赖（本仓硬规则，CI 强制）

- go.mod **永远不允许出现任何 require**：全部子包仅依赖 Go 标准库。
  golang.org/x、github.com、google.golang.org 等任何外部模块一律禁止——
  「看起来很稳」的 x/* 同样禁止（历史教训：dependabot 的版本升级曾把依赖链的
  Go 门槛连带抬到 1.26，pkg v0.9.0–v0.9.3 期间实际下限失守，见 CHANGELOG）
- 需要三方能力的基础工具：**拆独立仓，不进 pkg**（先例：validator/uidgen →
  [kitx](https://github.com/oekit/kitx)，2026-10 pkg v0.10.0 拆分）
- 强制点：CI `Zero Third-Party Deps` job（`go list -m all` 仅允许本模块一行）
  与本地 `make zero-dep`；`go mod tidy -diff` 门禁管依赖一致性，本规则管策略

## 硬规则速览（全矩阵适用）

### 分层与依赖
- 依赖严格自底向上，禁止反向依赖；本仓的矩阵内依赖见 README「矩阵总览」表
- 微工具集统一在 `github.com/oekit/pkg`；本仓新增基础工具前先确认 pkg 里没有

### 错误处理
- 一律使用 `github.com/oekit/pkg/errors`：`errors.Wrap(err, "ctx", kv...)`（nil-safe）、
  `errors.Cause`；业务错误用 BizError（BizCode 与 HTTPCode 双轨，命名工厂 NewNotFoundError 等）
- **类型同一性红线**：同一进程内只允许一套 errors 包。混用旧路径会把 BizError 的 As/Is
  判定打断（Not Found 变 500）
- handler 层统一 `mw.Fail(ctx, c, err)` 出口；所有 error 禁止 `_` 忽略

### 日志
- 门面 `github.com/oekit/logx`：业务服务 `logx.WithService("svc")`；
  **SDK/基础包必须 `logx.Delegating("组件名")` 懒解析**（禁止 Default()/WithService 声明包级 logger）
- kv 结构化风格为默认；`Ctx*` 方法自动携带 trace_id；禁止 printf 风格 `logx.Infof` 作为主要手段
- 后端经 `logx.SetDefault` 替换；`LOG_LEVEL` 环境变量统一生效

### 并发安全
- 禁止裸 `go func`：用 `github.com/oekit/concurrency/async` 的 GoSafe 系列（panic 被 recover）
- 共享状态 mutex / atomic 保护；包级全局可变状态用 `atomic.Pointer[T]`

### 生命周期
- 组件 Close 统一 `Close(ctx) error` 签名 + sync.Once 幂等
- 应用骨架用 `github.com/oekit/bootstrap（New + Run(fn)，v0.4.0 起无 Run(Config)）`；**userFn 返回即服务结束，
  长驻服务必须 `<-app.Context().Done()` 阻塞**

### Option 与命名
- 函数选项模式 `XxxOption`（禁止裸 `Option`，多包 co-import 冲突）
- 导出符号 PascalCase + godoc 注释；包文档放 doc.go

### 版本与发版
- SemVer；0.x 阶段 minor 允许 breaking；发版 = 打 annotated tag 推远端（CI 自动跑 lint+test）
- module 路径 = `github.com/oekit/<name>`；**改 module 路径必须同步**：
  全部 import、go.mod、README 安装行、logx caller 启发式（若涉及 logx）

### 文档
- 本仓 README 的「矩阵总览」表由脚本生成，结构变化后需重新生成
- 历史完整规范见 ekit 单体仓（git.enjoye.top/enjoydream/ekit，冻结维护）的 AGENTS.md 与 docs/

## 本仓范围

