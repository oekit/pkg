# AGENTS.md — oekit/pkg

> 微工具集 33 合 1（errors/bloomx/contextx/ctxscope/container/stringx/slicex/enumx/convert/encoding/jsonrepair/httpjson/mathx/pathx/pagination/ptrx/secretx/shellx/timex/cache/bufiox/strmap/singleflighttest/urlguard/webhook/cli/pack/procx/config/tarx/resumable/distlock/reflectx；v0.10.0 起 uidgen/validator 拆至 kitx）；errors（v0.12.0 起）New/As/Is/Unwrap 与 stdlib 同型透传——下游哨兵/As 判定单 import 即可，类型同一性红线可完整执行；cache（v0.4.0 起）含缓存接口 CacheAside + BatchGetter 批量能力（MGet）+ LocalCache/SingleFlightCache + cachetest 契约（Run 真实时间 + RunWithClock 假时钟确定性 TTL 边界）+ Checker；LocalCache 时钟可注入（LocalConfig.Now——TTL/NullTTL 全路径确定性测试，v0.13.0 起）；v0.16.0 起 LocalCache 可选形状——NoBackgroundSweep 关闭后台清扫 goroutine（嵌入式/短命进程，公开 Sweep 手动清扫口）、CloneOnRead 读命中返回副本（调用方可改写不污染缓存）、EvictPolicy 可选 LRU（默认 FIFO 不变）、GetEx 区分未命中与值为 nil（新增能力以可选接口 Sweeper/HitGetter 外置，不破坏其它后端）；v0.23.0 起补 Peek 非删除式读（可选接口 PeekGetter 外置——读到过期条目只算不命中、不删除它，stale-if-error 兜底需要那份过期真值留在表内；SingleFlightCache 包装 LocalCache 时曾因前置检查走 Get 惰性删除而静默废掉 StaleIfError，#72）；container（v0.9.0 起，收编原 collectionx）泛型容器 Set/Ring/Queue/SafeMap + Map（sync.Map 背书）/Slice（并发安全）；mathx/jsonrepair（v0.11.0）Unstable 单边向上抖动 Upward / 宽松解析 UnmarshalLenient / 抽对象与抽数组对称原语 ExtractObject·ExtractArray（v0.18.0 起——多段数组与散文含 '{' 的数组文本不再整段作废，取首个口径与对象面一致）；timex/stringx（v0.7.0 起）时间段边界 BeginningOf/EndOf 七对（分/时/日/周/月/季/年，周起始显式参数）+ trie 多模式替换器 Replacer（最长匹配）/ NewMask 敏感词遮罩；stringx Masker（v0.16.0 起）正则脱敏——按形态分类打码（形态标签进掩码供审计读）+ 保留首尾各 ≤4 字符定位线索且不可复原（保留位封顶不随长度变化），规则表由调用方完全提供；singleflighttest（v0.8.0 起）singleflight 共享契约测试套件（internal 与 concurrency 双侧引用）；urlguard（v0.15.0 起）出站 SSRF 防护原语——CheckURL 字面量校验（私网/loopback/链路本地含云元数据 169.254.169.254/CGNAT/ULA/组播保留、.internal·.local·localhost 域名、scheme 白名单、IPv4-mapped IPv6 归一）+ DialFunc 拨号层预解析校验与实际 RemoteAddr 端点核对闭合 TOCTOU（Happy-Eyeballs 覆盖），构造态豁免 WithAllowPrivate/WithAllowCIDRs/WithAllowDomains，与 httpx WithDialFunc 一行组合（httpx#11）；cli（v0.16.0 起）框架无关可单测的子命令分发原语——App/Command + 退出码 0/1/2 写死在包里、usage 按 Summary 自动生成、不读 os.Args 不调 os.Exit（输出流全由调用方给）；pack（v0.16.0 起）迁移包容器原语——zip + manifest（格式版本/条数/每文件 SHA-256）+ JSONL 流式读写 + Inspect 体检一次列全问题并区分包损坏与版本不认识；config/tarx（v0.42.0 起）启动期静态配置加载管线（yaml+env 覆盖+必填汇总校验+敏感字段脱敏 dump，#329）+ 目录 tar 传输三件套（同口径预扫 Scan/流式写出 Write/防御式解包 Extract 跳过并点名 Zip Slip 穿越条目 + sha256 manifest 自证，#326）；v0.43.0 起三处收口——cache.Checker 构造期 fail-fast（nil refresh panic/WithClock(nil) 回退默认，#356）+ 刷新跑在 WithoutCancel 上（leader 断开不再把取消缓存成 failTTL 传染等待者，瞬态 Canceled 不入缓存，#352）+ SingleFlightCache 按调用注入路径不再按 key 合并（source 不可比较，合并即串味/丢 TTL/强刷退化；Refresh 与 load 分键，#353）；container.Ring 逻辑序归基（32 位平台高频写入不再回绕出负下标 panic，#354）；cache/pack/urlguard 三个裸 Option 改名 CheckerOption/WriterOption/GuardOption（#355，禁裸 Option 仓规收尾）；v0.44.0 起三面补齐——shellx 词法事实面 Analyze（tokenize：普通词/重定向运算符+目标/动态词三类事实分离 + HasRedirect/HasDynamic/按控制符分段，前缀规则判据不再被「git status > /etc/passwd」整类绕过，判不了就报错不尽力解析，#358）+ procx 子进程内存上限原语（WithMemoryLimit/MemGroup：cgroup v2 memory.max 创建·设置·清理 + MemoryLimitCapable 问询面，兑现失败 fail closed 拒绝执行不静默不限、OOM-kill 与非零退出可辨、换出面一并封死、非 Linux 如实降级，#359）+ resumable 分片上传/断点续传会话面（新包：会话创建/分片登记/断点查询/完成/中止/Sweep 状态机，CAS 进度·合并抢占·双源对账·孤儿清扫四坑单点化，纯内存状态面 + ChunkStore 接口化存储后端，#361）；v0.45.0 起单源收敛十一单 + 双契约单源——distlock 分布式锁后端中立契约（Locker 接口两仓并集 + 哨兵错误集以 redisx 文案为基准 + WithLock 用 WithoutCancel 释放纪律，零依赖，redisx/etcdx 双后端消费同一份代码、errors.Is 跨包同一值，etcdx#123 治本侧）+ errors.PanicError panic 收编单源（error 身份不丢 + goroutine 栈 + panic 值类型）+ urlguard 内置拒绝哨兵与 CIDR 清单跨仓单源（ErrBlockedRange/ErrBlockedDomain/ErrExoticLiteral/BlockedCIDRSpecs，core#132 pkg 侧）；v0.46.0 起 reflectx 反射深拷贝单源（DeepCopyValue 指针环防护 + struct 先整块值拷贝保未导出字段、可 Set 引用字段逐一递归替换的强语义——core/config 与 pkg/pagination 两份平行实现收敛于此，core#130 治本侧）+ config.SetFromString/DurationType 字符串按 reflect.Kind 解析落位单源（core#129 治本侧）
> 当前版本：`v0.47.1` · module：`github.com/oekit/pkg` · org：[oekit = Open Easy Kit](https://oekit.github.io)

## 本仓包含
`bloomx/`、`bufiox/`、`cache/`、`cache/cachetest/`、`cli/`、`config/`、`container/`、`contextx/`、`convert/`、`ctxscope/`、`distlock/`、`encoding/`、`enumx/`、`errors/`、`httpjson/`、`internal/logkv/`、`internal/singleflight/`、`jsonrepair/`、`mathx/`、`pack/`、`pagination/`、`pathx/`、`ptrx/`、`procx/`、`reflectx/`、`resumable/`、`singleflighttest/`、`slicex/`、`secretx/`、`stringx/`、`strmap/`、`tarx/`、`timex/`、`urlguard/`、`webhook/`、`timex/cron/`

## 硬规则速览（全矩阵适用）

### 分层与依赖
- 依赖严格自底向上，禁止反向依赖；本仓的矩阵内依赖见 README「矩阵总览」表
- 微工具集统一在 `github.com/oekit/pkg`；本仓新增基础工具前先确认 pkg 里没有
- `pkg` 是**零第三方依赖仓**（go.mod 零 require，仅标准库，CI 强制）——需要三方能力的基础工具拆独立仓（先例：kitx）

### 错误处理
- 业务错误（需 BizCode/HTTPCode 双轨、可被 As/Is 判定）统一用
  `github.com/oekit/pkg/errors` 的 BizError（命名工厂 NewNotFoundError 等）
- 普通错误包装允许 stdlib：`fmt.Errorf("...: %w", err)` / `errors.New`
- BizError 载体同一进程只此一套：混入其他 BizError 实现会打断 As/Is 判定（Not Found 变 500）
- handler 层统一 `mw.Fail(ctx, c, err)` 出口；所有 error 禁止 `_` 忽略

### 日志
- 门面 `github.com/oekit/logx`：业务服务 `logx.WithService("svc")`；
  **SDK/基础包必须 `logx.Delegating("组件名")` 懒解析**（禁止 Default()/WithService 声明包级 logger）
- kv 结构化风格为默认；`Ctx*` 方法自动携带 trace_id；禁止 printf 风格 `logx.Infof` 作为主要手段
- 错误字段名统一用 `logx.ErrKey` 常量（值 "err"），禁止手写 "err"/"error" 字面量（两矩阵落同一 Loki/ES）
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
- Option 非法值总则：① 构造期非法值 fail-fast（panic 或返回 error，同仓内保持一致）；
  ② nil 参数 = 显式回退默认值，不算非法；③ 运行期不可恢复错误显式上抛，不静默

### 超时默认值
- 探测类 ≤10s、治理类 ≤60s、后台任务 ≤10min；跨区间取值须在包文档声明理由

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

