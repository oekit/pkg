# AGENTS.md — oekit/pkg

> 微工具集 23 合 1（errors/bloomx/contextx/container/stringx/slicex/enumx/convert/encoding/jsonrepair/httpjson/mathx/pathx/pagination/ptrx/timex/cache/bufiox/strmap/singleflighttest/urlguard/cli/pack；v0.10.0 起 uidgen/validator 拆至 kitx）；errors（v0.12.0 起）New/As/Is/Unwrap 与 stdlib 同型透传——下游哨兵/As 判定单 import 即可，类型同一性红线可完整执行；cache（v0.4.0 起）含缓存接口 CacheAside + BatchGetter 批量能力（MGet）+ LocalCache/SingleFlightCache + cachetest 契约（Run 真实时间 + RunWithClock 假时钟确定性 TTL 边界）+ Checker；LocalCache 时钟可注入（LocalConfig.Now——TTL/NullTTL 全路径确定性测试，v0.13.0 起）；v0.16.0 起 LocalCache 可选形状——NoBackgroundSweep 关闭后台清扫 goroutine（嵌入式/短命进程，公开 Sweep 手动清扫口）、CloneOnRead 读命中返回副本（调用方可改写不污染缓存）、EvictPolicy 可选 LRU（默认 FIFO 不变）、GetEx 区分未命中与值为 nil（新增能力以可选接口 Sweeper/HitGetter 外置，不破坏其它后端）；container（v0.9.0 起，收编原 collectionx）泛型容器 Set/Ring/Queue/SafeMap + Map（sync.Map 背书）/Slice（并发安全）；mathx/jsonrepair（v0.11.0）Unstable 单边向上抖动 Upward / 宽松解析 UnmarshalLenient；timex/stringx（v0.7.0 起）时间段边界 BeginningOf/EndOf 七对（分/时/日/周/月/季/年，周起始显式参数）+ trie 多模式替换器 Replacer（最长匹配）/ NewMask 敏感词遮罩；stringx Masker（v0.16.0 起）正则脱敏——按形态分类打码（形态标签进掩码供审计读）+ 保留首尾各 ≤4 字符定位线索且不可复原（保留位封顶不随长度变化），规则表由调用方完全提供；singleflighttest（v0.8.0 起）singleflight 共享契约测试套件（internal 与 concurrency 双侧引用）；urlguard（v0.15.0 起）出站 SSRF 防护原语——CheckURL 字面量校验（私网/loopback/链路本地含云元数据 169.254.169.254/CGNAT/ULA/组播保留、.internal·.local·localhost 域名、scheme 白名单、IPv4-mapped IPv6 归一）+ DialFunc 拨号层预解析校验与实际 RemoteAddr 端点核对闭合 TOCTOU（Happy-Eyeballs 覆盖），构造态豁免 WithAllowPrivate/WithAllowCIDRs/WithAllowDomains，与 httpx WithDialFunc 一行组合（httpx#11）；cli（v0.16.0 起）框架无关可单测的子命令分发原语——App/Command + 退出码 0/1/2 写死在包里、usage 按 Summary 自动生成、不读 os.Args 不调 os.Exit（输出流全由调用方给）；pack（v0.16.0 起）迁移包容器原语——zip + manifest（格式版本/条数/每文件 SHA-256）+ JSONL 流式读写 + Inspect 体检一次列全问题并区分包损坏与版本不认识
> 当前版本：`v0.15.1` · module：`github.com/oekit/pkg` · org：[oekit = Open Easy Kit](https://oekit.github.io)

## 本仓包含
`bloomx/`、`bufiox/`、`cache/`、`cache/cachetest/`、`cli/`、`container/`、`contextx/`、`convert/`、`encoding/`、`enumx/`、`errors/`、`httpjson/`、`internal/singleflight/`、`jsonrepair/`、`mathx/`、`pack/`、`pagination/`、`pathx/`、`ptrx/`、`singleflighttest/`、`slicex/`、`stringx/`、`strmap/`、`timex/`、`urlguard/`、`timex/cron/`

## 硬规则速览（全矩阵适用）

### 分层与依赖
- 依赖严格自底向上，禁止反向依赖；本仓的矩阵内依赖见 README「矩阵总览」表
- 微工具集统一在 `github.com/oekit/pkg`；本仓新增基础工具前先确认 pkg 里没有
- `pkg` 是**零第三方依赖仓**（go.mod 零 require，仅标准库，CI 强制）——需要三方能力的基础工具拆独立仓（先例：kitx）

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
- 应用骨架用 `github.com/oekit/bootstrap/bootstrap.Run`；**userFn 返回即服务结束，
  长驻服务必须 `<-app.Context().Done()` 阻塞**

### Option 与命名
- 函数选项模式 `XxxOption`（禁止裸 `Option`，多包 co-import 冲突）
- 导出符号 PascalCase + godoc 注释；包文档放 doc.go

### 版本与发版
- SemVer；0.x 阶段 minor 允许 breaking
- **main 受分支保护，改动一律走 PR，管理员也不例外**（`enforce_admins=true`，
  直推会被 `GH006 Changes must be made through a pull request` 拒掉）
- 发版流程：建分支 → 提交 → 开 PR → CI（`Lint & Test / Lint` + `Lint & Test / Test`）
  绿 → 合入 main → **合入之后**再打 annotated tag 推远端
- tag 不走 PR（`protect-release-tags` ruleset 管的是不可删/不可改，不是审批）；
  tag 必须落在已合入 main 的 commit 上，否则 `go get` 拉到的是没进过 CI 的代码
- module 路径 = `github.com/oekit/<name>`；**改 module 路径必须同步**：
  全部 import、go.mod、README 安装行、logx caller 启发式（若涉及 logx）

### 文档
- 本仓 README 的「矩阵总览」表由脚本生成，结构变化后需重新生成
- 历史完整规范见 ekit 单体仓（git.enjoye.top/enjoydream/ekit，冻结维护）的 AGENTS.md 与 docs/

## 本仓范围

