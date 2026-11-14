# pkg CHANGELOG

> 本文件由 `matrix-tools/scripts/gen_changelogs.py` 从 git 历史生成，按 tag 切段、取每段内的 conventional commit 渲染。

## [v0.49.0] — 2026-11-15

- fix: 审计单批量收口——cron 契约四单：ConstantDelay/Delay/RandomSchedule 直构兜底「严格晚于入参」+构造器拒非正延迟(#468,#469)、RoundDuration 负精度 panic+整数算术防 float64 丢精度(#470)、WrapSchedule 补 NewWrapSchedule(#471)；timex：NDateBefore 负 n 拒绝(#472)、ConvertTimezone 对齐 Date2Stamp 的 DST 前跳锚定口径(#473)、三函数补本机时区声明(#493)、SpecSchedule 零值 Location 回落 time.Local(#494)；container：Queue 压缩分支/Ring.Take 补清零(#474,#475)、Map/Slice 零值声明与实现对齐(#476)、Set.Clone 措辞(#492)；strmap 包文档拉链式措辞(#477)；config：yaml \\u 代理对合并+孤立拒绝(#478)、required 切片 len==0 同裁决(#479)、yaml float 补 OverflowFloat(#480)、parseEnvTag default 吞段显式报错(#488)；mathx Unstable 零值声明(#490)；slicex ToMap 语义声明+ToMapStrict(#491)；procx Windows keep/literal 归一(#489)；httpjson 新增 ClearRetryAfter 按尝试作废原语(#496)。feat: 下沉提案七单——auditchain append-only 审计哈希链(#481)、webhook Forwarder 游标外发+RFC5424(#482)、sse 游标回放应答器(#483)、canon 策略面规范形指纹(#484)、webhook IM 载荷构造器（企微/钉钉/飞书）(#485)、licensx Ed25519 license 签发/验签(#486)、tokenx HMAC 签名能力令牌(#487)

关联问题单：pkg#468、pkg#469、pkg#470、pkg#471、pkg#472、pkg#473、pkg#474、pkg#475、pkg#476、pkg#477、pkg#478、pkg#479、pkg#480、pkg#488、pkg#489、pkg#490、pkg#491、pkg#492、pkg#493、pkg#494、pkg#496、pkg#481、pkg#482、pkg#483、pkg#484、pkg#485、pkg#486、pkg#487。

## [v0.48.0] — 2026-11-13

- **BREAKING**：删除 errors.Errorf Deprecated 兼容层（#401/#405 恢复件的终局退役）。下游迁移：普通格式化包装 fmt.Errorf("...: %w", err)；kv 结构化上下文 errors.Wrap。跨 org 消费方（go-git-platform/go-gongfeng/guardx）已同列车迁移。wrap.go/doc.go 注释同步去 Errorf 化。

## [v0.47.1] — 2026-11-13

- fix(timex): EndOfMinute/EndOfHour 改「下一周期起点 −1ns」——非整小时 DST 位移时区（Lord Howe/Macquarie 30 分钟回拨）不再与相邻周期重叠（#418）。头注释不变量新增第三条（不重叠），适用域扩至全部 DST 时区。

关联问题单：pkg#418。

## [v0.47.0] — 2026-11-13

- 29 单修复批（#383-#408 清单 18 + 批3 九单 + #412/#413）：errors.Errorf 兼容恢复（#405/#401，stdlib 缺失语义原样+Deprecated 迁移指引）、resumable/bufiox 溢出双修（#406/#407/#408）、urlguard 写面哨兵（#390）、jsonrepair 导出哨兵（#384）、pack 构造期校验（#389）、timex int64 2038（#388）、mathx 随机源统一 math/rand/v2（#386）、bloomx 热路径零分配（#385）、WrapSchedule 零值哨兵（#383）、singleflighttest 元测试三件套（#394）等。

## [v0.46.0] — 2026-11-12

- - feat(config): 导出 SetFromString/DurationType——「字符串按 reflect.Kind 解析落位」的全矩阵唯一原语（core#129 治本侧：oekit/core/config 的 setScalarFromString 平行实现改引后删除）；内部实现零改动、行为零变化
- feat(reflectx): 新增反射原语包——DeepCopyValue/DeepCopy 深拷贝唯一实现（core#102 指针环防护 + core#84 struct 先整块拷贝保未导出字段、可 Set 引用字段逐一递归替换的强语义），core/config 与 pkg/pagination 两份平行实现收敛于此（core#130 治本侧）
- refactor(pagination): deepCopyItem 改走 reflectx.DeepCopy，删除本地 deepCopyValue/deepCopyVisited/structCopyPlan；含未导出字段的 struct 其导出引用字段由共享变深拷贝（快照隔离只紧不松）；nil 接口元素不再 panic

## [v0.45.0] — 2026-11-12

- 单源收敛十一单 + 双契约单源新增：convert 两套 any→int64/bool 开关收敛 OK 变体单源（#255 式漂移治本，#371）+ AppendStrings 改内建 append（#379）；cache 惰性删除块单源 lazyDeleteExpired + Get/GetEx 收敛 getHit 内核（#373）、世代写入路径 putEntryLocked 单源并按职责拆 local_load/local_sweep（#374）；cron parseRange normalize 钩子收敛三份范围+步长逻辑、splitStep 唯一步长切分（#375）；webhook 构造期校验收敛 mustHMACAlgo/mustSecret（#376）；timex ParseFirst 单行委托 ParseFirstInLocation（#377）；urlguard 拨号面两处 %w 补链 + 内置拒绝导出哨兵 ErrBlockedRange/ErrBlockedDomain/ErrExoticLiteral + 缺省 Guard 缓存（#380）+ CIDR 清单导出跨仓单源 BlockedCIDRSpecs/IsBlockedIP/ParseCIDRList（core#132 pkg 侧）；jsonrepair 终态错误双 %w 保留两路链（#382）；errors 新增 panic 收编单源 PanicError/Recovered（error 身份不丢+goroutine 栈+panic 值类型，cache/singleflight 转换点改用，全矩阵 wave 2 消费，#381）；pagination 快照深拷贝同步 core#102 指针环防护（core#130 pkg 侧）；新增 distlock 后端中立契约包（Locker 接口两仓并集 + 哨兵集以 redisx 文案为基准 + WithLock WithoutCancel 释放纪律，etcdx#123 治本侧，redisx/etcdx 波次消费）；Go 地板抬至 1.26.0。**BREAKING**：删除零引用的 errors.Errorf（#378）——普通错误包装统一 stdlib fmt.Errorf("%w")，本包只留 stdlib 没有的能力（Wrap kv/Recovered/MultiError 等）

关联问题单：pkg#371、pkg#373、pkg#374、pkg#375、pkg#376、pkg#377、pkg#378、pkg#379、pkg#380、pkg#381、pkg#382。

## [v0.44.0] — 2026-11-11

- 三件套新原语：shellx.Analyze 词法事实面（重定向/命令替换/普通参数可辨，#358）；procx cgroup v2 memory.max 内存限额（fail-closed + OOM 信号可辨 + swap 封死，#359）；resumable 分片上传/断点续传会话面（CAS 进度 + 断点双源对账 + 孤儿清扫，#361）

关联问题单：pkg#358、pkg#359、pkg#361。

## [v0.43.0] — 2026-11-11

- 五单审计收口：Checker 构造期 fail-fast + 刷新 WithoutCancel 不传染（#356 #352）；SingleFlightCache 注入路径不合并、Refresh 分键（#353）；Ring 逻辑序归基防 32 位回绕（#354）；cache/pack/urlguard 裸 Option 改名 CheckerOption/WriterOption/GuardOption（#355）

关联问题单：pkg#356、pkg#352、pkg#353、pkg#354、pkg#355。

## [v0.42.0] — 2026-11-10

- - fix(httpjson): parseRetryAfter 乘法前判上界——超大 Retry-After 溢出成毫秒级穿透 5 分钟可信闸的口子堵死（#343）
- - feat(config): 静态配置加载管线——yaml(自研子集 parser,零三方依赖)→env 覆盖→缺省→MultiError 汇总校验 + Redact 路径脱敏 fail-loud（#329）
- - feat(tarx): 防御式解包三件套——Scan/Write 奇偶同口径、Extract 穿越/设备类拒收、WithManifest sha256 清单+VerifyManifest 指名篡改（#326）
- - feat(webhook): 发端签名 helper 族——SignHMAC/SignTimestampHMAC 与验签器同 HMACOption 单源（#325）；HMACSigner/事件信封/NewEventID 幂等 ID 与五平台发↔收配对表（#328）
- - test(stringx): TruncRunes/TruncBytes 永不产出非法 UTF-8 的穷举证据（#327 证据收口——能力自 v0.34.0 已在）

关联问题单：pkg#343、pkg#329、pkg#326、pkg#325、pkg#328、pkg#327。

## [v0.41.1] — 2026-11-10

- - fix(mathx): Round/Floor/DivisionN 溢出防护补查 x*times 乘积中间量，不再静默产出 +Inf（#317）
- - fix(pagination): 快照改按引用结构深拷贝，指针元素缓冲复用不再误判伪翻页丢页（#318）
- - fix(httpjson): 304 重放不再把空缓存体当驱逐，204 与 HEAD 端点不再每轮双请求（#319）
- - fix(procx): Run 对残留 stdio 孙进程不再空等 6 秒并把成功报成错误（ErrWaitDelay 收口边界入文档）（#320）
- - fix(jsonrepair): 错配闭合符改写为栈顶期待符号+无主闭合符丢弃，Repair 收敛（#321）
- - fix(urlguard): allowDomains 前导点条目按域名语义剥点生效，豁免配置不再静默落空（#322）
- - fix(jsonrepair): UnmarshalLenient 的 partial 标记挂 EOF 截断迹象，截断被修复形态如实为 true（#323）

关联问题单：pkg#317、pkg#318、pkg#319、pkg#320、pkg#321、pkg#322、pkg#323。

## [v0.41.0] — 2026-11-09

- feat(procx): ChildEnv 最小环境清洗 + WithOutputLimit 输出封顶——#294 三件套收口（#314）

关联问题单：pkg#314。

## [v0.40.0] — 2026-11-09

- feat(urlpath): URL path 低基数模板化原语——ID 段折叠控制 metrics 标签基数（#291）
- fix(stringx): FirstN 空串 n==0 矩阵语义 + JoinWith 文档声明（#299 #300）
- fix(jsonrepair): ParseLenient 裸解析优先——schema 不再无条件 Normalize（#301）
- fix(httpjson): Retry-After 仅在 429 失败采信（#302）

关联问题单：pkg#291、pkg#299、pkg#300、pkg#301、pkg#302。

## [v0.39.0] — 2026-11-09

### 修复(procx)：树终止语义补全——跨组逃逸清扫 + windows Job Object + 仓规对齐（pkg#284 追平轮）

v0.38.2 落地的首版与 issue 验收判据/包文档承诺有三处落差，逐条补齐：
1. unix 跨组逃逸清扫：守护化/setsid 换组的真孙进程组击杀够不到
   （kill(-pgid) 只覆盖未换组后代），而「包装器常驻、工作进程 detach」
   恰是 #284 的动机场景（issue 判据 1 的 pgrep -P 递归口径要求它死）。
   按父链快照清扫（linux /proc、darwin ps(1)、其余 unix 降级纯组击杀并
   如实声明），TERM/KILL 两档都扫，KILL 档重枚举兜住宽限期内新 spawn 的
   逃逸者；连带换成宽限轮询（每 10ms 探树、死干净立即收手）——优雅退出
   不再空等满宽限，也消灭首版 time.AfterFunc 在 Wait 返回后 stray killpg
   的 pid 复用窗口。
2. windows Run 形态切 Job Object（issue 标题明定的机制）：kernel32 全走
   syscall.NewLazyDLL（不引 x/sys，零依赖红线不破）——Start 后立即挂 Job，
   取消 TerminateJobObject 整树终止，退出关句柄 KILL_ON_JOB_CLOSE 清树
   （宿主崩溃=句柄消亡同样触发，崩溃安全）；Command 形态保留 taskkill
   /T /F（无 Start 钩子挂不了 Job，/T 按父链快照走得到换组后代）——
   首版 doc 承诺 Job Object 而 code 是 taskkill 的相背就此消除。
3. Run 退出后残留清扫：windows 关 Job 句柄的 KILL_ON_JOB_CLOSE 天然清树
   ⇒ 两平台契约对齐，unix 在 Run 退出后清一次组内幸存者（无宽限直接
   KILL），「守护化残留不因主进程正常退出而泄漏」收进口径。
仓规对齐：裸 Option → RunOption（AGENTS.md 禁裸 Option；0.x minor 允许
breaking）；WithGrace 负值构造期 panic（总则①）、nil 选项回退默认
（总则②）、nil ctx 回退 Background。
测试 4 判据 → 11：跨组正对照（perl setpgrp 换组、取消后孙 pid 必死——
纯组击杀口径下必红，正是它钉住的行为）、Command drop-in 与跨组清树、
正常退出残留清扫、Option/预取消 ctx 契约面；原 4 判据用例一字未动全绿。
darwin 本机 -race ×2 全绿；GOOS=windows build+vet 过。
补齐 AGENTS.md/README 的 procx 条目（首版合并时矩阵表未更新）。
* fix(lint): sweepTimeout 归位 darwin 枚举文件——linux 上该常量无人使用（unused）
CI ubuntu 的 lint 抓到：sweepTimeout 只有 darwin 的 ps(1) 枚举用，
linux /proc 枚举不需要超时（ReadDir+ReadFile 无外部命令），声明在
linux 文件里恒 unused——平台专属标识符必须住在各自的 build-tag 文件里。
darwin 本机 lint 看不到这条（该文件在 darwin 被编译，常量有用例），
这正是跨平台守卫要 CI 全矩阵跑的原因。

## [v0.38.3] — 2026-11-09

### 修复(errors)：Cause 沿 Unwrap 链下探加深度上限——成环链死循环防御（#298）

对齐 biz_error.go maxFlattenDepth=32 纪律：成环 Unwrap 链（A 包 B、B 又
包 A，重构/聚合形态）此前让无界 for 永久迭代（goroutine 卡死+CPU 自旋）。
超限返回当前已到达的最深节点；环用例含 2s 死循环哨兵。

## [v0.38.2] — 2026-11-09

### 新增(procx)：进程树终止原语——Setpgid 建组 + TERM→宽限→KILL 整组（#284）

- Run/Command 双形态：Command 为 exec.CommandContext 逐字替换形态，Run 顺带统一 Result{Stdout,Stderr,ExitCode}
- unix：Setpgid 自立进程组，ctx 取消 TERM 整组（kill(-pgid)）→ WithGrace 宽限（默认 5s）→ SIGKILL 整组；WaitDelay 收口管道持有（孙进程继承 stdio 的场景）
- windows：taskkill /T /F 整树终止（纯 stdlib 无 Job Object——零依赖红线）；优雅档不存在，如实声明退化（不伪装）
- 验收判据全落：孙进程正对照（取消后组空）/负对照（无组杀真实泄漏复现，证明判据量的是树）/优雅档（trap TERM 自清理不被 KILL）/超时档（忽略 TERM 宽限后被 KILL）；darwin 实跑，GOOS=windows 编译验证

### 修复(test)：负对照 PID 经临时文件回传——孙进程继承 stdout 管道与进程内 buffer 数据竞争（-race 实证）

- 503048c

### 修复：pkg 第三轮 issue 清零（#292 #293 #295 #288 #289 #290 #296 #297）

* fix(jsonrepair): StripCodeFence 恢复 inline 围栏——散文+同行开栏被行首扫描静默丢弃，整段输入退化成空串（#293）
* fix(convert): AnyToStr(nil) 返回空串——REST 应答的 null 字段不再变成字面 <nil>（#295）
* feat(errors): Wrap kv 的结构化出口 Kv/KvFromChain + KVCarrier 接口——kv 不再只能拼进 Error() 文本（#292）
* fix(httpjson): ETag 面三连——no-store 门禁（RFC 9111 §5.2.2.5）/ 存储有界化（LRU 条目上限 + 单条 1MiB 体积上限）/ vary 头分区键 CanonicalETagKey·WithETagVary（#288 #289 #290）
* fix(urlguard): allowDomains 豁免 IP 面内置网段判定（checkIPForHost，写面/拨号面同源）+ blockedCIDRs 补 3fff::/20、5f00::/16、100:0:0:1::/64 对齐 IANA（#296 #297）

### 修复(lint)：groupMembers 直返 strings.Fields（staticcheck S1011）

- 79b5520

## [v0.38.1] — 2026-11-09

### 修复(pagination)：删未触发的 nolint:staticcheck 指令——nil 判定分支本就不触发 SA1012，nolintlint 报 unused（lint 清理，无行为变化）

- 65aaaed

### 修复(pagination)：WalkEach/Walk 伪翻页（无进展）检测 + WalkEach ctx 贯穿（BREAKING）——pkg#285、pkg#286

pkg#285：忽略 page 参数的服务器（Forgejo 15.0.1 现场形态）每页同长非空同内容，
短页/空页终止判定永不命中，行走空转到哨兵报错并丢弃全部已收集数据。第 2 页起
与上一页逐值相同（reflect.DeepEqual）即按列表末尾收手、返回/流出已收集数据
（对齐下游 EachBounded v0.99.10）；重复页在 yield/拼接前截住；上一页快照逐值
拷贝防 pooled buffer 别名误判；等长不同内容页序列不误判（用例双向钉住）。
pkg#286（BREAKING）：WalkEach 首参与 fetch 形参带 ctx，页间 ctx.Err() 检查，
与 Walk 及下游 EachBounded 的 PageFetch 形态对齐；nil ctx 兜底 Background；
godoc 用法示例同步。

- 文档：矩阵总览表重新生成（pkg 27 合 1 DESC 轮）
- 文档：版本引用对齐 v0.38.0

## [v0.38.0] — 2026-11-08

### 新增：round3 原语四连——httpjson 条件请求 / webhook 验签族(新包) / timex 时区显式变体 / SafeMap.Snapshot + secretx flake 关闭

- feat(httpjson) #269: WithETag(store,key) RequestOption——If-None-Match 注入、
  304 重放为 200 形响应（与 200 同一解码线）、304×驱逐竞态回查存储+降级
  无条件重发不硬失败、只对 GET/HEAD；ETagStore 接口（可插跨进程后端）+
  NewMemETagStore 进程内实现
- feat(webhook) #270: 新子包验签原语族——Validator/ValidatorFunc + HMACValidator
  (sha256/384/512 多算法、hex/base64 双编码、前缀剥离、header 回退链) +
  TimestampHMACValidator(时间戳防重放: Gitee 官方口径/容忍窗/时钟注入/query
  来源) + StaticTokenValidator + Fallback 多模消歧组合子(签名优先、错签不降级)
  + 平台构造器(GitHub/Forgejo/Gitea/GitCode/Gitee) + Registry(SafeMap 背书)；
  常量时间比较全走 hmac.Equal、空密钥构造期 fail-closed
- feat(timex) #271: ParseFirstInLocation(s,loc,layouts...)——无时区 layout 按
  loc 解析（逐 layout 独立判定）、nil loc 回退 UTC、边界与 ParseFirst 一致
- feat(container) #272: SafeMap.Snapshot()——Slice.Snapshot 的对偶，锁内整体
  拷贝/锁外交付的 plain map 出口
- fix(test) #275: secretx 篡改用例 1/16 flake 关闭（字符级翻转落 base64 末组
  丢弃位=空操作；改字节级 tamperBase64Byte + assertDecodedChanged 前置断言 +
  flipB64 性质门禁），-count=100 稳定

### 修复：lint 清理——gitee header 常量更名避 gosec G101 误报（含 token 字样），测试夹具删未用字段

- 9b5808c

## [v0.37.7] — 2026-11-08

### 修复(stringx)：MaskJSONFields 对 JSON null 判空——null 是合法 JSON，Unmarshal 把 nil 写进 map 且 err==nil（解析错误分支拦不住），随后写 map panic（assignment to entry in nil map）；同批输入里数组被正确拒绝、null 却崩，脱敏常在日志/错误处理旁路调用，panic 会盖掉真正的错误。null 与 `{}` 同口径：无字段可脱敏，原样返回。用例钉住 null/带空白 null/数组拒绝/空对象与正常对象对照（pkg#254）

- 14af82c

### 修复(pathx)：MatchesPath 非法 pattern 可判定 + SanitizeRel 盘符闸门按输出首段——两处独立修复：①MatchesPath 对语法非法 pattern（未闭合字符类 `[abc`）吞掉 ErrBadPattern 恒按「不匹配」处理，拒绝名单用法下配置写错一个 `[` 整条规则静默失效＝安全面 fail-open 无日志线索；新增 MatchesPathStrict（错误可 errors.Is(ErrBadPattern) 判定、文本带出问题 pattern）与 ValidatePattern（配置加载期 fail-fast 预检），MatchesPath 保持签名与宽松口径、文档明示。②SanitizeRel 盘符闸门此前按输入下标 i==0 判，"." 与空段 continue 后真正落为输出首段的段下标已非 0，"./C:/x"、".//C:/x" 绕过放行成盘符绝对路径（Windows 上直接越出目标目录）；判定移到 append 处（len(out)==0 即将落为输出首段），".." 抵消后盘符重新成为首段的形态（"C:/../x"、"a/../C:/x"）同样被拒；盘符不在输出首段时仍按普通段名放行（"a/C:/x" 口径不变）。issue 复现表逐项钉住（pkg#252、pkg#253）

- 0a2158d

### 修复(mathx)：WilsonCI 负 z 归一 |z|——负 z（取单侧下界的常见写法）让 half 带符号，产出 lo>hi 的颠倒区间且不报错：下游 `lo<=p&&p<=hi` 判据静默全 false，比例/成功率类告警该报不报。z 按绝对值参与计算（center 只用 z²本就对称），负 z 与对应正 z 逐位等价，恒有 lo<=hi；NaN/±Inf z 仍走 (0,0) 零区间。用例钉住 issue 复现表 + 负正 z 一致性 + 边界计数（pkg#251）

- 588703b

### 修复(cache)：SingleFlightCache 并发写入窗口下命中侧双计关闭——pkg#90 只抑制了加载路径的未命中计数（countMiss=false），load() 的命中分支仍无条件计 hit：前置 Peek 与内层自检之间该 key 被并发写入（Set/刷新/回填）时，同一次逻辑读被计成 miss+hit 两次，命中率被系统性抬高（违背 interface.go「一次读恰好计入命中或未命中之一」）。命中分支同受 countMiss 门控，noMissLoader/getOrLoadNoMiss/Stats/interface.go 口径注释同步。确定性注入用例（时钟第 2 次调用即 load 取时钟未持锁时写入，精确复现 issue 的 hit+miss=2）+ 400 轮真实并发 stress（pkg#250）

- ffc462d

### 修复(strmap)：Str2Str.LoadFromSlice 失败原子性——两段式编排（strStore.Load 先换缓冲 → strMap.LoadFromSlice 再校验）第二段失败（重复键）不回滚：strMap 仍持旧 offset，Get 用旧 offset 读新缓冲，旧 key 静默取到别的 value（合法但错误的数据，无任何错误信号）。改为先在临时 strStore 上打包、strMap 建索引成功后才整体提交——失败时两组件都保持旧表原样（StrMap.LoadFromSlice 自身先检测后变更）。失败态不变性用例：重复键与长度不齐两路径失败后旧值逐 key 比对 + 合法重载恢复（pkg#249）

- 7c89f54

### 修复(bloomx)：New 的 fpRate 归一闸门补 NaN/±Inf——NaN 与任何值比较均为 false，漏过 `fpRate<=0||fpRate>=1` 后 log(NaN)=NaN ⇒ 两个 clamp 比较同样不命中 ⇒ uint64(NaN)=0 造出 0 位过滤器，Add/Test 掩码退化为全 1 首次落位即越界 panic；归一后 m 再补有限性兜底（fpRate∈(0,1)、n 有限正 ⇒ 理论不可达，防 uint64(NaN) 渗进位数组长度计算）。NaN/+Inf/-Inf 用例钉住归一与可用性（pkg#248）

- f34bb50

### 修复(ctxscope)：With/From 写读对称——接口 Key 显式存 nil 可读回——无类型 nil 断言到接口 T 恒 ok=false，「存了 nil」与「从未写入」不可区分，而接口正是本包主用例（SessionScope/角色/sink）。改内部 entry[T] 指针载体（nil 值也有非 nil 载体）：存 nil 读回 (零值,true)；MustFrom 对存 nil 不 panic（nil 是显式合法值）；绕过 With 直写 context.WithValue 的值载体不是 entry，From/IsSet 一律按不存在处理；新增 IsSet 存在性糖（含显式 nil）。表驱动用例钉住 nil 往返/边界零值/bypass 拒收（pkg#245）

- e0c6d95

- 杂务：CHANGELOG 补 v0.37.7（第三轮 #245-#254；v0.37.5/v0.37.6 已被并行轮 round3-b/c 先行占用，本轮顺延）

## [v0.37.6] — 2026-11-08

### 修复(round3-c)：jsonrepair 收敛契约 / cli ANSI 列宽 / urlguard 非 ASCII host / pathx 设备名（#262-#268）

- jsonrepair: 连续尾逗号一次清空 + 非法转义保反斜杠 + 缺值补 null（#262 #263 #268）
- stringx: LastN godoc 对齐 #183 后的 FirstN 行为（#264）
- cli: displayWidth 先剥 ANSI 转义序列再计宽（#265）
- urlguard: host 判定前确定性折叠非 ASCII 形态，折叠不了 fail-closed（#266）
- pathx: SanitizeFileName 补 Windows 保留设备名改形（#267）
Fixes #262, Fixes #263, Fixes #264, Fixes #265, Fixes #266, Fixes #267, Fixes #268

## [v0.37.5] — 2026-11-08

### 修复：round3-b 七项修复——convert×3 / container / stringx / encoding / timex

- #255 convert.OptBool 数值/json.Number 分支与 AnyToBool 对齐（anyToBoolOK
  经 AnyToInt64 中转把 |v|<1 浮点截成 0，同一值两答案）；判定共用
  AnyToFloat64 != 0，两入口不再分叉
- #256 convert.AnyToUint64 json.Number 分支复用 float64ToUint64Safe
  （1e22 曾饱和成 uint64 最大值，UseNumber 主路径）
- #257 convert.BytesToInt64/BytesToFloat32/BytesToFloat64 判 err 返回 0
  （ErrRange 曾把饱和值/±Inf 当合法值，与文档及 BytesToInt 相反）
- #258 container.Queue.TryTake 关闭即返回 false（实现曾对齐文档反着走，
  Close 后照样出队一条）
- #259 stringx.SplitCSVNonEmpty 补 RFC 4180 引号状态机（引号内逗号不再
  切分、"" 转义、引号剥离；与 stdlib csv 交叉验证）
- #260 encoding.UnmarshalFromReader 改 excerptWriter 只驻留前 256 字节
  （成功路径不再把整份输入攒进内存）
- #261 timex.Date2Stamp DST 前跳日锚定目标日首个存在瞬间（对齐 dayStart
  口径，GetCurrentDayStartTime 日切窗口不再偏移一天）
每个 issue 补表驱动回归测试（含 issue 复现样本）；go test ./... -count=1
与 -race 全绿，vet / gofmt / zero-dep 干净。

### 修复(test)：secretx 篡改密文用例改为字节级翻转——字符级翻转会命中 base64 末组的丢弃位，篡改实为空操作（pkg#275）

- 3b27f7d

- 文档：子包口径统一到实际（24/25 合 1 -> 26 合 1，补登 ctxscope 与 secretx）

pkg 实际有 26 个公开子包，而文档里有两个数：
- AGENTS.md 第 3 行（由 matrix.py 的 DESC 同步而来）：「24 合 1」，清单 24 个
- README 首行（**手写**，没有任何生成器写它）：「25 合 1」，清单 25 个
- 实际：26 个 —— 多出 ctxscope、secretx
=> 同一个模块三套口径。
matrix-tools 的 desc_count_selftest 在**全工作区**里跑是红的（2026-11-08 实测），
但在 **CI 里是空跑**：matrix-tools 在 CI 里是独立 clone，兄弟仓不在旁边 =>
判据拿不到事实源 => 全「跳过」并 exit 0 => 这道门禁在唯一自动运行的地方没有牙齿
=> 同一类漂移第三次复发（23 -> 24 -> 26，matrix-tools#117 的同款）。
详见 matrix-tools#254。
本 PR 把三处口径统一到 26，并把 ctxscope、secretx 补进清单：
- README.md 首行：25 合 1 -> 26 合 1，补 ctxscope
- README.md 矩阵表行 + AGENTS.md 首行：24 合 1 -> 26 合 1，补 ctxscope、secretx
真相源（matrix.py 的 DESC）同步改动在 matrix-tools 仓，单独提 PR（该仓的
Matrix audit 门禁要等本轮地板对齐收敛后才绿）。
验证：
- python3 matrix-tools/scripts/desc_count_selftest.py => ✓（全工作区）

- 文档：版本行对齐最新 tag（refresh_versions.py）

## [v0.37.4] — 2026-11-08

### 新增(shellx)：Split 分词器——Quote/QuoteArgv 的逆运算

#243：本包只有转义没有分词，消费方（风险判定/审计留痕/用户确认）各自
手写 split，漂移不报错只在特定输入给出不同答案（空参丢失错位、未闭合
引号尽力解析绕过前缀判据、分隔符粘词切不开 a&&b）。
口径与 Quote 严格互逆，POSIX shell 词法、只拆词不展开（变量/命令替换/
glob 一律不碰——展开会让判定看到与执行时点不同的文本）：
- 单引号内一切原样（含反斜杠）；双引号内反斜杠仅转义 " \\ $ ` 与
  换行（\x 非 escape 字面保留）；
- 未加引号的 ; | & 是独立 token，相邻同类合并成 && / ||——分隔符不丢，
  是否按多条子命令处理是判据侧的严格度，不由语法层决定；
- 未闭合引号（ErrUnclosedQuote）、尾随转义（ErrTrailingEscape）报错，
  不做尽力解析；空参数保留（'' 还原空参，不错位）。
交付往返判据用例：Split(QuoteArgv(args...)) 逐项还原 args（含空参数/
引号/$/反引号/分隔符内容/UTF-8），Quote 口径一改即报警。
Fixes #243

### 修复(jsonrepair)：StripCodeFence 改逐行状态机——围栏内含围栏不再被腰斩

#241：整串正则的 (.*?) 非贪婪把内容里的 ``` 当结束标记，嵌套围栏
（JSON 字符串带代码块、4 反引号外栏包 3 反引号内栏）被截断，下游
Unmarshal/ParseLenient 报「无法解析为 JSON」而数据其实完整。
改法：弃正则，按 markdown 围栏规则逐行扫——开栏 = 行首 ≥3 反引号 +
可选语言标注；闭栏 = 行首反引号个数 ≥ 开栏且其后仅空白。内容里的行中
围栏（不在行首）与短围栏行（个数不足）不再被误当闭栏。「多块取第一块」
契约不变；只有开栏没有闭栏时返回开栏后全部（模型漏写闭栏的高频形态，
内容仍是要的 JSON，不整体作废）。同行开闭（```{...}```）与语言
标注剥离（#180）语义保持。
Fixes #241

### 修复(httpjson)：NormalizeBaseURL scheme 判定大小写不敏感 + 尾斜杠只裁 path 段

#239：scheme 前缀判定从字面量 HasPrefix 改为 EqualFold（RFC 3986 §3.1
scheme 大小写不敏感），命中后 scheme 归一为小写——HTTPS://EXAMPLE.COM
不再被二次补 http:// 产出 http://HTTPS://EXAMPLE.COM（scheme 静默降级
https→http、host 截成 "HTTPS:"）。只认 http/https 两个 scheme，
host:port / mailto:x 等其余形态仍按无 scheme 补 http://（既有行为不变）。
#240：尾斜杠裁剪从整串 TrimRight 收窄到 authority+path 段——先剥掉
scheme 前缀再裁（scheme 的 // 永不动，http:///http:// 不再截成 http:），
首个 ?/# 之后的 query/fragment 原样保留（http://host/?x=/ 的 query 值
/ 不再被静默吃成空串）。刻意不走 url.Parse 重组：u.String() 会重排/
百分号转义输入，违背「小写输入零变化」；保持函数 doc 承诺的字符级规则。
Fixes #239
Fixes #240

### 重构(stringx)：Contains 委托 slicex.Contains[string]——删第二份同构循环体

#242：stringx.Contains 与泛型版 slicex.Contains 函数体逐字同构（参数同向），
泛型落地后 []string 特化无存在理由，两份副本的 risk 在改口径时只改一处。
保留 API（签名不变、调用面零改动），实现改一行转发 slicex.Contains[string]，
doc 注明语义单源与保留理由（字符串场景可读性）。用例补 nil/空串/含分号
与单引号探针，并逐项钉住两函数一致。
Fixes #242

- 文档(agents)：治理同步——错误处理放开 stdlib 包装、日志 ErrKey 常量、Option 非法值总则与超时默认值段

Refs matrix-tools#237

- 文档：版本引用对齐最新 tag（refresh_versions）

- 格式(shellx)：Split 主循环改 tagged switch（golangci-lint QF1002）

## [v0.37.3] — 2026-11-08

- 与 v0.37.2 相比无代码差异（版本号/打包变更）

## [v0.37.2] — 2026-11-08

### 修复：urlguard exotic 闸门不受 allowPrivate 豁免；errs COW 先克隆；cache 增 MGetEx（#236 #237 #238）

- 04ad929

- 文档：溯源注释的旧 org 前缀改为「上游 zentao-*」——消除 audit 残留引用误报（旧 org 字样触发残留引用门禁）
- 文档：版本引用对齐最新 tag（refresh_versions）

## [v0.37.1] — 2026-11-07

### 修复(mathx)：Clamp 校验 lo<=hi，上下界颠倒快速失败（#228）

- 75ea028

### 修复(mathx)：RandIntn 校验 maxInt>0，带上下文 panic 替代 math/rand 裸 panic（#229）

- ac2d1ab

## [v0.37.0] — 2026-11-07

### 新增(secretx)：NewPassphraseBox 派生前置形态——PBKDF2+随机 salt 前置+v2 版本位（#212）

- 5652548

### 新增(pagination)：WalkEach 流式分页行走器——yield 提前终止、budget 预算哨兵（#211）

- 3fcf5a1

### 新增(ctxscope)：命名空间 Key + 泛型 With/From/MustFrom——请求级作用域传递原语（#201）

- 7276b37

### 新增(container)：Slice.RemoveIf 原子谓词删除——AppendIfAbsent 的删除侧对偶（#199）

- c07ffd6

### 新增(bufiox)：TailLines 行粒度尾部窗口——超预算按行丢头、truncated 诚实信号（#198）

- ac1febf

### 新增(timex)：ParseFirst 多 layout 容错解析——first-match 语义（#197）

- ee6de47

### 新增(cache)：SingleValue 单值 TTL 缓存——无 key 参数复用 LocalCache+SingleFlight（#213）

- 89d4f3f

### 修复(cache)：lint 修复——setWithTTLIfGen 去掉未用 ctx、测试 err 遮蔽改名（#200 #213）

- 913910d

### 修复(timex/cron)：NewRandomDelaySchedule/NewDelaySchedule 构造期校验非法参数（#227）

- d7fd99b

### 修复(errors)：BizError Error/String 统一格式、嵌套 BizError 扁平化、WithStatus 带 cause（#202）

- 97f28df

### 修复(cache)：LocalCache per-key 世代号——Del/覆盖写在途回源不回填旧值（#200）

- 4302740

### 修复(cache)：SingleFlightCache 对 nil 值真值命中双重计 hit——命中即早退（#203）

- 445c856

- 文档(pathx)：MatchSimpleGlob 补「* 跨 /」语义声明与钉语义测试（#210）
- 文档(cache)：契约写明 (nil,nil) 的三种情形与 GetEx 的区分职责（#209）

## [v0.36.0] — 2026-11-07

### 新增：SecureString/SecureBytes + pagination.Walk + httpjson.NormalizeBaseURL——zentao-mini/max 同源 helper 下沉单源

上游 zentao-mini#22（原 org 的 common 仓已归档，按 README 接档路线落本仓）：
- secretx: 内存态敏感凭证持有原语（XOR 混淆+双向零化+副本隔离+常量时间比较）
- pagination: Walk 翻页聚合单源（短页即末页+逐页 ctx 检查+MaxWalkPages 防呆）
- httpjson: NormalizeBaseURL（补 scheme/去尾斜杠/去空白/空串原样）
三个落点均为既有包内新增符号，零第三方依赖纪律不破；README 三行描述同步。

- 文档：版本引用对齐 v0.35.2

## [v0.35.2] — 2026-11-07

### 修复(mathx)：ProbabilityElems 加动态范围判据并消除中间量溢出，跨平台不再分叉（#208）

## 一、先确认边界：这不是「浮点算错了」
band[i] = int(acc * p) 里的 p = 10^n 让**最小权重恰好分到 precise 个单位**，
于是 band 的最大值是 total × p。超出 int 表示范围时，Go 的 float64→int 转换是
**实现相关**的，两种实测结果都不是错误信号：
| 平台 | int(1e306) | 后果 |
|---|---|---|
| arm64 | MaxInt64（饱和）| 多个大桶挤成同一个数，比例被静默改写 |
| amd64 | MinInt64 | Probability 的 max<=0 分支直接退化到「恒选 index 0」|
饱和那一侧尤其隐蔽。实测 [1e-30, 1e-30, 1e3]（precise=1000）得到
band = [1000, 2000, MaxInt64]：第二个桶真实占比 1e-33，band 却给出
2000:9.2e18 ≈ 2.2e-16，**被高估约 17 个数量级**。而 band[0] > 0，函数末尾那条
band[0] <= 0 的退化检查完全拦不住。
结论：这是表示法的**固有**边界，不是实现限制。最小桶要 ≥ 1 个单位、最大桶要
≤ MaxInt，两者不可兼得时无解——没有任何整数 band 能同时表达这两端。故只能
① 加防御判据；② 把边界写进文档（实际触发需要总权重比最小权重大 15~18 个
数量级，正常概率路由到不了）。
选 panic 而不是钳制，与既有输入校验同口径：本函数本来就对 0/负权重、precise<1
直接 panic，理由是「概率路由静默出错比快速失败更危险」。钳制会改变分布语义而
不告知调用方，正是要避免的那类静默失真。
## 二、第一版被 CI 抓出的真问题：中间量先溢出
第一版只加了 total × p 的范围判据，`p` 仍按 `log10(precise/minW)` 算。CI
（amd64）当场报红：
```
p_溢出为_Inf: panic 信息应指明动态范围，
got mathx: ProbabilityElems produced a degenerate band (...)
```
根因是**中间量先溢出**：precise=1、minW=1e-320 时 `1/1e-320` 已是 `+Inf`，
于是 `n = +Inf`，而 `int(+Inf)` 又一次是实现相关的——arm64 给 MaxInt64、
amd64 给 MinInt64 ⇒ p 在 arm64 上是 `+Inf`、在 amd64 上是 `0`，两个平台走进
**完全不同**的分支（一个撞范围判据、一个撞退化判据）。实测：
```
precise=1 minW=1e-320  oldN=+Inf newN=321  old-int=9223372036854775807 new-int=321
```
修法：n 改用对数之差 `ceil(log10(precise) - log10(minW))`。数学等价，但
`log10(1e-320)` 正常返回 -320，不产生溢出中间量，n 恒为有限值 ⇒ `int(n)` 不再
是实现相关的。同时把 `precise` 的有限性纳入校验（`+Inf` 会让 `log10` 给 `+Inf`，
同一个洞）。
这条值得单独记下来：**判据本身也可能建立在一个溢出过的中间量上**——若只在末端
比大小，而中间量早已分叉，判据在两个平台上的行为会不一样。
## 三、用例（mathx/probability_range_test.go，3 条）
1. `DynamicRangePanics` —— 3 组极端输入（跨度 1e303、跨度 1e33 且多桶、次正规
   权重让 p 自身溢出），断言 panic 且信息含 `too wide a dynamic range`；
2. `DynamicRangeBoundary` —— **成对判据**：跨度巨大但仍在 int 范围内
   （[1e-9, 1e3]，precise=1000 ⇒ max band ≈ 1e15）必须正常返回，且
   `band[0] == precise`、`band[1]/band[0] ≈ 1e12`。没有这一条，第 1 条的全绿
   可能只是因为判据把所有大跨度输入都拒了；
3. `BandStaysMonotone` —— 通用不变式：任何被接受的输入，band 必须非递减且
   首项 ≥ precise，兜住「判据放得太宽」的反向失误。
另在既有 `TestProbabilityElems_InvalidInputPanics` 补一条 `precise 为 +Inf`。
## 四、反向验证
| 注入 | 结果 |
|---|---|
| 去掉动态范围判据 | 3 条子用例全红：`未 panic——整数 band 装不下时必须快速失败` |
| 阈值收紧到 1e12（判据过严）| `DynamicRangeBoundary` 红（[1e-9, 1e3] 被误拒）|
| 退回除法形态 | **本机 arm64 不区分**（两种形态在 arm64 上都撞范围判据）|
第三条要说清：本机是 arm64，退回除法形态后用例**仍绿**——判别力在 amd64。
这正是第一版漏检的原因：本地全绿、CI 红。故这一条的证据是 CI 由红转绿，不是
本机反向验证。
## 验收
- `go build ./...` + `go test ./...` 全仓绿（27 个包，本机 arm64）
- `golangci-lint run ./mathx/...` ⇒ 0 issues
closes #208

### 修复(urlguard)：DialFunc 的 host 判定改走 checkHostLiteral，与 CheckHost 同源（#204）

DialFunc 的注释自己写着「消费方可能只挂 DialFunc（不挂 CheckURL），此时 host
判定不能缺席」，但补的只是 checkHostPolicy（消费方钩子），内置的约定域判定
没走 —— 而后缀判定（localhost / internal / local / localdomain）住在
checkHostLiteral 里。于是只接 DialFunc 的消费方对 foo.local、x.internal、
localhost 这类约定域完全放行，只要解析出的 IP 不在私网段；同一目标经
CheckURL/CheckHost 是被拒的。
写面拒绝、拨号面放行是最难发现的那种不一致：两个面的单测各自都绿，只有把它们
摆在一起比才看得出来。
修法是拨号层改为复用 checkHostLiteral。复用而非重写是有意的：那里共有四条
规则——内置约定域后缀、exotic IPv4 字面量、allowDomains 优先、allowPrivate
短路。各写一份必然漂移，而漂移的形态正是本例这种「一边拒一边放」。
用例（urlguard/dial_host_test.go，3 条）全部用 g.lookup 固定解析出公网 IP
（93.184.216.34），把域名规则的作用与 IP 范围判定的作用隔离开——公网可达时，
唯一能拦下拨号的只剩域名规则：
1. TestDialFunc_BlocksBuiltinDomainSuffixes —— 6 个约定域（含「等于后缀本身」
   的 local / internal，不能只比对点分后缀），断言拦截原因是 blocked domain
   且底层拨号未被调用；
2. TestDialFunc_AllowsPublicHost —— 成对负对照：同一套 fake lookup、同样的
   拨号包装，example.com 必须放行到 next。没有这一条，第 1 条的全绿可能只是
   因为拨号层把一切都拒了；
3. TestDialFunc_HostVerdictMatchesCheckHost —— 同源判据：11 条用例逐条比对
   CheckHost 与拨号层的结论。覆盖 allowPrivate 短路与 allowDomains 优先这两条
   最容易「各写一份」的规则，还包含「allowDomains("svc.local") 不得顺带放行
   other.local」。
反向验证（把 checkHostLiteral 退回 checkHostPolicy，实测红后还原）：6 条约定域
子用例全红（错误应为约定域判定，实得 next 的哨兵错误），同源判据 4 条打印出
「CheckHost 拒绝但拨号层放行」的漂移——失败信息本身就是这次修复的价值说明。
兼容性：这是收紧。此前能通过的 .local 类目标现在会被拒，但那些目标经
CheckURL/CheckHost 本来就是拒绝的，修复只是让两个面一致，不是新增策略。
已有 TestDialFunc_* 全套（含 exotic IPv4、端点复核 UDP/IPAddr/未知类型、自定义
IP/host 钩子）仍全绿。
验收：go build ./... + go test ./... 全仓绿（27 个包）；golangci-lint run
./urlguard/... 0 issues。
closes #204

### 修复(bufiox)：Release 兑现 e 参数语义，残留归位、旧视图失效、大缓冲不再滞留（#216）

接口文档（bufiox.go）对 Release 承诺三件事，实测皆不成立：
- 释放缓冲并重置状态 —— Buffered()>0 时缓冲整体保留、ri 也保留，下个周期从
  上一轮的消费点继续交付；
- 此后 Next/Peek 的视图全部失效 —— 旧视图仍指向同一有效数组，内容一字未变；
- 参数 e 供「连接错误触发整段丢弃」 —— 完全未读取，这条用途根本没实现。
修法先要认清一点：e == nil 时不能无条件丢弃残留。socket 上已读出的字节无法
重读，丢弃就是丢数据；而且残留恰恰可能是同一 TCP 段里下一条报文的开头——正是
带缓冲读接口存在的理由。所以让 e 真正承担职责：
- e != nil：整段丢弃（连接层错误意味着残留不可信：半包，或上个连接的尾巴）。
  这是文档写明但从未实现的那条。
- e == nil：保留残留但把游标归零——buf[ri:] 原地左移成 buf，于是「重置状态」
  对残留也成立（ri 恒为 0），且旧视图指向的区域正是被覆盖的位置。
- 容量远超残留时（cap > 4×残留）换右尺寸数组，把大缓冲还给 GC，而不是让它
  随残留帧滞留（最坏可到 1GiB 上限）。原地左移零分配，只有触发收缩才多一次
  拷贝。
BytesReader 侧同步兑现 e：e != nil 整段丢弃；e == nil 回到起点。两者不一致
不是随意为之——固定帧的「重置」自然含义就是重新解析同一帧，可续读的流不能
这么定义。接口文档已重写为「共同部分 + 差异由实现定义（见各实现文档）」，不再
用一句含糊的「重置状态」同时描述两种东西。
另外明确一条措辞：Go 里被「释放」的切片不会变野指针，读它不 fault，只会读到
已不属于你的字节。故接口文档现在写明「Release 之后不得使用旧视图」是契约要求、
不是运行时保证——把契约要求误当运行时保证，排查方向会被带偏。
用例（bufiox/release_contract_test.go，5 条）：Release(err) 整段丢弃；
Release(nil) 保留残留 + 游标归零 + ReadLen 归零 + 残留可继续读且未截断；旧视图
确实失效（断言内容已变）；大缓冲收缩（先断言前提 cap > 4×残留，再断言不丢数据）；
BytesReader 两侧语义各一条。原有 TestDefaultReader_ReleaseReset 语义未变仍绿。
反向验证（3 处注入，各只打掉对应用例）：
| 注入 | 变红的用例 |
|---|---|
| 退回修复前实现 | 4 条全红：Buffered=6 want 0 / ri=2 want 0 / 旧视图内容仍是 "ab" / cap=8192 仍远超残留 1000 |
| 只忽略 e | 仅 ReleaseErrorDiscardsResidual 红 |
| 只去掉收缩 | 仅 ReleaseShrinksOversizedBuffer 红 |
三处注入各只命中预期用例，说明 5 条用例彼此独立、没有互相兜底——「一组全绿」
不代表每条都在判事。
验收：go build ./... + go test ./... 全仓绿（27 个包）；golangci-lint run
./bufiox/... 0 issues；gofmt -l bufiox/ 空。
closes #216

### 修复(convert)：ConvertStruct 对非法 dst / nil src 报错，不再静默 no-op（#207）

ConvertStruct 的 godoc 写着「dst 必须是指针」，但实现遇到非指针或 nil 指针
时直接 return nil，src 传 nil 同样静默返回 nil。调用方拿到 nil 就认为 dst
已填充，实际一个字都没变；错误在下游很远的地方才暴露，且现场已无迹可循。
修法是入参不满足契约时返回明确错误，覆盖九种形态：
| 入参组合 | 修复前 | 修复后 |
|---|---|---|
| dst 为 nil 接口值 | nil | dst must be a non-nil pointer to struct, got <nil> |
| dst 为 nil typed pointer | nil | dst must be a non-nil pointer to struct, got nil *T |
| dst 非指针（结构体值 / 标量） | nil | dst must be a non-nil pointer to struct, got T |
| dst 是指针但不指向结构体 | nil | dst must point to a struct, got pointer to int |
| src 为 nil 接口值 / nil typed pointer | nil | src must be a struct or non-nil pointer to struct |
| src 不是结构体 / 指向的不是结构体 | nil | 同上 |
字段层面仍保持尽力而为：src 独有字段、dst 独有字段、类型不一致的字段、不可
导出字段都跳过而不报错。godoc 已写明这条边界——映射是「同名字段尽可能复制」，
不是严格转换；本次只收紧入参契约，不动字段语义。
用例：
- convert/struct_contract_test.go（新，2 条）
  - TestConvertStruct_ContractViolations —— 九种非法入参各一条子用例，断言
    err 非空且错误文案命中关键片段（只看「有 error」不足以区分是哪种契约违反，
    排错时文案本身是信息）；
  - TestConvertStruct_HappyPathAndFieldLevelTolerance —— 成对正例，钉住
    「字段层面仍尽力而为」。若有人把字段不匹配也升级成 error，这条会红，
    从而防止本次修复把契约收得过紧、破坏既有调用方。
- convert/coverage_test.go：TestConvertStruct_NilDst 原先断言「nil dst 返回
  nil error」——它把这个缺陷固化成了预期。已改为断言必须报错。
反向验证（把 struct.go 退回修复前实现，实测红后还原）：TestConvertStruct_NilDst
加 9 条 ContractViolations 子用例全部变红，而 HappyPathAndFieldLevelTolerance
保持绿——说明失败来自契约检查本身，不是夹具写坏。
兼容性说明：这是行为变更，修复前返回 nil 的调用点现在会拿到 error。判断依据
是这些调用点本来就在做契约违反的事——它们的 dst 从未被填充过。仓内所有调用点
与 go test ./...（27 个包）均通过。
验收：go build ./... + go test ./... 全仓绿；golangci-lint run ./convert/...
0 issues。
closes #207

### 修复(convert)：AnyToBool 数值分支改经 float64 比较，修复 |v|<1 浮点被判假（#205）

AnyToBool 的 godoc 承诺「数值类型非零为 true」，实现却经 AnyToInt64 中转，
而 AnyToInt64 会把浮点截断成整数：
| 输入 | 文档承诺 | 修复前 |
|---|---|---|
| `AnyToBool(0.5)` | true | **false** |
| `AnyToBool(-0.9)` | true | **false** |
| `AnyToBool(json.Number("0.5"))` | true | **false** |
触发面不是刁钻取值：概率、比例、阈值这类字段的正常取值就在 (0,1) 内，经
AnyToBool 判开关时静默判假——JSON 配置或查询参数里的开关整体失真。
修法是数值分支改走 AnyToFloat64（json.Number 分支同）。这是安全的双向替换：
浮点只在自身为 0 时才舍入到 0；整数经 float64 转换也不会变成 0。
顺带修正一处同源的静默失真：`AnyToInt64(uint64 大于 MaxInt64)` 按自身契约
返回 0（「超出表示范围属于无法转换」），于是 `AnyToBool(uint64 最大值)` 修复
前也是 false——但该值非零，按 AnyToBool 的文档应为 true。改经 float64 后
一并修正。
用例 `convert/conv_bool_test.go` 两条：
1. `TestAnyToBool_FractionalFloat`（17 条子用例）——(0,1) 区间、极小次正规数、
   负零、json.Number 各形态，外加整数侧回归（1 / -1 / 0 / MaxInt64 / MaxUint64）；
2. `TestAnyToBool_NotViaIntTruncation`——**机制断言**：先验证 `AnyToInt64(v) == 0`
   这一前提成立（否则用例失去判别力），再断言 `AnyToBool(v)` 为 true。它直接
   钉住「不得经整数中转」这条根因，有人换回 AnyToInt64 立刻变红。
反向验证（把两处 AnyToFloat64 改回 AnyToInt64，实测红后还原）：
```
--- FAIL: TestAnyToBool_FractionalFloat/float64_0.5        AnyToBool(0.5) = false, want true
--- FAIL: TestAnyToBool_FractionalFloat/json.Number_0.5    AnyToBool("0.5") = false, want true
--- FAIL: TestAnyToBool_FractionalFloat/uint64_最大值       AnyToBool(0xffffffffffffffff) = false, want true
--- FAIL: TestAnyToBool_NotViaIntTruncation                 走了整数截断路径：非零浮点被判成 false
```
closes #205

### 修复(timex/cron)：dow 字段 7≡Sunday 在列表/范围/步长等所有形态下生效（#215 #206）

#186 的修法是「整串判等」：`if dowSpec == "7" { dowSpec = "0" }`。它只覆盖
单值形态，从系统 crontab 迁移过来的 `0 2 * * 6,7` 与 `0 2 * * 5-7` 仍然
加载失败。更糟的是报错文案写着「7≡Sunday 已支持」，把排错方向反向误导。
修法是把 7→0 的归一**下沉到 token 落位层**，而不是在文本或数值层提前归一：
- 文本层 `strings.ReplaceAll(s, "7", "0")` 会把 `5-7` 变成 `5-0`，撞上
  `start > end` 判非法区间；
- 只在 getField 层做数值归一，同样撞 `start > end`。
正确做法是范围按**端点原值**遍历、置位时走 dowValue（7→0）。这样 `5-7`
天然是环形区间 {5,6,0}，不会被端序检查挡住。
同时：
- `maxDow = 7` 作为输入上界，域仍是 0–6；单值 `N/step` 的步进上界取 6
  （7 与 0 是同一个 Sunday，算进去等于重复遍历一圈）。
- 报错文案改为 `(0/7 都是 Sunday; MON/JAN 等名字与 L/W/# 扩展不支持)`，
  不再声称「已支持」而实际不支持复合形态。
- 顺带修正注释：0=Sunday 编号下 `5-7` 是**周五到周日**（issue 原文写
  「周六-周日」是错的）。
用例 `timex/cron/dow7_test.go` 三条：11 行复合形态表（`7`/`6,7`/`7,6`/
`5-7`/`6-7`/`7-7`/`0-7`/`5-7/2`/`7/2`/`0-6`/`1,3,5`）、越界仍须报错
（`8`/`5-8`/`7,8`/`9-7`）、以及 `0 0 0 * * 5-7` 的真实推进（11-07 /
11-08 / 11-13）。
反向验证（4 处，均实测红后还原）：
| 注入 | 结果 |
|---|---|
| 退回整串判等（#186 旧修法） | `Parse("0 2 * * 6,7")` ⇒ `out of range [0, 6]: 7`；`5-7` 同样报错 |
| 文本层 ReplaceAll 归一 | `5-7` ⇒ `invalid range: 5-0` |
| 范围落位不做 7→0（`1 << i`） | `Dow bits = 11100000`，且漏掉周日（want 11-08，实得 11-13） |
| 去掉单值分支的 `start = dowValue(start)` | `0 2 * * 7`：`Dow bits = 10000000`，want `0000001` |
closes #215, closes #206

### 修复(timex/cron)：Next 在夏令时回拨/前跳日退化成过去时刻或死循环（#214）

根因：所有推进都是「绝对时间 Add(1h/1m/1d) 后按墙钟重建」，而 Go 的
time.Date 对两类歧义墙钟都会把时刻挪到别处：
- 回拨（同一墙钟出现两次）⇒ 一律解析到**较早**那次；
- 前跳（墙钟不存在）    ⇒ 归一到**前一天 23:00**（实测）。
回拨：America/New_York 2026-11-01，01:59 EDT +1min 走到 01:00 EST，重建却
回到 01:00 EDT（倒退 59 分钟）⇒ Minute()==0 触发 WRAP、各字段全部命中 ⇒
Next 返回**早于入参**的时刻；调度器「next := Next(prev) → sleep → prev = next」
的循环立即热转且永不前进（任务被疯狂重复执行）。
小时字段受限时更糟：从 02:00 EDT 推下一个 03:00，绝对时间先落到 01:00 EST、
重建回 01:00 EDT ⇒ **Next 内部死循环**，调用方连返回都拿不到。
前跳：Santiago 2026-09-06 的 00:00 不存在，time.Date 归一到 09-05 23:00，
而 AddDate(0,0,1) 从 09-05 00:00 也落到 09-05 23:00 ⇒ 日期循环原地打转、
**Next 永不返回**（#214 只提到「补用例」，实测是第二个独立缺陷）。
改法：推进一律走绝对时间，不再按墙钟重建。
- nextHourStart / nextMinuteStart：绝对推进，天然单调；现代时区偏移都是整
  分钟 ⇒ 绝对整点必然落在墙钟整分上。
- nextDayStart / nextMonthStart / firstExistingOnDay：用**正午**做日期加法锚点
  （真实时区不在正午跳变），落到目标日/月**最早存在的时刻**（前跳日即跳变后
  的首个整分），不再被 time.Date 归一回前一天。
- Next 外层加单调性兜底：真出现不前进时从入参后挪一秒重算（最多 3 次）。
  它只兜「轻微倒退」，钉住正确性的仍是精确时刻断言（见反向验证第 5 条）。
语义钉死（写进 doc.go）：回拨时两次出现都会各自被推进到；表达式要求的墙钟
时刻当天不存在（仅在 9/6 00:00）则当年不触发、顺延到次年命中，不补到 01:00。
反向验证 5 处（均实测红后还原）：
1. 小时循环退回墙钟重建 ⇒「Next 在回拨窗口内死循环（3s 未返回）」
2. 分钟循环退回墙钟重建 ⇒「Next(01:59 EDT) = 01:00 EDT，want 01:00 EST，差 -1h」
   同时「第 0 次迭代未前进（回拨窗口内活锁）」
3. 日期循环退回 AddDate+墙钟重建 ⇒「Next 在前跳日死循环」
4. firstExistingOnDay 退回裸 time.Date ⇒「前跳日应仍落在 09-06，实得 09-05 23:00」
5. next() 注入 -1ns ⇒ 单调性用例仍绿（兜底生效）、精确时刻用例红（钉住的是循环本身）

## [v0.35.1] — 2026-11-06

### 修复(mathx+stringx+cron+cache+httpjson)：看板 #182-#189 八单——NaN 防护/FirstN 口径/Union 保序/cron 方言收口/Key 警示/空 body 对偶面

- d29efcd

- 杂务：版本行 v0.35.0 + AGENTS DESC 对齐真相源（发版后置）
- 杂务：矩阵总览表重生成（gen_tables）
- 杂务：AGENTS DESC 行对齐矩阵真相源（refresh_agents_desc）

- 格式：移除 Union 保序化后不再使用的 placeholder/mark（unused）

## [v0.35.0] — 2026-11-06

### 新增(stringx)：新增 MaskJSONFields JSON 字段级脱敏原语

顶层 JSON 对象的字段值替换为固定文案：字段缺省时补上（map 赋值语义）、
解析失败透传 json 包原始错误（不包装——消费方把错误文本透传到响应，
包装即改文案）、输出经 map 字典序重序列化。
来源：kuboard-go maskSensitiveSystemConfig 已验证实现上移。

### 新增(pathx)：新增 MatchSimpleGlob 单星名字通配原语

只有第一颗 * 是通配符（第二颗起按字面量）、空串判不匹配、? 非通配——
与 full-backtracking 的 MatchGlob 是反语义对（混用会静默改判）。
来源：kuboard-go simpleGlobMatch 已验证实现上移；2,000,000 例差分 fuzz
两实现 0 差异。

### 修复(pathx)：MatchSimpleGlob 补 starIdx 防御分支（gocritic offBy1）

- 08fac96

- 杂务：README/AGENTS 版本行对齐 v0.34.1（维护轮 #233 发版后置）

## [v0.34.1] — 2026-11-04

### 修复(jsonrepair)：StripCodeFence 剥掉任意语言标记（#180）

(?:json)? 白名单 → [a-zA-Z0-9_+-]* 字符集：yaml/toml/sql 等语言围栏的
标记不再残留进结果污染下游解析（oagentkit/textx#21）。
Co-authored-by: Murphy-Yi <zhangyi@Murphy-YideMac-Mini.local>

## [v0.34.0] — 2026-11-05

### 修复(secretx)：删无用 nolint 指令（nolintlint 门禁）

- 1da20d2

- 文档：版本引用对齐 v0.33.0

- feat(secretx)+feat(container): 信封加密原语 + Slice.AppendIfAbsent（#177 #178）

- secretx：ParseKey/NewBox 构造期 fail-fast/Box.Seal-Open（v1:+base64(nonce‖ct)，
  GCM 必带认证，与 domain-manage 手搓形态逐字节兼容存量零迁移）/
  SealWithAAD 防挪用/ErrUnsupportedVersion 轮换识别位/ErrBadCiphertext 统一表述。
  收编四仓各写一遍的 AES 手搓（CFB 无认证、缺钥明文透传两个劣化形态一并消灭）
- container.Slice.AppendIfAbsent：原子「查重+追加」，有序注册表的 GetOrSet 对应物
关联问题单：pkg#177、pkg#178


## [v0.33.0] — 2026-11-03

### 修复(stringx)!：Filter 对齐 slicex.Filter 保留命中语义，新增 KeepFunc/DeleteFunc（#173）

机制根因：同库同名反方向——slicex.Filter「保留命中」（主流 filter 语义：
JS Array.filter / Java Stream.filter / Rust Iterator::filter），stringx.Filter
「剔除命中」，且函数名不带任何方向提示。谓词写反零报错，只是结果里多/少了
字符；实测事故（domain-manage echoSafe）：404 文案回显路径参数的净化按
「保留命中」的肌肉记忆写谓词，控制字符被留下、正常字符被滤掉，该回显面还
接收 %00 类输入，属安全敏感路径。
修法（破坏性变更，0.x minor 允许 breaking）：
- stringx.Filter 语义对齐 slicex.Filter（保留命中），同库同名不再反向；
- 新增对偶入口 KeepFunc（保留命中）/ DeleteFunc（剔除命中，即 Filter
  v0.32 及以前的行为），方向在调用点自解释（命名对齐 slices.KeepFunc/
  DeleteFunc 口径）；
- godoc 写明语义变更、与 slicex.Filter 的关系与迁移指引。
迁移说明：
- 原「剔除命中」用法（filter(x)=true 的字符被删）→ 改用 stringx.DeleteFunc，
  行为完全一致；
- 原谓词写的是「保留条件」却以为在删 → 改用 Filter/KeepFunc（结果与旧版
  相反，需按意图复核）；
- 仅删除/保留 ASCII 字符类场景亦可改用 slices.DeleteFunc/slices.KeepFunc。
测试（stringx/string_test.go）：TestFilter（新语义）/ TestKeepFunc /
TestDeleteFunc / TestDeleteFunc_ControlChars（#173 事故场景：剔除控制
字符，Filter 取反谓词同效）/ TestFilter_AlignsWithSlicex（slicex/stringx
两侧行为一致的对偶用例）。

### 修复(cache)：选择性 stale——Source 可取消 stale 兜底（#172）

机制根因：load 路径的 stale 标志在调 Source **之前**捕获（local.go），
Source 内 Del(key) 也取消不了本次兜底——消费方表达「此错误不走 stale」
只剩两条路：整体放弃 LocalCache 自管，或发明毒标记绕行（1ms TTL 成功值
写入、读到即 Del 还原错误）。且任意回源 error 一律吃 stale，「上游明确
拒绝」类错误（401/403/业务拒绝）会借旧缓存旁路认证，安全语义静默劣化。
修法（两条取消路径，Source 在被调用期间即可表达）：
- 兜底资格在回源**之后**重新捕获（staleEntryNow）——Source 内 Del/覆盖
  写入该 key 即取消本次兜底，不再用回源前的快照；
- 新增哨兵 cache.ErrNoStale：回源错误 unwrap 命中（errors.Is）即原样
  回传、不吃 stale——按错误类型选择「此错误不配吃 stale」，消费方对
  业务错误的 errors.Is 判定不受影响。
SingleFlightCache 装饰层语义同步：singleflight 共享的是 leader 过滤后
的结果，等待方与 leader 一致（取消即全员拿错误，兜底即全员拿陈旧值）。
测试（cache/stale_selective_test.go）：Del 取消生效（OnStale 不触发）/
ErrNoStale 取消生效且普通错误仍兜底 / 未取消仍兜底（原 StaleIfError
行为不回归）+ 单飞 leader/等待方结果一致性用例（三种情形）。

- 文档：版本引用对齐 v0.32.0

## [v0.32.0] — 2026-11-03

### 新增(cache)：NXSetter 可选能力接口 + LocalCache.SetNX + 装饰器转发（#168）

「不存在才写入」原子原语（同 Redis SET NX）：判定+写入单锁区间原子，
Get+Set 两步在并发下会双双判「不存在」互相覆盖。契约全文见 NXSetter：
过期条目与 null marker 按不存在处理；失败零副作用（不动值/TTL/淘汰
顺序）；ttl<=0 不过期（与 SetWithTTL 同口径）；写路径不计 Stats。
可选能力接口 + 类型断言（同 HitGetter/PeekGetter 哲学），不进 Cache[T]。
SingleFlightCache 透明转发、内层无能力时 fail-loud（ErrNXUnsupported，
errors.Is 可判定）；cachetest 补 RunSetNXWithClock 硬失败契约套件
（六情形，含「失败不刷新 TTL」判据），直连与装饰两形态都跑。

### 新增(stringx)：LastN 保尾截断原语（#167）

与 FirstN 互为镜像：保尾（丢前缀、留后缀）、省略号拼头。文件名限长保
扩展名这类「尾部比头部更有信息量」的场景不再手搓 rune 回退循环。
边界口径对齐 TruncRunes（n<=0 = 保留 0 个、不 panic），与 FirstN 的
n<0 怪癖（循环永不命中截断点⇒原样返回）有意不同，已在 godoc 注明。

- 文档：版本引用对齐 v0.31.2

## [v0.31.2] — 2026-11-02

### 修复(cli)：Table 按序列算宽度——emoji 状态符号不再错位（#162）

cli.Table 的列宽用 displayWidth 估算，旧实现逐码点累加，于是：
- ⚠️（U+26A0 + U+FE0F）被算成「⚠ 一列 + VS16 一列」= 两列，碰巧对；
- ✅（U+2705）、❌（U+274C）落在旧区间表外 ⇒ 各算一列，而终端按 emoji
  呈现占两列 ⇒ 状态列补白少一列，后面的列整体左移一列；
- 变体选择符 U+FE0F 自己还占了一列（旧表把 FE10–FE6F 记两列，FE0F 落在
  兜底分支记一列）。
复现（旧代码渲染 + 用终端真相回读）：状态列在中间、右侧还有一列时，最后一列
的视觉起点逐行是 [14 14 15 15] ⇒ 后两行错位一列。
改动：
- 新增 cli/width.go：列宽判定与 Unicode 18.0 的区间表（East Asian Width、
  emoji-data、emoji-variation-sequences、General_Category）搬到这里，表格本
  身只管排版。pkg 是零第三方依赖仓，宽度表只能自带。
- 判定口径改成按序列看：
  · 变体选择符 U+FE0F（VS16）把「窄的 emoji 字符」提升成两列（⚠ ⇒ ⚠️）；
    U+FE00–U+FE0E 不占列、也不改基字符宽度。
  · emoji 簇按一个字形算：基字符 + 修饰符（肤色、键帽、标签）+ ZWJ 链 ⇒ 两列；
    两个区域指示符配成的旗帜也是一个字形。
  · 零宽（Mn/Me/Cf/Cc/Zl/Zp、谚文组合型字母）不占列；East Asian Width 为
    W/F 与默认 emoji 呈现的码点占两列；其余一列。
- cli.Usage 的子命令名不再补尾随空格（没有摘要时直接输出名字）。
为什么不按 issue 字面建议把 U+2600–U+27BF 整块记两列：U+2713（✓）的 East
Asian Width 是 N，多数终端按一列渲染，整块记两列会把纯文本呈现的对勾算错。
本实现取 Unicode 权威语义（EAW + VS16 提升），与 wcwidth 0.9.2 在 VS16
（371 条）、ZWJ 序列（1614 条）、区域指示符对（676 组）上逐条一致。
测试：新增 cli/width_test.go，覆盖状态符号宽度、变体选择符、emoji 簇、零宽
字符，以及 #162 的可复现形态（含 emoji 状态列的表格必须对齐）。变异验证：
拆掉 VS16 提升 / EP 两列 / ZWJ 并簇 / 旗帜配对 / 肤色吸收 / 零宽表六处，
六种变异均被新断言抓住并转红。
Closes #162

- 文档：版本行对齐 v0.31.1（发版后的收尾）

pkg v0.31.1 已发布，README 安装行与 AGENTS.md 的「当前版本」行还停在 v0.31.0 ——
矩阵门禁的 refresh_versions 检查盯着这两行。
上游触发：#158（httpjson.DoJSON 对 out==nil 跳过响应体解码，不再把 2xx 误报失败）。


- 格式(cli)：ZWJ 字面量改用 \u200d 转义——过 staticcheck ST1018

## [v0.31.1] — 2026-11-02

### 修复(httpjson)：DoJSON 对 out==nil 跳过响应体解码，不再把 2xx 误报失败

out 为 nil 是 fire-and-forget 调用方的语义——只关心请求送达与状态码，不关心
响应体。此前 DoJSON 无条件 json.Unmarshal(raw, out)，out==nil 时恒定拿到
`json: Unmarshal(nil)` 的 InvalidUnmarshalError ⇒ 服务端已返回 2xx 的成功
请求被误报为失败，真失败反而淹没在假失败里。
下游实证：pigeonbox/core 的 webhook 推送渠道自 2026-10 起一直以 out=nil 调用
⇒ 每次成功送达都记一条 "webhook push failed" 假失败日志。
改动：
- DoJSON 在状态码检查之后、json.Unmarshal 之前加 out==nil 守卫 ⇒ 2xx 返回
  nil；非 2xx 的判定与 out 无关，照样返回 StatusError（错误体摘要照旧带上）。
- DoJSONWithRetry 经 DoJSON 透传，out==nil 时同款语义（2xx 即成功）。
- out 非 nil 时行为不变：解码失败仍返回「响应解析失败」错误。
测试：新增 5 组用例（2xx + 非 JSON 响应体、非 2xx 不得放过、重试语义不变、
表驱动矩阵覆盖 out==nil / out 非 nil 两态）。变异验证：删守卫 / 守卫挪到状态码
检查之前 / 守卫条件取反，三种变异均被新断言抓住并转红。
Closes #158

- 文档：版本行对齐 v0.31.0（发版后的收尾）

pkg v0.31.0 已发布，README 安装行与 AGENTS.md 的「当前版本」行还停在 v0.30.0 ——
矩阵门禁的 refresh_versions 检查盯着这两行。


## [v0.31.0] — 2026-10-29

### 新增(cli)：Table 支持无表头——形状由第一行决定

有些输出（工具目录）本来就没有列名，硬塞一个表头反而改变了它的形状。

### 新增(cli)：Report 补 Result 覆盖口——两个程序对「什么算坏了」的口径本就可能不同

措辞跟着口径走（有的把警告也算成「需要处理」，有的只算失败），所以结果行
要能被调用方覆盖；不覆盖时仍按 Status() 自动生成。

### 新增(cli)：子命令别名、默认子命令、内建 help/version 与两个渲染原语

三件不必每个消费方各写一遍的事：内建帮助（--help/-h/help 打印用法到 stdout、
退出码 0——问帮助不是错误）、内建版本（App.Version 非空时 --version/version）、
默认子命令（App.Default 非空时无参即执行它，适合以 stdio 提供服务、没人
传子命令名的进程）。另加 Command.Aliases 认别名（名字优先于别名）。
两个渲染原语：Table（列宽自适应，中文按显示两列算宽——按字节算会让中文列
宽出一倍）与 Report（doctor 式自检清单：状态标记定宽、名称列自适应、修复
建议缩进到名称列起点）。
默认行为不变：Default 为空则无参仍是用法错误，Version 为空则 version 仍是
未知子命令，既有消费方不受影响。

### 修复(cli)：Report 的 nil receiver 返回写 nil（gocritic nilValReturn），SetHeader 改名为 Header 且改为累积语义

SetHeader 原本是「替换」语义，调用方要写两行头部就得调两次、第二次把第一次
冲掉 ⇒ 改成 Header（累积追加）。nil receiver 的返回改成 nil（gocritic 要求
返回值不能是恒为 nil 的表达式）。

- 文档：AGENTS.md 首行 DESC 同步（DESC 口径 pkg 24 合 1）
- 文档：矩阵总览表同步（DESC 口径 pkg 24 合 1）
- 文档：AGENTS.md 首行 DESC 同步到矩阵真相源
- 文档：矩阵总览表同步到真相源（本轮发版后口径同步）
- 文档：AGENTS.md 首行 DESC 同步（pkg 24 合 1 + shellx 登记）
- 文档：矩阵总览表同步（pkg 24 合 1 + shellx 登记）

## [v0.30.0] — 2026-10-28

### 新增(convert)：补 nil 安全的可选转换入口——AnyToStr(nil) 的 "<nil>" 哨兵

Any* 一族把「转换不了」一律变成零值，而 AnyToStr(nil) 更是返回字符串
"<nil>"：它不是空串、不是零值、不是 error，而是一个**长得像值**的哨兵——
过得了任何 schema 校验（就是个 string）、被当成「这个字段有内容」送到
模型面前 ⇒ 模型拿到一个**不存在的名字**当作真实值。「没有这个字段」与
「这个字段的值是 <nil> 字符串」在下游必须分得开，但用 AnyToStr 之后分不开。
新增（与既有 Any* 并列，不改既有语义）：
- AnyToStrOr(v, def)：nil ⇒ def（默认 "" ⇒ nil 读成空串，而不是 "<nil>"）
- OptStr / OptBool / OptInt32 / OptInt64：**没有就是没有**——缺失或无法
  表示为目标类型时返回 nil 指针；不返回 &false / &0 那种「长得像值」的伪装
- AnyToInt32：补上 int32 这一档（JSON 解码出的数字一律是 float64，进
  int32 契约前必须转）；装不下按「无法转换」返回 0，**不发生回绕**
Refs: #143

### 修复(singleflight)：internal 侧清理改 CompareAndDelete，并与 concurrency 对齐语义

矩阵里有两套同源的 singleflight 实现：concurrency/singleflight（公开 API）
与 pkg/internal/singleflight（pkg 内部副本）。concurrency 侧**已修**「Forget
之后旧 call 的收尾误删新 call」，pkg 侧没跟进 ⇒ 两套语义不同的实现跑在
**同一套契约套件**上，给出「都通过了」的假保证。
pkg 侧的清理此前是无条件 Delete ⇒ Forget(key) 之后注册的同 key **新** call
会被一并删掉 ⇒ 第三个调用方另起一次 fn，「同 key 只执行一次 fn」的承诺被
打破。改为 CompareAndDelete（判同一，只删自己那条），与 concurrency 侧同一
口径。保留 pkg 侧原有的「先摘键、后唤醒」顺序（x/sync/singleflight 同款，
反过来会留一个「陈旧结果被当成新鲜结果返回」的窗口）。
契约套件补「Forget-竞态」用例：Forget → 新 call 仍在飞行 → 旧 call 收尾
→ 第三个调用方。旧套件只断言「Forget 后单次新 Do 会重跑 fn」，**不构造**
旧 call 与新 call 交叠的时序 ⇒ 该缺陷在套件里不可观测 ⇒ 两个语义已漂移的
实现都能通过。补上后两侧实现同时被钉住。
Refs: #147

### 修复(cache)：HashKey 的参数边界歧义 ⇒ 不同参数组算出同一个缓存 key

此前每个参数用 "%v:" 直接拼进 SHA-256 的摘要输入，参数值里的 ':' 与
「参数边界」不可区分 ⇒ 语义不同的参数组算出**完全相同**的 key：
  HashKey("p", "a", "b")   == HashKey("p", "a:b")
  HashKey("p", "a:b", "c") == HashKey("p", "a", "b:c")
  HashKey("p", 1)          == HashKey("p", "1")
三组碰撞全部命中（第三条是 int 与 string 互撞）。故障形态最坏：静默——
没有报错、没有 panic，只是命中率异常地高；越权面——租户名/用户 ID/关键词/
路径进参数时就是跨租户串数据；难复现——只有参数含 ':' 才触发，而测试
数据通常不带。
改用**长度前缀的无歧义编码**（类型名长度:类型名 + 值长度:值）⇒ 参数边界
与值内容不再混淆，类型不同的参数组（1 与 "1"）也不同键。
key 为进程/缓存生命周期内临时值、无持久化契约 ⇒ 换编码不构成破坏性变更；
但存量 key 的**取值会变**（升级后旧缓存条目不再命中，属预期内的失效）。
补碰撞用例把「不同参数组 ⇒ 不同 key」钉成契约。
Refs: #146

- 杂务：CHANGELOG 补 v0.30.0 + 版本引用对齐

- 文档：版本引用对齐 v0.29.1

## [v0.29.1] — 2026-10-28

### 修复(pkg)：修正 5 处包级语义缺陷（切片滞留 / 标志输出 / nil 父 ctx / 枚举覆盖 / 记录数口径）

- container.Slice: Delete/Clear 缩容时把底层数组的内容清零，避免大对象滞留
  （此前只改 len，GC 收不走被删元素）。
- cli.NewFlagSet: stderr == nil 时改写 io.Discard；此前不调 SetOutput 会让
  标准库默认写 os.Stderr，错误信息与用法逃出调用方给的输出流。
- contextx.ValueOnlyFrom: parent == nil 立即 panic；此前 nil 会延后到某次
  Value 调用才炸（堆栈离「传错了」很远）。
- enumx.NewRegistry: 重复 Value() 立即 panic；此前后者静默覆盖前者
  （连 labels 一起覆盖）⇒ 取到哪个枚举取决于传入顺序。
- pack: 摘要清单的「条数」改为**非空行数**，与写入侧（每条一行）和读取侧
  （ReadJSONL 跳过空行）同一口径；此前数物理行数，含空行的包会被误报
  「条数不符」，而读出来的条数恰好等于清单里的数。
每项都配了回归用例（修前红、修后绿），并经变异验证。

- 文档：版本引用对齐最新 tag（refresh_versions）
- 文档：版本引用对齐 v0.29.0

## [v0.29.0] — 2026-10-28

### 修复(container/slicex)：零值可用性与别名语义的口径与同包其它 API 不一致

(cherry picked from commit 7545a0791f8c9430244ddaf10d37d32e202c40e0)

### 修复(timex/cron)：月份扫描跳过目标月 —— 下次触发晚一年

## 为什么
cron 是调度底座，月份算错一次就是「月度/年度任务晚一年才跑」。
### 1. 月份扫描跳过目标月（严重）
月份推进的顺序是「**先** AddDate(0,1,0) 加一个月，**再**复位到 1 号」：
    t = t.AddDate(0, 1, 0)                                  // 1/31 + 1 月 → 归一化成 3/3
    t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc) // 复位到 3/1 —— 2 月被整个跳过
AddDate 在「当前日 > 目标月天数」时会溢出到**再下个月**（1/31 + 1 月 = 3/3），
随后的「复位到 1 号」就锁死在溢出后的月份 ⇒ 目标月被跳过。
实测（修前）：
    cron "0 0 0 1 2 *" 从 2025-01-31 → 2026-02-01   （期望 2025-02-01，晚一年）
    cron "0 0 0 1 4 *" 从 2025-03-31 → 2026-04-01   （期望 2025-04-01，晚一年）
月底重启的月度/年度任务最常见（1/31 起算的「每月 1 号」正好踩中）。
修法：**先归一到当月 1 号再加一个月**——1 号不存在「溢出到下个月」的余地，
加出来就是下个月的 1 号。
### 2. "a-b/n"（区间带步长）被拒，对应分支是死代码
parseRange 先 parseUint(parts[1]) 整体解析上界，而 parseUint 用
strconv.ParseUint 全串严格校验 ⇒ "1-5/2" 的上界是 "5/2" ⇒ 直接报错
invalid ... field: parsing "5/2" ⇒ 后面那段处理步长的代码永远到不了。
修法：上界**先按 "/" 切分再分别解析**，步长分支不再可达性为零。
### 3. NextSchedule(n <= 0) panic
make([]time.Time, 0, n) 遇负数直接 panic（makeslice: cap out of range）。
n 来自配置或算术时（count - 1 之类）就是一次进程崩溃。
修法：n <= 0 返回空切片，不做任何调度计算。
## 用例
新增 timex/cron/month_step_test.go：
- TestSpecSchedule_Next_MonthNotSkipped：5 个用例钉住「不跳月」——2 月、4 月、
  跨年（12→1）、闰年 2/29；期望值按 time.Local 构造（Parse 产出的
  Schedule 用 Local）。
- TestSpecSchedule_Next_NoSuchDate：「2 月 30 日」不存在 ⇒ 返回零值时间
  （既有契约，不受本次修复影响）。
- TestSpecSchedule_Next_MonthNotSkipped_LongRun：连续 12 个月逐月推进，
  每月都命中一次。
- TestParse_RangeWithStep：1-5/2 ⇒ 小时位含 1/3/5、不含 0/2/4；
  0-6/3 ⇒ 0/3/6；0-10/5 ⇒ 分钟位 0/5/10。
- TestParse_RangeWithStep_Invalid：步长为 0 / 非数字 / 空 仍报错。
- TestNextSchedule_NonPositiveN：n = -1 / -100 / 0 返回空切片、不 panic。
## 影响面
- 月度/年度 cron 的下次触发时间在「月底起算」时由「晚一年」变为「下个月」。
  依赖旧（错误）行为的部署会看到触发时间提前——那本来就是修 bug。
- a-b/n 表达式由「解析失败」变为「按步长解析」。
- NextSchedule 负数 n 由 panic 变为空切片。
- 无签名变更。
## 自检
go build / go vet / go test -race / golangci-lint 全绿。
(cherry picked from commit 93ed6375ec914912bbcb5569cf03984e37d2af02)

### 修复(pack)：重名 zip 条目未被检出 + 数据条目无解压上限（zip bomb）+ JSONL 单行无上限

pkg/pack 是迁移包容器（zip + manifest + JSONL）。体检（Inspect）是它的安全门
——「这个包能不能信」全靠它。本轮实测出三处缺口。
### 1. zip 内**同名条目并存**不被报出 ⇒ 未校验的副本可以混进包里
indexEntries 对同名条目只保留**第一个**：
    for _, f := range zr.File {
        names = append(names, f.Name)
        if _, ok := entries[f.Name]; !ok {
            entries[f.Name] = f      // ← 同名的第二份被丢弃
        }
    }
而 verifyEntries 的摘要/条数核对只针对索引里的那一份；「清单未记录的文件」那一
轮检查按**名字**判断——重名条目在清单里 ⇒ 被跳过 ⇒ **完全不报**。
后果：包里可以放一份「声明过的文件名 + 任意内容」的**第二份**副本，它不被任何
检查覆盖；而**按 zip 条目遍历**（而不是按清单）的消费方读到的正是这份未校验
的副本。
修法：indexEntries 一并返回重名条目名，verifyEntries 把它们报成问题（清单
自身重名同样报）。
### 2. 数据条目**没有解压上限** ⇒ zip bomb
清单有 maxManifestSize 保护，但**数据条目没有**——digestAndCount 要把每个声明
的条目完整解压一遍算摘要与行数：
    for {
        n, err := rc.Read(buf)
        ...
    }
一个「几 KB 的 zip、条目声明 1TB」的包能让体检无限期地烧 CPU。
修法：
- 预检 f.UncompressedSize64（声明大小）超过 maxEntrySize（1GiB）直接拒绝；
- 实际解压时用计数器硬卡——**声明大小可以撒谎**，所以预检之外必须有实际量的
  上限。
### 3. ReadJSONL 的单行长度无上限 ⇒ 一行就能吃满内存
    line, err := br.ReadBytes('\n')
bufio.Reader.ReadBytes 为超长行**无限增长**（bufio 的 4KB 缓冲不是上限）⇒ 一个
「一行几百 MB」的条目就能把内存吃满。
修法：改用 ReadSlice + 手动拼接 + 超限立即停手（单行上限 8MB）。
## 用例
新增 pack/security_test.go：
- TestInspect_DuplicateEntry_Reported：数据文件重名必须被报出。
- TestInspect_DuplicateManifest_Reported：清单自身重名同样要报。
- TestDigestAndCount_SizeLimit：声明解压大小超限的条目**直接被拒绝**（不实际
  解压）。
- TestReadJSONL_LineTooLong：超长行报错、且错误信息说明是上限问题。
- TestReadJSONL_NormalLinesUnaffected：正常行照旧能读（空行仍跳过）。
- TestReadJSONL_CallbackErrorPreserved：回调错误原样带回（errors.Is 仍可判定，
  既有口径）。
(cherry picked from commit 253ef4c6c0726c7146e5d372567ce9c74c0396e3)

### 修复(timex)：夏令时窗口内的边界不变量不成立（EndOfHour 早于输入 / BeginningOfDay 落到前一天）

pkg/timex 的时间段边界函数有两条基本不变量：
    BeginningOfX(t) <= t <= EndOfX(t)
    BeginningOfX(t) 与 t 落在同一个墙钟周期内
在夏令时切换窗口里，这两条都不成立——而调用方（按日/按小时切片、账期统计、
日志归桶）恰恰会在那些时刻用到它们。
### 1. 回拨（同一墙钟小时出现两次）⇒ EndOfHour 落在输入**之前**
EndOfHour 此前是 BeginningOfHour(t).Add(time.Hour - time.Nanosecond)，而
BeginningOfHour 是「按墙钟重建」：
    time.Date(y, m, d, t.Hour(), 0, 0, 0, t.Location())
Go 的 time.Date 对**歧义**墙钟时间**一律解析到较早的那一次** ⇒ t 落在较晚那次
时（如 America/New_York 回拨日的第二个 1:30 = 06:30 UTC），算出的起点是第一次
1:00（05:00 UTC）⇒ 区间 [Begin, End] 整个落在 t **之前**。
实测（修前）：EndOfHour(第二个 1:30) = 05:59:59.999999999 UTC < t = 06:30 UTC。
### 2. 前跳（某个墙钟午夜不存在）⇒ BeginningOfDay 落到**前一天**
America/Santiago 的 2026-09-06：00:00 直接跳到 01:00（该日只有 23 小时）。
Go 的 time.Date 对不存在的墙钟时间归一到**前一天**的同一时刻：
    time.Date(2026,9,6,0,0,0,0,America/Santiago) == 2026-09-05 23:00 -04:00
实测（修前）：BeginningOfDay(2026-09-06 10:00) = **2026-09-05 23:00**（日=5！）
——按天切片的调用方会把这一天的数据归到前一天。
## 修法
两类墙钟歧义各配一个解析原语：
- **wallHourStart**：起点 + 一个周期后若仍是**同一墙钟小时** ⇒ 该小时重复了；
  此时若 t 已落在后一次，起点取后一次。分钟边界建立在其上（wallMinuteStart）。
- **dayStart**：在当天 00:00-23:00 的墙钟小时里找**第一个存在**的那个 ⇒ 午夜
  不存在时返回该日首个存在瞬间（通常 01:00）。
BeginningOfX/EndOfX 全部改为走这两个原语；EndOfX = 下一周期起点 - 1ns 的定义
保持不变。
## 用例
新增 timex/boundary_dst_test.go：
- TestBoundaries_DSTFallBack_BeginningBeforeT_EndAfterT：回拨日的两次 1:30
  （EDT 05:30 UTC / EST 06:30 UTC）都满足两条不变量；且起点与 t 的距离 ≤ 1
  小时（修前差 1.5 小时）。
- TestBoundaries_DSTFallBack_Minute：回拨小时内的分钟边界同理。
- TestBoundaries_DSTSpringForward_BeginningOfDaySameDate：午夜不存在的那一天，
  BeginningOfDay 必须落在**同一个日期**（修前是前一天），EndOfDay 也必须在当天。
- TestBoundaries_NonDST_Unchanged：非 DST 时区的既有取值逐字不变（回归护栏）。
- TestBoundaries_Invariant_AllFunctions：5 个关键时刻（含前跳日、回拨日、午夜不
  存在的日、跨年首尾）上，7 个边界函数全部满足 BeginningOfX(t) <= t <= EndOfX(t)。
## 影响面
- 夏令时窗口内的边界取值变了（此前是错的）：BeginningOfHour 在 t 落在回拨日的
  较晚那次时，返回该次的小时起点（此前返回较早那次）。
- **非 DST 时区（含全部 UTC 与固定偏移时区）的取值逐字不变** ⇒ 绝大多数部署
  无感知。
- 无签名变更。
(cherry picked from commit d55e09427131d71a6e7825beb463c2b397f5c992)

### 修复(singleflighttest)：契约套件的等待无界（缺陷实现挂死 CI）+ 漏了 key 释放不变量

pkg/singleflighttest 是 pkg/internal/singleflight 与 concurrency/singleflight
**共用**的契约套件——它定义了「什么样的实现算合格的 singleflight」。契约本身有
缺陷时，两侧实现都会跟着「合格」。
### 1. 实现有缺陷时，契约套件会**挂死**而不是干脆失败
契约套件里所有「等实现进入某个状态」的等待都**没有上界**：
- runForget：for calls.Load() == 0 { time.Sleep(5ms) } —— 实现若永远不调 fn
  ⇒ 无限轮询；
- runDifferentKeys：for running.Load() < 2 { time.Sleep(5ms) } —— 同上；
- runWaiterCtxCancel：f.Do(wctx, …) 直接在测试 goroutine 上阻塞 —— 实现若
  **忽略调用方 ctx**，Do 一直阻塞，而 block 只在 Do 返回后才关 ⇒ **死锁**。
后果：一份有缺陷的实现让 CI 挂到 go test 的 10 分钟超时（栈里只有一个 goroutine
卡在 Do），而不是在几秒内给出「waiter 未响应 ctx 取消」这种能定位的失败。
修法：所有等待改为**有界**（2s 上限 + 明确失败原因）；runWaiterCtxCancel 的 Do
放进 goroutine 并有界等待。
**变异验证**（internal/singleflight/contract_broken_test.go）：用一个「waiter 忽略
ctx」的缺陷实现在**子进程**里跑契约套件——
    修前：挂死 → 子进程 60s 超时 → 变异测试 FAIL（实测 60.01s）
    修后：2.07s 内干脆失败 → 变异测试 PASS
### 2. 契约清单漏了一条可观测的关键不变量
飞行结束后，key 必须已释放——否则后续调用会复用上一次的 call、拿到**陈旧结果**
而不执行 fn。契约清单里没有这一条。
修法：新增 AfterFlightNewCallRerunsFn —— 连续 3 次 Do（前一次已结束）必须执行
3 次 fn。
### 关于「先摘键后唤醒」这个窄窗口
pkg/internal/singleflight 的实现刻意是「**先摘键、后唤醒**」（#124）——反过来会留
一个窗口：新到的调用命中**已结束**的那次 flight、拿到上一次的陈旧结果。
这个窗口**通过公开 API 无法确定观测**：并发到达的调用方拿到上一次结果是**合法的
去重**（实测：两种实现下并发到达的调用方都拿到上一次的结果，无法区分）。所以契约
套件只能钉住**可观测**的性质（飞行结束后新调用重新执行 fn），窄窗口由实现仓自己
守。
### 3. runPanic 的固定 sleep
time.Sleep(20ms) // 等待进入 LoadOrStore waiter 分支 是启发式等待——机器负载高时
waiter 可能还没挂上就被放行，于是它自己开一次新 flight、触发 t.Error("waiter
不应执行 fn") ⇒ **偶发失败**。
修法：等待改为**有界轮询 + 明确原因**（waitFor），放行前额外确认所有 waiter 都
已发出信号。
## 用例
- internal/singleflight/contract_broken_test.go → TestContract_FailsFastOnBrokenImplementation：
  子进程跑契约套件对缺陷实现，期望**几秒内失败**（不是挂死）。
- 契约套件新增 AfterFlightNewCallRerunsFn（飞行结束后新调用必须重新执行 fn）。
## 影响面
- 契约套件更严：此前能「通过」的部分缺陷实现现在会失败（这正是契约的目的）。
- 契约套件的等待全部有界 ⇒ CI 不再被挂到 10 分钟超时。
- 无签名变更（Run 的子用例集合变多了——这是新增覆盖，不是行为变化）。
(cherry picked from commit 78fcd289f9fadc00d4a9f52f0154f46b3f577989)

### 修复(pathx)：截断时留下半个扩展名 —— 正是本函数文档禁止的形态

回退分支截断的是**整个名字** s（含扩展名）⇒ 砍进扩展名：
    SanitizeFileName("中.txt", WithMaxFileNameLength(5)) == "中.t"
而函数自己的文档写着「扩展名自己都装不下时**丢弃扩展名**、只截主名：留半个
扩展名（a.tx）比没有扩展名更糟——扩展名是骗人的，下游按它判类型会判错」。
改法：预算连一个完整 rune 都放不下时**丢弃扩展名**、只留按总预算截断的主名
（truncateFileNameBytes(stem, maxBytes)），而不是整体截断 s。
"中.txt" + 上限 5 字节 ⇒ "中"（无扩展名），不再是 "中.t"。
用例新增 pathx/extension_test.go：
- TestSanitizeFileName_NoHalfExtension：不得得到 "中.t"；结果的扩展名要么是
  完整的 .txt、要么为空；结果不得是空串。
- TestSanitizeFileName_LongExtensionDropped：扩展名超限时丢弃扩展名（回归护栏）。
- TestSanitizeFileName_KeepsFullExtensionWhenItFits：预算够时保留完整扩展名。
(cherry picked from commit 2986edcb45b53410d85d55b374af9a07e9d4e623)

### 修复(pagination)：偏移量溢出保护只兜住一半情形 ⇒ 返回垃圾偏移量

原判据是「乘法后 offset < 0 ⇒ 回退 0」，但 int 溢出**回绕**后的结果有一半是
**正数**——那些被直接放过去，返回的是一个完全错误的偏移量。
实测（真值用 math/big 精确计算）：
    page=1<<62     pageSize=500  真值 2.3e18（超 int64）  修前 0    ✓
    page=1<<62 + 2 pageSize=500  真值 2.3e18（超 int64）  修前 500  ✗
    page=3<<54     pageSize=500  真值 2.7e19（超 int64）  修前 8.5e18 ✗
    page=1<<60     pageSize=500  真值 5.8e20（超 int64）  修前 4.6e18 ✗
page 来自 query:"page" / form:"page"（**外部可控**）⇒ 不能靠「回绕后是否为负」
兜底。
改法：**乘法前**判（page-1 > math.MaxInt/pageSize ⇒ 回退 0），保留原有的
offset < 0 兜底。同一处缺陷在 Policy.Offset（服务级口径入口）——一并修。
用例新增 pagination/overflow_test.go：
- TestPageRequest_Offset_OverflowBeforeMultiply：6 个溢出 page 值必须回退 0；
  正常值 Offset(3,20)==40 不受影响。
- TestPolicy_Offset_OverflowBeforeMultiply：同样的保护覆盖 Policy.Offset。
- TestPolicy_Offset_UnlimitedMaxPageSize：MaxPageSize:-1 口径下同样不得溢出。
(cherry picked from commit a494680da2d4e179992eb605df3b957afe16fff6)

### 修复(container)：SafeMap.Range 持锁调回调 ⇒ 回调里写同一个 map 死锁

Range 遍历期间**一直持着读锁**（RWMutex 不可重入）：
- 回调里写同一个 map 会**死锁**：回调的 Set 要写锁、写锁等读锁释放、读锁等
  回调返回 ⇒ 互等永久挂起（实测确认）。
- 文档承诺的「遍历期间其他 goroutine 的写入不阻塞」并不成立：Set（写锁）会被
  读锁挡住直到遍历结束。
改法：先在读锁内取快照（键 + 值），再在**锁外**遍历——与同包 Slice.Range 同构。
快照语义不变：遍历期间的并发写入不影响本次遍历。
用例新增 container/safemap_range_test.go：
- TestSafeMap_Range_CallbackMayWrite：回调里写同一 map 不得死锁（3s 超时护栏）。
- TestSafeMap_Range_ConcurrentWriteNotBlocked：遍历期间的并发写入不被读锁挡住。
- TestSafeMap_Range_ConcurrentWriteSafe：并发写 + 遍历在 -race 下无 data race。
(cherry picked from commit 1e06a469cab282f17aaa1089bc77a85a5f54922b)

### 修复(mathx)：概率路由选错元素（band 顺序）/ 最后一个桶永远选不到 / 统计函数返回 NaN

## 为什么
pkg/mathx 是矩阵内唯一的统计/数学工具集。本轮实测出 8 处缺陷，其中 3 处会让
调用方**静默拿到错误结果**——正是本仓最忌讳的一类。
### 1. ProbabilityElems 排序后丢弃了置换关系 ⇒ 概率路由选错元素（严重）
    sorted := make([]float64, len(elems))
    copy(sorted, elems)
    sort.Float64s(sorted)      // ← 排序副本
    ...
    band[i] = band[i-1] + int(sorted[i]*p)   // ← band 按**排序后**的顺序建立
Probability(band) 返回的是**排序后**顺序里的下标，而调用方拿它去索引**自己的**
elems ⇒ 元素与权重被静默置换。
实测（修前）：elems=[0.6,0.1,0.3] ⇒ band=[10,40,100]（对应排序后的
[0.1,0.3,0.6]）——下标 0 实际指向权重 0.6 的元素、却只分到了 0.1 的份额。
修法：band 按**调用方给定的顺序**建立（不再内部排序），精度基数改用
min(elems) 推导（原先用 sorted[0]，作用相同）。
### 2. Probability 的比较差一 ⇒ **最后一个桶永远选不到**
    for i, v := range band {
        if r <= v {   // ← 应为 r < v
            return i, r
        }
    }
band[i] 是「累计到 i 为止」的**上界**，元素 i 的份额是 [band[i-1], band[i])。
用 <= 会把边界值算给前一个桶 ⇒ 第一个桶多一个单位、最后一个桶少一个。
实测（修前）：band=[1,2,3] 时下标 2 **一次都没被选中**（分布 2/3、1/3、0）；
band=[1,2] 时下标 1 一次都没被选中。
修法：改用 r < v。
### 3. 非法输入静默退化成「恒选 index 0」
包自己的注释写着「概率路由静默出错比快速失败更危险」，但只有 sorted[0] <= 0
这一种输入会 panic：
- precise < 1 ⇒ 10^ceil(log10(precise/minW)) < 1/minW ⇒ 最小权重分不到任何
  一个单位 ⇒ band 全 0 ⇒ Probability 恒返回 index 0。**仓里自己的测试就在用
  precise=0.1**，且只断言 len(band)==3 ⇒ 退化状态照样通过，该用例钉不住任何
  东西。
- 权重为 NaN ⇒ NaN 比较恒为 false ⇒ 绕过 <= 0 检查。
修法：precise < 1 / NaN 权重 / 非有限权重一律 panic；另加一道「band[0] <= 0
⇒ 退化」的兜底 panic。
### 4. DivisionN 经 int64 中转 ⇒ 大数回绕
float64(int64(x)) 在 x 超出 int64 范围时**回绕**（Go 规范未定义该情形，
amd64/arm64 实测得到 -9223372036854775808）。实测（修前）：
DivisionN(1e18, 1, 6) = 9.22e12（期望 1e18）；DivisionN(1e300, 1, 0) = 9.22e18。
修法：截断改用 math.Trunc（同样向零截断，但没有范围限制）。
### 5. McNemarExact 大 n 返回 NaN
直接累加组合数 C(n,i) 会溢出（C(1200,600) ≈ 1e360 > float64 上限）⇒ 累加和
与 2^n 同时成 +Inf ⇒ Inf/Inf = NaN ⇒ **返回 NaN**。实测（修前）：
McNemarExact(600,600) = NaN；n=1024 时分母为 Inf ⇒ p 被静默压成 0（「高度
显著」）。
修法：在**相对权重**空间递推（以众数 floor(n/2) 的权重为 1，向两侧递推），
权重和 = 2^n / C(n,mode)（n=1200 时约 10^2 量级，float64 装得下）⇒ 全程不碰
会溢出的组合数。k >= mode 时用对称性换到下侧算，数值更稳。
### 6. WilsonCI 越界计数 ⇒ NaN 区间
passed 超出 [0,total] ⇒ p 落到 [0,1] 之外 ⇒ p*(1-p) < 0 ⇒ math.Sqrt(负数)
= NaN ⇒ 返回 (NaN,NaN)。实测（修前）：WilsonCI(5,3,1.96) = (NaN,NaN)。
修法：计数钳到 [0,total]；total <= 0 返回零区间；结果为 NaN 时返回零区间。
### 7. toFixed 吞掉 ParseFloat 错误 ⇒ 负精度静默返回 0
%.−1f 产出 "%!.-1f(...)"（非法浮点字面量）⇒ ParseFloat 失败 ⇒ 错误被 _ 吞掉
⇒ 返回 0。实测（修前）：ToFixed(3.456,-1) = 0、Round(3.456,-1) = 0。
修法：负精度 panic（与本包「静默错值不如快速失败」一致）；ParseFloat 出错时
不再吞错。
### 8. NewUnstable(NaN) ⇒ 垃圾抖动值
NaN 与任何值比较都是 false ⇒「夹到 [0,1]」的承诺落空 ⇒ NaN 一路传下去 ⇒
int(NaN) 得到实现相关的垃圾整数（TTL 抖动场景下就是垃圾 TTL）。实测（修前）：
NewUnstable(NaN).Around(10) = 0。
修法：NaN 偏差夹到 0（恒等抖动）。
## 用例
新增 mathx/probability_stats_test.go：
- TestProbabilityElems_BandFollowsCallerOrder：band 与 elems 同序（修前为
  [10,40,100]，期望 [60,70,100]）；band 严格递增。
- TestProbability_AllBucketsReachable：每个桶都能被选中，且均匀权重下大致
  等分（修前最后一个桶**一次都没被选中**）。
- TestProbabilityElems_InvalidInputPanics：零权重/负权重/precise<1/
  precise=NaN/NaN 权重 一律 panic。
- TestDivisionN_NoInt64Overflow：大数不回绕（1e18 → 1e18）。
- TestToFixed_NegativePrecisionPanics：负精度 panic，不静默返回 0。
- TestMcNemarExact_NoNaNForLargeN：大 n 不返回 NaN；量级合理；小 n 经典值
  McNemarExact(5,0)==0.0625。
- TestMcNemarExact_NegativeCountsTreatedAsZero：负数计数按 0 处理。
- TestWilsonCI_NoNaNForOutOfRangeCounts：越界计数不产出 NaN；区间落在 [0,1]。
- TestNewUnstable_NaNDeviation：NaN 偏差被夹住（恒等）。
既有 TestProbabilityElems（math_test.go）同步改正：原用例传 precise=0.1（退化
输入）且只断言 len(band)==3，钉不住任何东西；改为 precise=1 并断言 band 的
**具体取值**。
**新旧 McNemar 一致性**：0..12 全 169 组 (n01,n10) 组合上新实现与旧实现（小 n
不溢出）逐位一致 ⇒ 证明重写没有改变既有语义。
## 影响面
- ProbabilityElems 返回的 band **取值变了**（按调用方顺序而非排序顺序）——这是
  修正：原返回值无法用于索引调用方的 elems。org 内无消费方（全仓 grep 无
  ProbabilityElems 调用）。
- Probability 的选中分布变了（原先最后一个桶选不到）。
- ProbabilityElems 对 precise < 1 由「静默退化」改为 panic；toFixed 对负精度
  由「静默 0」改为 panic。
- DivisionN / McNemarExact / WilsonCI 在大数/越界输入下由「回绕值 / NaN」变为
  正确值。
- 无签名变更。
## 自检
go build / go vet / go test -race -count=1 / golangci-lint 全绿。
(cherry picked from commit db8f53602bd288de3b8c1f2915761e8a1b27582a)

### 修复(bufiox)：ReadBinary 直读路径漏记 ReadLen + rdErr 粘滞终态错误未实现 + 倍增溢出死循环

## 为什么
pkg/bufiox 是「用户态零拷贝缓冲读」的底座（协议帧解析、连接读取都走它）。
本轮实测出 3 处缺陷。
### 1. ReadBinary 直读路径漏记 ReadLen
接口约定（bufiox.go）：
    ReadLen 返回自上次 Release 以来累计消费的字节数（Next/ReadBinary/Skip 累加，
    Release 归零）。
但 ReadBinary 的「直读」路径（缺口 ≥ 4KiB，直接读进调用方切片省一次中转拷贝）
在 return n, err 之前**漏了 r.rn += n**：
        if need >= directlyReadThreshold {
            nn, err := readAtLeast(r.rd, bs[n:], need)
            r.rdErr = err
            n += nn
            return n, err          // ← r.rn 没加
        }
实测：ReadBinary 读 8192 字节后 ReadLen() 返回 **0**（期望 8192）。走这条路径
的每一次调用都少算——ReadLen 被用来算「这条连接读了多少」时，数字直接是错的。
修法：直读路径补 r.rn += n（部分读同样计入——字节确实被消费了）。
### 2. rdErr 只写不读 ⇒ 文档承诺的「粘滞终态错误」根本没实现
    rdErr error // 终态错误（读错误/io.EOF），粘滞到下次 Release
rdErr 在 4 处被赋值、**从未被读取**。而 Release 的文档写着「终态读错误不重置
（下一次操作仍返回，粘滞语义与上游一致）」——这层语义完全没有兑现：每次操作
都重新读底层 reader（对端已断开时白白多一次 syscall），语义上也没实现
bufio.Reader 的「终态后不再返回任何数据」。
修法：让终态错误真正粘滞——
- acquire 在终态错误已存在时直接返回它，不再碰底层；
- 直读路径（ReadBinary / Read / Skip）同样先挡终态错误；
- **缓冲里已有的数据仍照常交付**（与 bufio 一致）：粘滞只挡「还要更多数据」的
  那一步，不挡「把已读到的数据交出去」；
- Release 清掉终态错误——下一个读取周期重新感知底层状态（与 bufio.Reset 同
  口径，也与接口文档「Release 释放缓冲并重置状态」一致；Release 原注释里
  「终态读错误不重置」与字段注释「粘滞到下次 Release」互相打架，本 PR 统一
  为后者）。
### 3. 极大长度请求让倍增循环溢出成死循环
acquire 按 size *= 2 倍增找容量，而 size 是 int：
    size := r.bufSize
    for ; size < n; size *= 2 {
    }
请求量超过 2^62 时倍增溢出成负数（再翻倍回到 0），size < n 永远成立 ⇒
**无限循环**。实测 Next(1<<62+1) 永久挂起（goroutine 死等，且不会有任何分配）。
DefaultWriter.acquire 是同型问题（Malloc 同款挂死）。
修法：倍增循环加上限（maxBufferSize = 1GiB），超限返回明确错误 ErrTooLarge，
不进循环。
## 用例
新增 bufiox/robustness_test.go：
- TestReadBinary_ReadLenCountsDirectPath：直读路径读 8192 字节 ⇒
  ReadLen()==8192（**修前为 0**）。
- TestReadBinary_ReadLenCountsPartialDirectPath：直读路径**部分读**（遇 EOF）
  时读到的 5000 字节同样计入。
- TestDefaultReader_StickyTerminalError：终态后连续 3 次 Next 都返回错误，
  且底层 reader 的 Read **调用次数不再增加**（**修前每次都重读**）。
- TestDefaultReader_ReleaseClearsTerminalError：Release 后终态错误清掉，
  再次读会重新碰底层。
- TestDefaultReader_HugeRequestDoesNotHang / ..._ReturnsError：
  Next(1<<62+1) 不得挂起（3s 超时护栏），且返回 ErrTooLarge。
- TestDefaultWriter_HugeRequestDoesNotHang：Malloc(1<<62+1) 不得挂起。
既有的 TestDefaultReader_StickyError / TestDefaultWriter_StickyError 继续全绿。
## 影响面
- ReadLen() 的数值此前在直读路径下完全不准，现在与接口约定一致。**依赖旧
  （错误）数值的统计代码会看到数字变大**——那本来就是修 bug。
- 终态错误变为粘滞：底层断开后，后续读不再触发额外 syscall。对端「断开后又
  恢复」的非常规 reader 行为会变（终态即终态，与 bufio 一致）。
- Release 现在会清掉终态错误（此前该字段根本没人读）。
- 新增导出错误 ErrTooLarge；超出 1GiB 的单次请求改为明确报错，不再挂死。
- DefaultWriter.acquire 改为返回 error（**包内私有方法**，不影响外部 API）。
## 自检
go build / go vet / go test -race -count=1 / golangci-lint 全绿。
(cherry picked from commit 86985c3340b0bf3f357e0d3d387f11eddf9245d8)

### 修复(jsonrepair)：截断数组的元素串被误判成键位 ⇒ 闭合出非法 JSON

## 为什么
jsonrepair 的宽松模式（UnmarshalLenient / WithAllowPartialArray）要在「原文被
截断」时尽力闭合出**合法** JSON。它对「引号是不是键位」的判定只看引号前面那个
非空白字节：
    keyPos = lastStruct == '{' || lastStruct == ','
但**逗号既可能是对象的键值分隔符，也可能是数组元素的分隔符**。数组里第二个元素
串前面正是 ',' ⇒ 被误判成键位 ⇒ EOF 闭合时补 ":null" ⇒ 产出 ["a","b":null]
这种**非法** JSON ⇒ 宽松解析整体失败。
实测（修前）：
    UnmarshalLenient(`["a","b`)      → err「宽松模式无法闭合出合法 JSON」
    UnmarshalLenient(`{"a":["x","y`)  → err「宽松模式无法闭合出合法 JSON」
即：LLM 返回的 JSON 数组被截断、且元素是字符串时，这个「专治截断」的修复器
反而救不回来——而数组元素是字符串恰恰是 JSON 数组最常见的形态。
修法：判据从「前面那个字节」改成「**开口时所处的括号上下文**」——closePartial
本来就在维护括号栈，栈顶是 '{' 才是对象内（键位），栈顶是 '[' 是数组内
（值位）：
    keyPos = lastStruct == '{' ||
        (lastStruct == ',' && len(stack) > 0 && stack[len(stack)-1] == '{')
## 用例
新增 jsonrepair/array_partial_test.go：
- TestClosePartial_ArrayElementStrings：4 个用例钉住闭合结果必须是**合法**
  JSON——["a","b → ["a","b"]、{"a":["x","y → {"a":["x","y"]}、
  ["a","b","c → ["a","b","c"]、[{"a":1},"b → [{"a":1},"b"]。
- TestUnmarshalLenient_TruncatedArrayOfStrings：从解析入口看，三种截断数组
  必须能宽松解析出来（partial=true、无错）。
- TestUnmarshalLenient_TruncatedObjectKeyStillKey：回归护栏——**对象里的键位
  截断**仍要补 :null（{"a":"b","c → {"a":"b","c":null}、{"Host → {"Host":null}、
  {"a":1,"b → {"a":1,"b":null}），不能被本次修复带偏。
- TestClosePartial_ArrayElementAfterCommaNotKey：直接钉住判定口径——数组里
  逗号之后的字符串**不是**键位。
## 影响面
- 截断数组（元素是字符串）此前一律解析失败，现在能闭合出来。
- 对象语义、键位补 :null 的行为完全不变。
- 无签名变更。
## 自检
go build / go vet / go test -race -count=1 / golangci-lint 全绿。
(cherry picked from commit 13a05d40d215ed6960c308235ad8a09ee7db69e8)

### 修复(httpjson)：DoRaw 静默截断（文档承诺不截断）+ 响应体未排空致 keep-alive 失效

## 为什么
### 1. DoRaw 静默截断 —— 调用方无法区分「半截报文」与「完整报文」
DoRaw 的文档同时写了两句互相打架的话：
    DoRaw 执行请求并返回**原始**状态码与响应体：不判 2xx、**不截断报文**、不解 JSON。
    ……
    limit 是读体上限（字节）：<=0 时用包级 maxBody。调用方应按自己的报文体量
    给一个够用又防内存放大的值（如 8KB）。
实现是 io.ReadAll(io.LimitReader(resp.Body, limit))——超过 limit 的部分**直接
丢弃**，且**没有任何标记**。调用方拿到的 body 与完整报文无法区分：
- 拿半截 JSON 去解析 ⇒ 得到一个指向报文中间的错误位置，极难定位；
- 拿半截报文进错误文案 ⇒ 服务端的说明文字被截掉，排查信息丢失。
「不截断报文」这句本身也是错的（limit 就在下一段），属于典型的**文档与实现
打架**。
修法：
- DoRaw 的返回值改为 RawResponse{StatusCode, Body, Truncated}——**截断如实
  标记**，调用方据此判断拿到的是完整报文还是半截。
- 实现上多读一个字节（limit+1）来判断是否超限；limit 取 math.MaxInt64 时
  不做 +1（避免溢出）。
- 文档改为「返回**完整**报文（受 limit 上限约束），超过部分被截断且在
  Truncated 里标记」。
**为什么改签名而不是加个新函数**：DoRaw 是 v0.28.1（今天）才加的，org 内
**零消费方**（全仓 grep 无 httpjson.DoRaw 调用）；把「静默截断」的主入口留着、
另加一个正确的新函数，等于让缺陷继续存在——大家还是会用那个「看起来对」的
DoRaw。0.x 阶段 minor 允许 breaking（AGENTS.md），此刻改签名的代价最低。
### 2. 响应体没排空 ⇒ 连接无法复用
io.ReadAll(io.LimitReader(resp.Body, limit)) 只读到 limit 就 Close，响应体
剩余部分没人读。Go 的 transport **只有在 body 被读完时**才把连接放回空闲池
⇒ 连接被丢弃 ⇒ 下次调用要重建 TCP（+ TLS 握手）。
实测：服务端侧统计新建连接数，5 次请求新建了 **2** 条连接（修后为 1 条）。
修法：读完后排空剩余响应体（drainBody），再 Close。排空量有上限
（drainLimit = 64KB）——对端用超大响应体拖住本次调用时，放弃复用、让连接被
关闭即可，不为了复用把时间与内存赌上去。DoJSON 一并处理。
## 用例
httpjson/raw_test.go 改写并扩充：
- TestDoRawReturnsStatusAndBody：原始状态码与报文照旧；完整报文不标记
  Truncated。
- TestDoRawRespectsLimit：读体上限仍生效（4 字节）。
- TestDoRawReportsTruncation：20KB 报文 + limit=1024 ⇒ Truncated=true
  （**修前无从判断**）。
- TestDoRawNotTruncatedWhenWithinLimit：未超限不标记。
- TestDoRawBodyExactlyAtLimit：长度**正好等于** limit 不算截断。
- TestDoRawConnectionReused：服务端侧统计新建连接数：5 次请求应为 1 条
  （**修前为 2 条**）。
## 影响面
- **DoRaw 签名变更**（breaking）：(int, []byte, error) → (RawResponse, error)。
  org 内零消费方，v0.28.1 才发布。
- 响应体超 limit 时，调用方现在能知道拿到的是半截。
- 连接复用改善：报文超 limit 时不再每次重建 TCP/TLS。
- DoJSON 行为不变（仅增加排空）。
## 自检
go build / go vet / go test -race -count=1 / golangci-lint 全绿。
(cherry picked from commit 8ca20a540850c0ae36ade058d139a5b049673368)

### 修复(stringx)：ToCamelCase 遇非法 UTF-8 panic + Pluralize 空词丢计数

## 为什么
### 1. ToCamelCase 遇非法 UTF-8 panic
    func ToCamelCase(s string) string {
        for i, v := range s {
            return string(unicode.ToLower(v)) + s[i+utf8.RuneLen(v):]
        }
        return ""
    }
range 把非法字节解成 U+FFFD，而 utf8.RuneLen(U+FFFD) = **3**（U+FFFD 本身是
3 字节 rune），但非法字节**实际只占 1 字节** ⇒ 用 RuneLen 切片会越界：
    ToCamelCase("\xff")   →  panic: slice bounds out of range [3:1]
注释说的是「须按其 UTF-8 宽度切片」——但 RuneLen 给的是该 rune 的**规范**宽度，
不是这段输入里**实际消费**的宽度。非法字节从外部输入（用户上传、第三方 API
返回、日志文本）进来是常事 ⇒ 一个字符串工具函数因为一段坏字节把调用方进程
带走。
修法：改用 utf8.DecodeRuneInString(s[i:]) 取**实际**解码宽度（合法 UTF-8 时
与 RuneLen 相同，非法时是 1）。
### 2. Pluralize(count, "") 丢掉计数
    n := len(word)
    if n == 0 {
        return "0"
    }
空词分支返回字面量 "0"——**count 整个丢了**：Pluralize(5, "") 返回的也是
"0"，Pluralize(0, "") 恰好撞上同一个字面量所以从没被发现。
修法：空词时仍带上计数，形态与其他分支一致（"<count> <word>"）。既有测试
TestPluralize_EmptyWord 钉的是旧的错误行为，同 PR 一并改正。
### 顺带：itoa 的注释与实现不符
    // itoa 简单整数转字符串，避免引入 strconv。
    func itoa(n int) string {
        return strconv.Itoa(n)
    }
注释说「避免引入 strconv」，实现却正是 strconv.Itoa——上一版手写实现已被换成
标准库，注释没跟上。删掉这句。
## 用例
新增 stringx/edge_test.go：
- TestToCamelCase_InvalidUTF8：6 种坏输入（单字节、多字节连续、截断的
  2/3/4 字节序列、前缀带合法字符）均不得 panic。
- TestToCamelCase_Valid：合法输入行为不变（空串、"A"、"ABC"、汉字、"Ärger"、
  "ǅungla"→"ǆungla"）。
- TestPluralize_EmptyWord_KeepsCount：Pluralize(0|2|5, "") ⇒ "0 "/"2 "/"5 "。
- TestPluralize_Normal：既有行为不变（file/story/box/dish/church）。
既有 TestPluralize_EmptyWord（coverage_test.go）同步改正。
## 影响面
- Pluralize(n, "") 的返回值由 "0" 变为 "<n> "。依赖旧值（把 "0" 当「没有
  条目」的哨兵）的调用方需要调整——但旧值本身就是错的（Pluralize(5, "")
  也返回 "0"）。
- ToCamelCase 对非法 UTF-8 由 panic 变为正常返回。
- 无签名变更。
## 自检
go build / go vet / go test -count=1 / golangci-lint 全绿。
(cherry picked from commit b331b15b89bcf27c57a7fc0743147d7effa8dfae)

### 修复(strmap)：重载就地改写底层缓冲 —— 先前返回的字符串被改写

## 为什么
pkg/strmap 的设计目标是「GC 友好的只读字符串 map」：键与值打包进单一 []byte，
Item/Get 返回**零拷贝视图**。包文档承诺：
    Item/Get 返回的键字符串是底层数组的零拷贝视图，StrMap 存活期间保持有效，
    不得对其写入（底层只读保证）。
但「底层只读保证」在**重载**时被打破：LoadFromSlice / strStore.Load 此前是
**就地复用**旧缓冲（m.data = m.data[:0]、s.buf = s.buf[:total]）后重写字节，
而返回给调用方的字符串是这块缓冲上的 unsafe.String 视图 ⇒ **之前返回的
字符串内容被就地改写**。
实测（修前）：
    sm := strmap.NewStr2StrFromSlice([]string{"k"}, []string{"v1"})
    v, _ := sm.Get("k")        // "v1"
    sm.LoadFromSlice([]string{"k"}, []string{"w2"})
    fmt.Println(v)            // "w2"  ← 同一个变量，内容被改了
这破坏 Go 的字符串不可变保证：调用方以为拿到的是快照，实际是个**活引用**。
任何「取出键/值后缓存起来、稍后再用」的用法都会静默拿到错误数据。
## 修法
重载时**重新分配**底层缓冲，不再就地复用：
- StrMap.LoadFromSlice：m.data = make([]byte, 0, sz)（原先按 cap 决定是否复用）。
- strStore.Load：s.buf = make([]byte, total)（原先 s.buf = s.buf[:total]）。
旧视图仍指向旧缓冲，而旧缓冲由视图保活（GC 不会回收仍被字符串引用的数组）
⇒ 视图在整个 StrMap 存活期间保持有效，包文档的承诺重新成立。读路径零拷贝的
设计不变，只有重载路径多一次分配。
## 顺带：包文档补「Load 不得与读并发」
重载会就地改写本表的内部缓冲（data / items / hashtable），并发读会看到撕裂
的中间态。这是既有行为、本次未改，但此前文档未声明——补进 doc.go 的「语义
约束」，并给出调用方的正解（构造新表后整体替换指针）。
## 用例
新增 strmap/reload_test.go：
- TestStrMap_ReloadDoesNotMutateReturnedKey：Get 返回的值在重载后不得改变；
  重载后 Get 必须返回新值。
- TestStrMap_ReloadDoesNotMutateItemKey：Item 返回的键视图同样不得被改写。
- TestStr2Str_ReloadDoesNotMutateReturnedValue：Str2Str.Get 返回的**值**
  （打包进 strStore）同样不得被改写。
- TestStr2Str_ReloadDoesNotMutateReturnedKey：Str2Str 的键走 StrMap，同样
  不得被改写。
注：Item(i) 按**内部存储序**返回（哈希槽序，maphash 种子逐进程随机），不是
字典序 ⇒ 用例断言的是「重载前后不变」，不假定 Item(0) 是哪个键。
## 影响面
- 重载路径每次多一次分配（原本的复用优化被移除）。读路径零拷贝不变。
- 依赖「重载会让旧视图失效」这一**未被文档承诺**的行为的代码会变成「旧视图
  保持旧内容」——那是修正，不是倒退（文档承诺的是保持有效）。
- 无签名变更。
## 自检
go build / go vet / go test -count=1 / golangci-lint 全绿
（strmap 连跑 15 次无 flake）。
(cherry picked from commit 70b816b299a5f119d00efe2e5ec8ddaa4761c457)

### 修复(cache)：stale-if-error 被后台清扫打断 / Checker panic 永久卡死 / 回填绕过 CloneOnRead / stale 双计 / logger 冻结在 init / TTL=0 时 null marker 不回收

## 为什么
`pkg/cache` 是全 org 的缓存底座（redisx / data 等仓都经它）。本轮实测出
6 处缺陷，其中 3 处是「文档承诺的能力实际不成立」。
### 1. StaleIfError 被后台清扫打断（严重）
`LocalConfig.StaleIfError` 承诺「过期条目在本路径下保留于表内（回收交给
后台清扫），可反复兜底直至上游恢复」。但后台清扫（缺省开启、TTL/2 一跳）
把**所有**过期条目删掉，包括要留作 stale 兜底的真值；读路径的惰性删除
（Get/GetEx/Exists）也是。
实测（TTL=100ms、后台清扫开着）：
    过期 300ms 后 GetOrLoad → err="upstream down"   // 期望：拿到 stale 值
    关掉 NoBackgroundSweep → 拿到 stale 值 ✓
即兜底只在「[过期, 下次清扫)」这个窗口里有效，文档承诺落空。
修法：新增 `retainForStale`——StaleIfError 开启时，过期**真值**条目不被
清扫回收、也不被惰性删除回收（null marker 不参与 stale：它表示数据不存在，
没有可兜底的历史真值）。上游恢复后同 key 被新值覆盖，不额外占空间。
### 2. Checker 的一次 refresh panic 把它彻底废掉（严重）
清理（`c.inflight = nil` + `close(call.done)`）写在 refresh **之后**：refresh
一旦 panic，清理就不会执行 ⇒ inflight 永远挂着、done 永远不关 ⇒ 后续每一次
Check 都卡在 `<-call.done` 上。context.Background() 下是**永久挂起**（实测），
带 deadline 时也只是拖到 deadline 再报错——一次 refresh 的 panic 让 Checker
只剩第一次能用。
修法：清理移进 defer，panic 路径先把 panic 转成 error 交给等待者（他们不该
跟着崩，但需要知道「这次刷新失败了」），再把 panic 原样抛给首个调用方
（保留原始调用栈）——与 internal/singleflight 同型。panic 后的失败按 failTTL
缓存，与普通刷新失败同口径。
### 3. 回填路径绕过 CloneOnRead（改写返回值就污染缓存）
回填直接 `Set(val)` 再把**同一个指针**返回给调用方。回填之后该指针已经是
缓存的条目了，调用方一改写就污染缓存；CloneOnRead 挡不住——下次读命中克隆
的是**已被改写**的那份。LocalConfig.CloneOnRead 的注释说「回源路径不克隆
——那是调用方自己的值」，在回填前成立，回填后不成立。
修法：配了 CloneOnRead 时**存一份副本**、把调用方自己的值还给调用方；未配置
时保持零拷贝既有口径（接口文档：实现不拷贝时调用方此后不得改写）。
### 4. 一次 stale 读取同时计命中与未命中（命中率虚高）
stale 路径既在回源前计了未命中、又在兜底后计了命中 ⇒ 一次读被计成两次 ⇒
命中率虚高。这与 interface.go「一次读恰好计入命中或未命中之一」的承诺矛盾，
也与 Stats 的 Hits 口径（「读到**未过期的**真值条目」）矛盾——stale 拿到的是
已过期的条目。
修法：stale 兜底只计未命中、不计命中。Stats 文档同步补明这条口径。
### 5. 缓存日志汇流不到消费方的 logger
`var cacheLog = slog.Default().With(...)` 是包级 var，在 init 阶段求值——
早于任何 main() 里的 `slog.SetDefault` ⇒ 缓存日志始终走进程启动时的默认
handler。注释声称「消费方服务可通过 slog.SetDefault 汇流」，实测不成立。
修法：改为调用时取 logger（`cacheLogger()`）。
### 6. TTL=0 时过期的 null marker 永远没人回收（无界增长）
后台清扫的启动条件是 `TTL > 0`，但条目会过期有两个来源：真值 TTL **和**
null marker 的 NullTTL（缺省 1 分钟）。TTL=0（真值永不过期）+ NullTTL>0 的
配置下，null marker 会过期却永远不被回收——惰性删除只覆盖 Get/GetEx/Exists，
回源路径是非删除式的 ⇒ 大量 miss 的 key 会把表撑爆。
修法：启动条件改为「存在会过期的条目来源」（`sweepNeeded`），清扫节奏取最短
过期窗口的一半（`sweepInterval`：TTL 与 NullTTL 取小，下限 1s）。
### 顺带：internal/singleflight 的两个缺陷
- **先唤醒后摘键**：清理顺序是「先 close(ready)、后 Delete(key)」⇒ 两者之间
  存在窗口，此刻新到的调用 LoadOrStore 命中的是**已结束**的那次 flight ⇒
  直接拿到上一次的陈旧结果、根本不执行 fn。改为先摘键后唤醒（与
  golang.org/x/sync/singleflight 一致）。
- **panic 错误丢身份**：panic 值用 `%v` 拼接，只留文本 ⇒ 等待者无法
  errors.Is 到原始错误。r 是 error 时改用 `%w`。
## 用例
新增 `cache/local_stale_sweep_test.go` 与 `cache/robustness_test.go`：
- `TestLocalCache_StaleIfError_SurvivesBackgroundSweep`：stale 兜底要活过
  后台清扫，且能**反复**兜底（修前第一次就失败）。
- `TestLocalCache_StaleIfError_Disabled_StillSweeps`：未开启时回收行为不变。
- `TestLocalCache_StaleIfError_NullMarkerNotRetained`：null marker 不参与
  stale 保留、照常被回收。
- `TestLocalCache_SweepNeeded_NullTTLWithoutTTL`：TTL=0 + NullTTL>0 时过期的
  null marker 必须被回收（修前 size 停在 20 不降）。
- `TestLocalCache_SweepInterval_UsesShortestHorizon`：清扫节奏取最短窗口的一半。
- `TestLocalCache_CloneOnRead_BackfillDoesNotSharePointer` /
  `..._Unset_KeepsZeroCopy`：回填不再共享指针；未配 CloneOnRead 时保持零拷贝。
- `TestLocalCache_Stats_StaleCountsAsMissOnly`：一次 stale 读取计 1 次未命中、
  0 次命中。
- `TestCacheLogger_FollowsSetDefault`：缓存日志跟随 slog.SetDefault。
- `TestChecker_RefreshPanic_DoesNotWedge` / `..._WaiterGetsError` /
  `..._ThenSuccess`：panic 不再废掉 Checker；等待者拿到错误；失败窗口过后能
  重新刷新。
- `TestSingleFlight_FlightKeyRemovedBeforeWake`：flight 结束后新调用必须重新
  执行 fn。
- `TestSingleFlight_PanicErrorKeepsIdentity`：等待者的错误保留原始错误身份。
## 影响面
- **stale 兜底的可用性变大**（从「[过期, 下次清扫)」窗口变成「直到上游恢复
  或同 key 被覆盖」）。开启 StaleIfError 的部署会多留过期真值——与「缓存条目
  无上界」的既有口径一致；要上界用 MaxSize。已在 LocalConfig.StaleIfError
  的注释里写明该代价。
- **stale 读取不再计入 Hits**：按 Stats 文档的口径修正，命中率先前虚高。
- Checker 的 panic 语义：等待者拿到 error 而不是跟着崩；首个调用者仍收到
  panic（原始调用栈）。
- 无签名变更。
## 自检
go build / go vet / go test -race -count=1 ./... / golangci-lint run ./... 全绿。
(cherry picked from commit 8e6529dc211202eaf72f5ee58927bdc6a21a27f4)

### 修复(errors)：Errorf("%w", 空报文错误) 返回 nil —— 失败被静默吞成成功

## 为什么
`pkg/errors` 是全 org 唯一的错误包（类型同一性红线：New/As/Is/Unwrap 与
stdlib 同型透传），每个仓都经它。三个缺陷都在「错误处理本身出错」的类别上，
其中第一个直接把失败变成成功。
### 1. Errorf("%w", 空报文错误) 返回 nil（严重）
`Errorf` 先渲染消息，再判 `if msg == "" { return nil }`。「格式化结果为空串
返回 nil」的本意是给 `Errorf("")` 一个方便取向，但 %w 的消息来自**被包装
错误本身**——被包装错误的报文为空时（`errors.New("")`，或消息由空变量拼出
的自定义错误），整条消息渲染成空串 ⇒ 返回 nil：
    err := errors.Errorf("%w", errors.New(""))
    // err == nil —— 被包装的错误连同整条链一起消失
后果是**失败被静默吞成成功**：`if err != nil { return err }` 的整个错误分支
被跳过，上层拿到 nil 就当成功继续走。`fmt.Errorf` 在同输入下返回非 nil 错误。
修法：只有「无 %w 被包装**且**消息为空」才返回 nil；有 %w 时一律返回错误
（消息可以为空串，包装链必须保留）。
### 2. typed-nil *BizError：Error() 崩、AsBizError 报「命中」
`var be *BizError; var err error = be` 是极常见的形态（函数返回 *BizError、
调用方赋给 error 接口）。此时：
- `err.Error()` 空指针崩溃（`Error()` 直接解引用 e.Cause）——任何日志或
  格式化路径踩到就崩，把「错误处理出错」升级成「进程崩了」；
- `AsBizError(err)` 返回 `(nil, true)`——`stderrors.As` 对 typed-nil 返回
  true，而按文档用法 `if ok { use b.BizCode }` 就是一次空指针解引用；
- `IsBizError(err)` 同样报 true。
修法：`Error()`/`String()`/`Unwrap()` 对 nil 接收者做保护；`AsBizError` 与
`IsBizError` 以「取到的指针非 nil」为命中口径。
### 3. MultiError.Unwrap() 泄漏内部切片
`Errors()` 刻意返回防御性拷贝，`Unwrap()` 却把内部切片直接交出去。调用方
`err.(interface{ Unwrap() []error }).Unwrap()[0] = x` 就绕过了 `Append` 的
copy-on-write 保护，内部状态被改。
修法：`Unwrap()` 同样返回防御性拷贝。`Is()` 改为直接只读遍历内部切片
（不必走拷贝），保持遍历开销不变。
## 用例
- `TestErrorf_WrappedEmptyMessage_NotNil`：空报文错误不得被吞成 nil，包装链
  仍可判定。
- `TestErrorf_WrappedEmptyMessage_MatchesStdlib`：四种输入下 nil 性与 stdlib
  一致。
- `TestErrorf_EmptyFormatStillNil`：`Errorf("")` → nil 的原有取向保持不变
  （被既有测试钉住，不在本次修复范围）。
- `TestBizError_TypedNil_NoPanic` / `TestBizError_TypedNil_InChain`：typed-nil
  不崩、不被当成有效 BizError；`TestBizError_Normal_StillWorks`：正常路径不变。
- `TestMultiError_Unwrap_ReturnsCopy`：Unwrap 不得交出内部切片。
## 影响面
- `Errorf` 在「空报文 + %w」下由 nil 变为非 nil 错误——这是修 bug，不是行为
  倒退：原先的 nil 是把失败吞掉。
- `AsBizError` / `IsBizError` 对 typed-nil 的判定由 true 改为 false。
- 无签名变更。
## 自检
go build / go vet / go test -race / golangci-lint 全绿。
(cherry picked from commit 3a6402f05ceeac1fb14ad0bbf872372e69d501f0)

### 修复(urlguard)：IPv6 zone 后缀击穿全部 IP 字面量判定（SSRF 绕过）

## 为什么
`urlguard` 是 org 唯一的出站 SSRF 防护原语（webhook 出口在用），包文档
明确承诺阻断 loopback、链路本地、ULA、IPv4-mapped 等网段。实测发现
**IPv6 字面量带 zone 后缀时，全部 IP 字面量判定被整段跳过**：
    http://[::1%25lo]/              => 放行（期望：阻断 ::1/128）
    http://[fe80::1%25eth0]/        => 放行（期望：阻断 fe80::/10）
    http://[fc00::1%25eth0]/        => 放行（期望：阻断 fc00::/7）
    http://[::ffff:127.0.0.1%25eth0]/ => 放行（期望：归一化后阻断 127/8）
    http://[::1]/                   => 阻断（对照组）
即：只要在 IPv6 字面量后缀一个 `%25<zone>`，本该被拦的私网/回环/链路本地
地址全部放行。`net.Dial("tcp", "[::1%lo]:80")` 能按 zone 找到接口并连上
本机 loopback ⇒ 只挂 `CheckURL`（包文档推荐的用法）的调用方被完整 SSRF。
## 根因
`net.ParseIP` 明确不认 zone（Go 文档："does not accept zones"），而
`url.Parse` 后的 `u.Hostname()` 会保留它——Go 把 `%25` 还原成 `%`：
    url.Parse("http://[::1%25lo]/").Hostname() == "::1%lo"
    net.ParseIP("::1%lo")            == nil      // ⇒ IP 字面量判定整段跳过
`checkHostLiteral` 与 `resolve`（拨号层预检）都直接 `net.ParseIP(host)`，
拿到 nil 就落到域名分支继续走 ⇒ zone 形态整体绕开。
## 修法
zone 只决定「走哪块网卡」，不改变「连的是哪个地址」——判定按地址部分做。
新增 `stripIPv6Zone`，在两处 `net.ParseIP` 之前剥掉 `%` 之后的 zone。
## 顺带修掉的同族缺口（同一 PR、同一个「守卫没守住它声称守住的」主题）
1. **端点复核只认 `*net.TCPAddr`** ⇒ UDP 连接（`*net.UDPAddr`）与 IP 连接
   （`*net.IPAddr`）的端点复核被整段跳过 ⇒ TOCTOU 窗口对 UDP 完全敞开。
   实测：预检通过、真实端点是 `127.0.0.1` 的 UDP 连接照样放行。
   改为一并覆盖 TCP / UDP / IP 三种 `net.Addr`。
   未知 `net.Addr` 实现（自定义传输、代理连接）**刻意不 fail-closed**：
   代理链路的 RemoteAddr 是代理地址（本机代理常见 127.0.0.1），一拒就把
   所有代理用法打死。该边界写进包文档「已知边界」。
2. **IPv6 保留段缺口**：`::/96`（IPv4-compatible，如 `::127.0.0.1`）与
   `fec0::/10`（site-local，RFC 3879 已废弃）此前不在阻断表内 ⇒ 放行。
   补进 `blockedCIDRs`。
3. **包文档补「已知边界」**：走 `http.Transport.Proxy` 时 `DialContext`
   收到的是**代理**地址，真实目标的 DNS 从头到尾没被 IP 级校验过 ⇒
   走代理必须先过 `CheckURL`。此前文档未声明该边界。
## 用例
- `TestCheckURL_IPv6ZoneBypass` / `TestCheckHost_IPv6ZoneBypass`：
  四种 zone 形态必须被阻断（修前全部放行）。
- `TestCheckURL_IPv6Zone_AllowedPrivate`：豁免口径对带 zone 与不带 zone
  一致（豁免的是网段，不是写法）。
- `TestCheckURL_IPv6ReservedRanges`：`::/96` 与 `fec0::/10` 必须被阻断。
- `TestDialFunc_EndpointRecheck_UDP` / `..._IPAddr`：端点复核覆盖 UDP/IP。
- `TestDialFunc_EndpointRecheck_UnknownAddrType`：未知 addr 类型不
  fail-closed（把该边界钉住）。
- `TestStripIPv6Zone`：剥 zone 的边界行为。
自检：go build / go vet / go test -race / golangci-lint 全绿。

- 文档：版本引用对齐 v0.28.1

README 安装行与 AGENTS.md 当前版本行仍指向 v0.28.0，而 v0.28.1 已发布。
Why:
- 矩阵审计的「版本线」独立门禁会判红（安装行 / AGENTS 当前版本 ≠ 最新 tag）。
- 对外安装行与 AGENTS 版本行是消费方核对版本的事实源，落后即失真。
What:
- README.md：`go get github.com/oekit/pkg@v0.28.0` → `@v0.28.1`
- AGENTS.md：当前版本 `v0.28.0` → `v0.28.1`
Verify:
- `python3 scripts/refresh_versions.py` 输出 pkg「已对齐」


## [v0.28.1] — 2026-10-28

### 新增(httpjson)：加 DoRaw——返回原始状态码与原文报文（不判 2xx、不截断）

## 为什么
`DoJSON` 的语义是「2xx 即成功 + 错误体 rune 安全截断到 200 字符的**摘要**」——
适合「要么成功要么报错」的调用方。但有两类调用方**用不了**它：
1. **要按状态码映射自己的语义**（如 404 → 「口令未知」、409 → 「他人占用」，
   两者是不同的客户端哨兵错误，不是同一个失败）；
2. **要把原文报文带进错误文案**（摘要会丢掉服务端给的说明文字）。
消费方实证（pigeonbox/p2p 的配对流程 `internal/client/pairing.go`）：
`doRaw` 返回 `(status, body, err)`，由 `pairHTTPError(status, body)` 映射
404/409 → `ErrPairUnknown`/`ErrPairBusy`，其余错误**原样带报文**。
## 语义
- 返回 `(status int, body []byte, err error)`：
  - 传输层错误（连不上/超时）⇒ err 非 nil，status/body 无意义；
  - HTTP 层错误（任何状态码，含 2xx）⇒ err 为 nil，由调用方按状态码自行判定。
- **不判 2xx、不截断报文、不解 JSON**（调用方自己决定）。
- `limit` 是读体上限（字节），<=0 时用包级 maxBody（64MB）⇒ 调用方可按自己的
  报文体量给一个够用又防内存放大的值（如 8KB）。
## 用例
- 2xx 与非 2xx 都返回原始状态码 + **原文**报文；
- 读体上限生效（4 字节上限 ⇒ 只读到 4 字节）。
自检：go build / go vet / go test ./... 全绿。

- 文档：版本引用对齐 v0.28.0

## [v0.28.0] — 2026-10-25

### 新增(pagination)：补 Policy.NormalizeLimit——条数口径的归一化入口（#115）

## 缺口
`pkg/pagination` 现在的归一化口径全是**按页**的：
```go
func (p Policy) Normalize(page, pageSize int) (int, int)
func (p Policy) Offset(page, pageSize int) int
func (p Policy) Limit(page, pageSize int) int
```
但有一类调用点要的其实不是「第几页、每页几条」，而是「**最多返回多少条**」——
比如一个工具的 `limit` 参数，语义是行数，没有页的概念。
下游的实际绕法（功能正确但语义拧）：
```go
func normLimit(v, def, max int) int {
	policy := pagination.Policy{
		DefaultPageSize: def,
		MaxPageSize:     max,
		OverMax:         pagination.OverMaxFallback,
	}
	_, size := policy.Normalize(1, v)   // ← 把 page 恒钉成 1，当页长用
	return size
}
```
`Normalize(1, v)` 之所以能用，是因为 page 恒为 1 ⇒ offset 恒为 0，页长就是条数。
但读起来是「把「第 1 页的页长」当「条数」用」——翻页的人看到 `Normalize(1, v)`
会先愣一下才反应过来这里没有页。
## 改法
给 `Policy` 加一个**条数口径**的便捷入口（与按页的 `Normalize` 并列，**不改动现有语义**）：
```go
// NormalizeLimit 按「最多返回多少条」的口径归一化：
// n <= 0 ⇒ 口径的默认条数；n 超上限 ⇒ 按 OverMax 语义处理
// （OverMaxFallback 回退默认条数 / OverMaxClamp 钳到上限）；
// 口径不限上限时原样返回。
func (p Policy) NormalizeLimit(n int) int
```
下游变成：
```go
func normLimit(v, def, max int) int {
	policy := pagination.Policy{
		DefaultPageSize: def,   // 条数口径复用同一组字段
		MaxPageSize:     max,
		OverMax:         pagination.OverMaxFallback,
	}
	return policy.NormalizeLimit(v)
}
```
**字段复用**（`DefaultPageSize` 兼作「默认条数」）而不是另起一组字段，是为了不把
`Policy` 的语义分裂成两套几乎一样的口径。
## 语义与既有入口的关系
`NormalizeLimit(n)` **等价于** `Limit(1, n)`——页码维度被丢弃，只剩条数维度。
区别只在**表达**：不必传一个恒为 1 的页码，调用点的意图自明。
规则与 `Normalize` 的页长档逐条一致（非法值取默认 / 超限按 OverMax 语义 /
不限上限不截断 / 未知 OverMax 按钳到上限处理）。
## 测试
`pagination/policy_test.go` 新增两个表驱动用例：
- `TestPolicy_NormalizeLimit`（12 例）：非法条数取默认、负值取默认、上限内原样返回、
  超限钳到上限、超限回退默认条数、不限上限不截断、未知 OverMax 按钳到上限处理；
  每例同时断言**等价不变量** `NormalizeLimit(n) == Limit(1, n)`。
- `TestPolicy_NormalizeLimit_VsPageDimension`：钉住「条数口径」与「按页口径」各自自洽
  ——同一个 `Policy` 下 `Normalize(3, 50)` 给的是「第 3 页、每页 50 条」，
  `NormalizeLimit(50)` 给的是「最多 50 条」；且条数口径**不受 `DefaultPage` 影响**，
  页码口径**不受条数口径影响**。
**变异验证（实跑）**：把 `NormalizeLimit` 改成直接 `return n`（不过归一化）⇒ 3 个
子用例立刻 FAIL（含等价不变量那条）；还原后全绿。
## 兼容性
- 只新增方法，**零行为变化**：既有 `Normalize` / `Offset` / `Limit` 语义与实现均未动。
- 零第三方依赖（本仓 go.mod 无 require），只动标准库。
- `Policy` 零值口径（`Policy{}`）与包级默认逐字一致的口径不变。
关联 oekit/pkg#115。

- 文档：版本引用对齐 v0.27.0

## [v0.27.0] — 2026-10-25

### 新增(shellx)：补 POSIX shell 参数转义原语（#112）

把外部输入拼进 shell 命令时，仓里只有 pathx 的「路径合法吗」，
「怎么写进命令行才不被 shell 解释」这一层是空的——消费方于是各写一份
或干脆不写。不写的后果是实测级的：
- "a.txt; rm -rf /" 是两条命令而不是一个文件名
- 含空格的路径被拆成两个参数，文件写到别处，而命令看起来「成功」了
- "../etc/x" 落到预期之外的目录
新增 pkg/shellx：
- Quote(s) 把任意字符串转义成一个 shell word。POSIX 单引号包裹——
  单引号内 shell 不做任何解释，唯一要照顾的是引号本身：关掉、转义、
  重新打开。空串返回 ''（空参数仍是一个参数，丢掉会让 cmd '' arg
  变成 cmd arg，参数整体错位）。
- QuoteArgv(args...) 逐个转义后空格连接。
- 纯转义、不做策略判断：换行与越界各有各的规则，由调用方决定。
与 pathx 是两个不同问题、互补而非重复：pathx 问「路径合法吗」，
shellx 问「怎么写进命令行不被解释」，路径类输入通常两道都要过。
测试除表格用例外，另有一条用真 shell 的 round-trip（把转义后的值交给
sh -c，回来的值必须等于原串）——只比对期望字符串只能证明「和作者想的一样」，
round-trip 才能证明「shell 真的这么解」。

- 文档：版本引用对齐 v0.26.0

## [v0.26.0] — 2026-10-25

### 新增(convert)：客户端 IP 解析收紧——X-Real-IP 默认不采信、候选值校验、可信代理预解析（BREAKING，#104、#106、#107）

BREAKING：ResolveClientIP / GetClientIP / GetClientIPWithProxies **不再采信 X-Real-IP**，
入参与语义都有变化。
## X-Real-IP 默认不采信（#106）
X-Real-IP 是**单跳头**，HTTP 头本身无法区分「代理写的」与「客户端自带的」——大量网关
（Envoy / Higress / Traefik）默认不写该头，但会**原样透传**客户端自带的 X-Real-IP。
于是 XFF 那条有「右向左跳过可信代理」保护的路径，与 XRI 这条没有保护的路径信任强度
不对等：客户端可以任意改写自己的客户端 IP —— 限流/锁定计数桶被绕过、审计与访问
日志的来源 IP 可被改写。
「让 XRI 也走可信链判定」救不了这条：伪造值与真实客户端 IP 都是公网地址，都会被
接受。唯一的正解是**默认不采信**，把「要不要采信」交给部署形态决定。
- ResolveClientIP(remote, xff, trustedProxies) —— 去掉 xri 入参（BREAKING）；
- ResolveClientIPWithXRI(remote, xff, xri, trustedProxies) —— 显式开启；
- GetClientIPWithXRI / GetClientIPWithProxiesAndXRI —— net/http 适配层同款开关。
## 候选值合法性校验与端口归一（#104）
旧实现把 XFF 的每一段原样返回：任意字符串进限流/锁定键 = 「伪造 IP 永不命中计数」
= 绕过，且可注入任意内容污染审计日志；带端口的候选（"1.2.3.4:54321"）与裸 IP 是
两个不同的分桶键，同一客户端换个端口就换一个桶。
- 每一段候选先归一为裸 IP（去空白、剥端口、IPv6 压缩写法归一），**非法段直接跳过**，
  不进返回值；整条链都没有合法候选时回退对端地址；
- XFF 存在且给出合法候选时，结果由 XFF 决定（整条链都可信 ⇒ 回退最左端），
  不再看 X-Real-IP——保持 v0.25.2 的既有优先级不变。
## 可信代理裸 IP 条目不再被静默忽略（#104）
清单里写 "203.0.113.5" 的本意是「信任这一台代理」，而 net.ParseCIDR 对裸 IP 解析
失败后旧实现直接 continue —— 配置**不生效且无任何告警**：可信代理悄悄退化成
「不信任任何代理头」，表现为限流 key 变成代理 IP、审计里看不到真实客户端，而查
配置看不出任何异常。
- 裸 IP 一律归一为单主机网段（IPv4 /32、IPv6 /128）；
- NewTrustedProxySet 构造期对非法条目**报错**（fail-fast）：静默跳过把「配置写错」
  变成「安全策略降级」且不可观测；
- 清单形态的函数签名无法回报错误，非法条目仍按历史口径跳过，但裸 IP 一律生效。
## 可信代理预解析（#107）
旧实现每个请求都把整份可信代理清单逐条 net.ParseCIDR 一遍（清单 N 条 ⇒ 每请求
N 次解析），而解析结果在清单不变时是恒定的。
- TrustedProxySet —— 构造期解析一次、请求期只做网段包含判定，热路径零 CIDR 解析；
- ResolveClientIPWithSet / ResolveClientIPWithSetAndXRI —— 集合形态入口，
  采信规则与清单形态**逐字一致**（有用例做差分对表）。
## 其它
- 空对端地址的语义写进文档并补测（返回空串：没有可信对端 ⇒ 不采信任何代理头）；
- README 的 convert / encoding 行同步新签名与新原语。
## encoding（#105）
补 Base64RawURLEncodeBytes / Base64RawURLDecodeBytes —— RawURL 一族此前只有
string 形态，字节场景（签名原文、密钥材料、PEM DER）只能经 string 中转，多一次
拷贝且把二进制塞进 string 有不可见字符问题。
## 测试
convert 5 组（默认不采信 XRI / 显式开启 / 候选值硬化 / 集合与清单差分 / 非法条目
fail-fast）、encoding 2 组（字节入口差分 stdlib + 往返）。既有
TestGetClientIP_XRealIP 编码的是「默认采信 XRI」的旧语义，随语义变更改写。

- 文档：版本引用对齐 v0.25.2

## [v0.25.2] — 2026-10-25

### 新增(pagination)：分页口径可按服务声明（#100）

默认页长/上限写死在包级常量、超限只有「钳到上限」一种语义，导致没有任何一个
消费方能逐字等价切到上游实现——各家的默认页长、上限、超限语义都不同，响应条数
是可观测差异。新增 Policy：默认页长/上限/超限语义由服务自己声明，零值口径等同
包级默认；OverMaxClamp（钳到上限）与 OverMaxFallback（回退默认页长）两种语义都
能表达；MaxPageSize < 0 表示不限上限。

### 新增(convert)：抽出框架无关的客户端 IP 解析纯函数（#95）

GetClientIP / GetClientIPWithProxies 的入参都吃 *http.Request，非 net/http 框架
（Hertz 等）用不了，于是每个仓都自己手写一份等价实现，可信代理的采信口径逐仓漂移。
新增 ResolveClientIP(remote, xff, xri, trustedProxies) 纯函数承载全部采信判定——
XFF 从右向左跳过可信代理、伪造值不采信、直连不可信时忽略一切代理头；net/http
适配层改为委托它，既有签名与行为逐字不变。
另导出 DefaultTrustedProxies()（返回副本）供框架适配层取同一份默认可信网段，
避免各框架各抄一份内网网段清单后各自漂移。

- 文档：版本引用对齐 v0.25.1

## [v0.25.1] — 2026-10-25

### 新增(encoding)：补 base64 无填充（Raw）变体（#96）

现状：pkg/encoding 的四个 base64 变体全是**带填充**形态（base64.StdEncoding /
base64.URLEncoding），没有 Raw（无填充）那一套。
缺口：JWT / OIDC / PKCE 协议里用的就是**无填充 base64url**
（base64.RawURLEncoding）——base64url 的填充字符 '=' 在 JWT 里不允许出现
（RFC 7515 §2：base64url 编码时省略 '=' 填充）。拿现有的 Base64URLDecode 去解
JWT 段（无填充，长度非 4 的倍数）会返回 "illegal base64 data at input byte N"
——**包里的方法名与协议实际用到的变体不对应**，收编时极易踩坑。
改动（纯新增，现有方法签名与行为不动）：
- Base64RawEncode / Base64RawDecode / Base64RawEncodeBytes / Base64RawDecodeBytes
  （base64.RawStdEncoding）
- Base64RawURLEncode / Base64RawURLDecode（base64.RawURLEncoding）
- 现有 Base64Encode / Base64Decode / Base64EncodeBytes / Base64DecodeBytes /
  Base64URLEncode / Base64URLDecode 的 godoc 点明「本方法是**带填充**变体，
  JWT / OIDC 协议段请用 Raw 变体」——此前文档没写，踩坑只能靠读实现。
命名与 stdlib 对齐（base64.RawStdEncoding / base64.RawURLEncoding）。
测试：以 **stdlib 作独立 oracle** 逐例差分（编码逐字节相等、解码逐字节相等，
不复述被检逻辑），另钉三条契约——无填充变体输出不含 '='、URL 变体输出不含
'+' 与 '/'、带填充变体解不了无填充串（而 Raw 变体解得了）。

## [v0.25.0] — 2026-10-24

### 新增(urlguard)：补自定义 IP/host 判定钩子（pkg#91）

缺口：urlguard 是纯放行型防护——缺省阻断私网/loopback/保留段与约定域名
后缀，除此之外的公网地址一律放行。现有三个 IP 维度都表达不了消费方常见的
限制型策略：WithAllowCIDRs 的语义是「豁免私网阻断」而非「限制到这些网段」，
blockedCIDRs 是固定私网清单、没有运行时追加拒绝项的入口，整个包没有端口维度；
host 维度同样只有放宽型入口（WithAllowDomains 后缀豁免 / WithExactHosts
authority 钉死），没有常规白/黑名单入口。
后果：消费方的出站防护通常是两段式——写面校验（发起前解析 DNS 按自己的
策略判定 URL）+ 建连层复核（拨号时解析、再按同一套策略判定真实连到的端点
IP）。第 2 段不是冗余：两段之间存在重解析窗口（DNS rebinding / TOCTOU），
攻击者可在窗口内把 A 记录切到内网地址。Guard.DialFunc 已有骨架（解析 → 预检
→ 建连 → 以真实 conn.RemoteAddr() 复核），但复核用的是 urlguard 自己的
CheckIP ⇒ 消费方只能放弃复核（防护实际失效）或在下游另写一套拨号包装
（把「SSRF 防护的单源判定」这个定位在下游又复制了一份）。
修法：补两个判定钩子，让消费方自己的策略成为单一判定源：
- WithCheckIPFunc(fn func(net.IP) error)：作用于所有 IP 判定点——拨号层
  预检（解析出的每个 IP）、建连后的真实端点核对（RemoteAddr）、URL 层的
  IP 字面量判定 ⇒ 消费方策略在「预检」与「建连后端点复核」两处都生效，
  重解析窗口由消费方自己的策略闭合；
- WithCheckHostFunc(fn func(host string) error)：作用于所有 host 判定点
  ——CheckHost、CheckURL、拨号层的目标 host。
判定顺序：内置判定在前、自定义判定在后（内置的私网/保留段阻断仍是兜底，
fail-closed）。构造态豁免语义收敛：WithAllowPrivate / WithAllowCIDRs 只
豁免内置判定，不再跳过自定义判定——豁免不再意味着「整个 IP 判定被短路」。
未注入钩子时行为与既有完全一致（既有测试全绿）。
入参口径：IP 已归一化（IPv4-mapped IPv6 → IPv4，消费方无需自己防 mapped
绕过）；host 已归一化（小写、去尾点、去 IPv6 方括号）。钩子可能被多次调用
（各判定层各一次、每个解析 IP 各一次），应是无副作用的纯判定。
测试：新增 urlguard/policy_test.go（9 个用例），已验证回退修法即红。
Closes #91

### 修复(cache)：SingleFlightCache 把一次未命中计成两次 Misses（pkg#90）

现象：SingleFlightCache 包装 LocalCache 时，一次未命中被计成两次 Misses
——前置检查（Peek）计一次，内层回源加载路径再计一次。Hits 正确、Misses
每次回源路径多 1，命中率指标失真（实测 90% 的真实命中率被报成 81.8%）。
根因：内层无法区分「装饰器为了判命中做的探测」与「调用方真正的一次查找」。
修法：让装饰器的前置探测走一条不计数的路径，而不是在装饰器层做计数补偿
（装饰器不改写内层计数语义）：
- LocalCache 增 getOrLoadNoMiss：同流程但不计入未命中计数（命中计数不受
  影响——stale 兜底等读命中仍计入，与内层直连口径一致）；
- SingleFlightCache 的三条回源路径（GetOrLoad / GetOrLoadWithSource /
  GetOrLoadWithSourceTTL）在内层提供该能力时优先走它；
- 内层不提供该能力时退回内层原有口径（能力缺失是内层的事实）。
效果：一次读恰好计入命中或未命中之一，与内层直连一致：
- 3 次查找（1 冷 + 2 热）：hits=2 / misses=1（修正前 misses=2）；
- 10 次查找：hits=9 / misses=1，命中率 90%（修正前 81.8%）；
- 20 次并发未命中：misses=20（修正前 21），回源仍只 1 次（单飞有效）。
测试：新增 cache/singleflight_stats_test.go，判据一律是「装饰器口径 ==
内层直连口径」（不写死期望数字）；已验证回退修法即红。
Closes #90

- 文档：版本引用对齐 v0.24.0

## [v0.24.0] — 2026-10-24

### 新增(cache)：补观测计数面 Stats 与按调用注入回源（#76、#77）

#76：LocalCache 缺命中/未命中/淘汰计数观测面——要暴露缓存指标的仓只能自己写
一份带计数的 TTL 缓存。
- 新增 cache.Stats（Hits/Misses/Evictions/Expirations/Size）与可选能力接口
  cache.StatsProvider（不加入 Cache[T]，不破坏其它后端实现）；
- 读路径计数：Get/GetEx/Peek/MGet（逐键）/GetOrLoad；null marker 按未命中
  口径（无值可返，与 GetEx/Peek 的 hit=false 一致）；写路径（Set/Del）与
  Exists（存在性探测，不是取值读路径）不计入；
- 容量淘汰（MaxSize 触发）计 Evictions、TTL 过期回收计 Expirations——两类
  回收分开计，Evictions=0 不会与「没发生过淘汰」混淆；
- SingleFlightCache 原样透出内层快照（装饰器不丢能力，同 MGet/Peek 的退化
  口径：内层无该能力时返回零值快照）；
- cachetest 补 RunStatsWithClock 契约（计数口径自洽 + 单调不减）。
#77：回源函数只能在构造期固定——回源依赖 key / 依赖调用上下文的缓存
（token / 凭据 / 租户级）拿不到按调用注入的入口。
- 新增可选能力接口 cache.SourceInjector 与 LocalCache/SingleFlightCache 的
  GetOrLoadWithSource / GetOrLoadWithSourceTTL：回源函数由调用方在**调用点**
  给出，source 为 nil 时退回构造期 Source/SourceWithTTL；
- SingleFlightCache 保持单飞语义（同一 key 的并发请求只调一次 source），
  内层不支持该能力时退回内层构造期 source——能力缺失是内层的事实；
- cachetest 补 RunSourceInjectionWithClock 契约（注入生效 / 命中不回源 /
  nil 退回构造期 / 动态 TTL 生效 / 显式 ttl 覆盖动态 TTL）。
兼容性：全部为**新增**导出符号，既有签名与行为不变（0.x 下按 minor 发版）。
构造期 Source 本就收 key（func(ctx, key)），「按 key 分派」一直可做；本轮
补的是**按调用注入**这一层——同一 key 的不同调用方可给出不同回源函数。

- 文档：版本引用对齐 v0.23.1

- 矩阵 DESC 同步——bootstrap 就绪门漏接可观测（nacos 看门狗超时告警 + RegistrationState 暴露注册阶段，#33）

## [v0.23.1] — 2026-10-24

### 新增(jsonrepair)：导出 StripCodeFence 原语——围栏语义单源落到上游

pkg/jsonrepair 内部早有 markdown 围栏剥离实现 stripCodeFence，但它未导出，
下游因此在 3 个仓各抄了一份逐字相同的实现（oagentkit/agentkit、
agentkit-bisect、textx 的 textutil/fence.go），四处共享同一条正则
`(?s)```(?:json)?\s*(.*?)\s*````。
现状是「单源在下游、上游反而是副本」：围栏口径一旦要改（多围栏块取哪块、
~~~ 变体、CRLF），要同步改 4 处。而 ExtractJSON 的 godoc 明写「语义单源
textutil.StripCodeFence」——单源该落在能约束下游的地方。
修法（pkg#74）：
- stripCodeFence 导出为 StripCodeFence，成为围栏语义的单源；
- 包内既有调用点沿用 stripCodeFence 别名（指向同一实现，不存在两份口径）；
- 补表驱动测试锁定导出原语的行为：无围栏、``` 与 ```json 变体、
  多围栏块取第一块、围栏外有散文、空串——副本方改语义时会被这份用例
  提醒跟着改。
只剥围栏、不做 JSON 切片——需要「提取 JSON 文本」用 ExtractJSON，
需要「宽容解析」用 Unmarshal / ParseLenient，既有导出面不变。

## [v0.23.0] — 2026-10-24

### 修复(cache)：SingleFlightCache 包装 LocalCache 时静默废掉 StaleIfError

前置检查走 Get（LocalCache 的惰性删除路径），把 stale-if-error 兜底需要的
过期真值提前删掉 ⇒ 包装后 StaleIfError 形同虚设，回源失败直接抛错，与
LocalConfig.StaleIfError 字段注释承诺的 fail-open 语义相反。
修法（issue 选项 1）：
- LocalCache 新增 Peek：非删除式读——读到过期条目只算不命中、不删除它
  （过期真值留在表内供 stale 兜底）；命中真值时行为与 Get 一致（LRU 提升
  顺序 + CloneOnRead 返回副本）
- 新增可选能力接口 PeekGetter（与 Sweeper/HitGetter 同款外置，不破坏其它
  后端实现）
- SingleFlightCache.GetOrLoad 前置检查改走 Peek；内层不支持 PeekGetter 时
  退回 Get（历史行为，由内层自行决定过期条目回收口径）
- SingleFlightCache 自身也暴露 Peek——装饰器透明：外层再包一层装饰器时
  前置检查仍走非删除式读路径，不会再次废掉 stale 兜底
契约固化（issue 建议）：
- cachetest 新增 RunStaleIfErrorWithClock / RunStaleIfErrorRepeatedWithClock
  ——钉住「装饰器不得改变内层语义」：回源失败时必须回退过期真值（fail-open），
  且兜底可反复生效（兜底值留在表内，不是只生效一次）
- 接入点覆盖 LocalCache 直连与 SingleFlightCache 包装两种形态
回归验证：把前置检查回退到 Get 后，TestSingleFlightCache_StaleIfErrorPreserved、
TestSingleFlightCache_StaleIfError_RepeatedFallback 与 TestContract_StaleIfError
的两个包装子用例全部转红；LocalCache 直连子用例保持绿（直连本就没问题）。
Closes #72

- 文档：版本引用对齐 v0.22.1

## [v0.22.1] — 2026-10-24

### 修复(urlguard)：阻断 IPv6 特殊用途段/NAT64 段与 exotic IPv4 字面量形态

写面（CheckURL / CheckHost）此前放行两类可指向回环/内网的地址形态：
- IPv6 特殊用途段与 NAT64 段：64:ff9b::/96（64:ff9b::7f00:1 即 127.0.0.1
  的 IPv6 写法）、64:ff9b:1::/48、100::/64、2001::/32、2001:2::/48、
  2001:10::/28、2001:20::/28、2001:db8::/32、2002::/16
- exotic IPv4 字面量形态：0x7f000001 / 0177.0.0.1 / 127.1 / 2130706433
  ——net.ParseIP 一律返回 nil，旧实现把它们当普通域名放行；但宽松解析器
  （cgo resolver 的 inet_aton 语义、浏览器 URL 解析器）会当地址，
  写面放行 + 拨号层放行 = 一条回环绕过路径
- blockedCIDRs 补齐 IPv6 特殊用途段与 NAT64 段
- 新增 looksLikeExoticIPv4：词法级判定，命中即拒（fail-closed——不解码成
  IPv4 再走范围判定，cgo resolver 语义更宽），正常点分十进制仍由 net.ParseIP
  先判
- 拨号层 resolve 同步同源判定：不把 exotic 字面量推给 DNS
- 既有用例把 2001:db8::1 当「不在阻断清单」，按新口径改为用全球可达 IPv6
Closes #73
Closes #75

- CI(workflow)：入板 workflow 的 action pin 对齐 checkout v7

dependabot 的 github-actions ecosystem 只扫 .github/workflows/（directory: /），
扫不到 workflow-templates/ ⇒ PR #22 把 .github 自身的副本升到 v7，本体却停在 v5。
sync-project-hooks.sh 的 SRC 就是本体，会把陈旧本体回灌到 25 份副本、把升级 revert 掉。
本 PR 由对齐后的本体下发（oekit/.github#23，8ec56f00），各仓副本与本体逐字节一致。
全 org 无 pull_request_target / workflow_run 触发器 ⇒ checkout v7 的 breaking 面不触发。

- CI(workflow)：入板并发组按事件类型与条目号隔离

events 与 push:main 的 github.ref 同为 refs/heads/main，在 cancel-in-progress=true
下互相 cancel——实测各仓 push→main 的入板刷新被取消（pkg 24/31、core 23/36、
logx 19/29、metrics 21/30、kitex 20/33），大盘刷新只能靠 tag 推送或每小时整点
全量补跑兜底。
group 改为带上 github.event_name 与条目号：
- push 仍按 refs/heads/main 收敛，连续推送只跑最新一次
- issues / pull_request 按条目号隔离，并发开多个 issue 不再互相顶掉
不改 workflow 行为语义，只改并发分组键。规则本体在 oekit/.github 的
workflow-templates/issue-to-project.yml 维护，各仓副本与本体逐字节一致。


- 文档：版本引用对齐 v0.22.0

## [v0.22.0] — 2026-10-22

### 新增(stringx)：补 CSV 拆分原语 SplitCSVNonEmpty——拆 + trim + 丢空项，空串返回 nil

stringx.SplitByComma 的实现就是 strings.Split(s, ",")：不 trim、不过滤空项、
空串返回 [""]（1 个空元素）。名字叫「按逗号拆」，但「拆完顺手清干净」这层
留给每个消费方各写一遍——典型样板是 strings.Split → 逐项 TrimSpace → 跳过空项，
本轮在一个跨两仓的真实项目里就发现两份逐字相同的实现（core 的 executor 与
壳层的 CLI）。
新增 SplitCSVNonEmpty(s string) []string，把那层样板一次收进去：
- 逐项去掉首尾空白（strings.TrimSpace，含 Unicode 空白）
- 去掉空白后为空的项丢弃（"a,,b" → ["a","b"]）
- 空串返回 nil（不是空切片）——调用方普遍用 != nil / len()>0 区分「没配」
  与「配了空」，返回空切片会把两者混同；全是空白或空项的输入同样返回 nil
与 SplitWith 的区别（不要拿 SplitWith 顶替）：SplitWith 走 strings.Fields，
按**空白**切分，值里含空格时（如 glob 列表 "my glob"）会被静默拆成两项、
改变语义。CSV 值的空白是有意义的（"a b" 是一个值），所以这条面需要按逗号
切的专门原语，而不是拿现有函数拼。
SplitByComma 的既有语义不动（不 trim、不过滤空项）——两个原语语义各归其位。
测试：14 例口径表驱动 + nil/非 nil 契约钉死 + 不按空白切钉死 + 与
SplitByComma 的差异钉死
Closes #65

### 新增(pathx)：补两个缺失原语——名字通配匹配 MatchGlob 与单段文件名合法性判定 IsSafeFileName

pathx 此前只覆盖**路径**语义，缺两块消费方反复自写的面：
**MatchGlob**——`*` 可跨 `/` 的 glob 原语。MatchesPath 是 filepath.Match 语义
（`*` 不跨 `/`），而分支名 `feature/x`、ref 名 `refs/heads/x`、仓库全名
`owner/repo` 这类对象本身含 `/`，路径段语义答不了：`*` 匹配不到 `owner/repo`。
这让「排除所有」这类配置**彻底失效且毫无察觉**——exclude_globs 写 ["*"] 等于
没配，include_globs 写 ["*"] 反而把仓库全部过滤掉（语法合法、无报错、只是不生效）。
口径写死：`*` 匹配任意字符序列（可跨 `/`、可匹配空）、`?` 匹配**一个 rune**
（按字节切会把中文名切出半个字符）、其余字符按字面量比较、**不做 shell 风格
转义**（不引入 `[...]` 字符类）。与 MatchesPath 是两个语义各归其位的原语，
不互相取代。
**IsSafeFileName**——「这个名字能不能原样作为某目录下的单个文件名用」的判定。
消费方把外部输入（上传名、归档名、bundle 名）拼进 filepath.Join(dir, name)
时要的正是这个判定，而最自然的写法 filepath.Base(name) != name 是**错的**：
filepath.Base("..") == ".."、filepath.Base(".") == "." 都会被放行，name 随后被
Join 拼进路径就把路径指到 dir 的父目录。判定口径与 SanitizeRelStrict 严格档
同源（拒 `.`/`..` 段、绝对路径、盘符、UNC），外加「必须是单段」，且 fail-closed：
首两字符形如盘符的名字（`C:foo.bundle`）在 Windows 上是盘符相对路径，同样拒绝。
与 SanitizeFileName 语义相反——后者是**改写器**，拿它做回环校验会把「本来就不合法」
与「被消毒规则改了」混为一谈。
测试：表驱动口径表 + 与 SanitizeRelStrict 的一致性断言（避免同一仓里两份实现
再次漂移）+ 与 MatchesPath 的差异钉死（`*` 跨 `/` 一侧）+ 「排除所有」形态回归。
MatchGlob 另做 40 万例随机差分（对正则参考实现）——差分抓到一处真 bug：
模式里的 `*` 遇被匹配串里也有 `*` 时被当字面量放行（`*` 分支必须先于字面量
比较判断），已修并补进用例。
零第三方依赖。
Closes #63
Closes #64

- 文档：版本引用对齐 v0.21.0

- 矩阵 DESC 同步——middleware v0.13.0 起 accesslog 内建日志可关闭/改道（WithAccessLogSilent 只采不写、WithAccessLogLogger 注入宿主 logger 避免双写；静默下 skip/redact/sink/panic 记录照常），#38

## [v0.21.0] — 2026-10-22

### 新增(pathx)：文件名消毒原语 SanitizeFileName

展示名 / 存储名 / 上传原始名这条面此前是空的：pathx 只覆盖路径语义
（SanitizeRel / SanitizeRelStrict 答的是「这路径合法吗、越不越界」），
而文件名要答的是另一套问题（「这名字能不能安全显示和存储」）——
目录成分丢弃、控制字符丢弃、Windows 保留字符替换、结尾点去除、超长截断，
与 traversal 无关。
与 SanitizeRel 语义显式区分：同一段输入在两者下结论必然不同——
"C:\Users\me\报告.txt" 在 SanitizeRel 下是越界拒绝，在 SanitizeFileName 下是 "报告.txt"。
口径：
- 目录成分只取最后一段（"/" 与 "\" 同为分隔符，跨平台同判）
- 控制字符（r < 0x20 或 r == 0x7f）丢弃
- Windows 保留字符 : * ? " < > | 替换为 _（保留可读性与扩展名结构）
- 去首尾空白与结尾的点（Windows 目录项陷阱）；前导点保留（.gitignore）
- 空名 / 纯符号名回退为 fallback（缺省 "file"）
- 超长按**字节**上限截断（ext4 单目录项 255 字节口径），保留扩展名，
  截断点回落到 rune 边界不产出半个 UTF-8 字符；扩展名自己装不下时丢弃扩展名
  而不是留半个（"a.tx" 的扩展名是骗人的，下游按它判类型会判错）
- 非 ASCII 一律保留；非法 UTF-8 字节替换为 U+FFFD，保证输出是合法 UTF-8
选项：WithMaxFileNameLength(n)（<=0 不限长）、WithFileNameFallback(s)（逐字透传）
零第三方依赖（#58）

- 文档：版本引用对齐 v0.20.0

## [v0.20.0] — 2026-10-21

### 新增(urlguard)：精确 host 白名单（WithExactHosts）

pkg#48：WithAllowDomains 是后缀豁免——只能"放宽某域及其全部子域"，无法"钉死"
到具体 host；而 urlguard 是放行型模型（默认只阻断私网/回环/保留段与约定域名），
白名单外的任意公网域默认放行。于是"host 必须完全等于管理员配的那个 host"
（webhook 出口签名绑 host、内部平台多一个子域就多一个未审计入口、凭据与端点
绑定）在既有配置里根本没有对应表达。
- WithExactHosts(hosts...)：URL 的 authority（host[:port]）必须完全等于列表中
  某项，否则拒绝
- 与 WithAllowDomains 语义独立、可叠加：后者定"哪些域放宽阻断"，前者定
  "只允许哪些 host"；域豁免不解除钉死
- 命中后仍走 scheme 白名单与私网判定——钉死 host 不等于放行私网
- 比较口径：host 大小写不敏感、尾点不敏感；端口按数值比较且参与比较（配置
  不带端口时 URL 必须也不带端口，不替调用方猜默认端口）；userinfo 不参与比较
  （url.Parse 已把 "https://h@evil.com/" 的 userinfo 拆开，authority 是
  evil.com——正是"钉死 host"要挡的逃逸形态）
- 条目文法非法（空串、含路径/空白/用户信息、端口非法或越界）时 New 报错
  fail-fast；未设置时行为与既有完全一致，配了空清单 = 一个 host 都不许
- 判定位置：CheckURL 的 scheme 白名单之后、私网判定之前；DialFunc 仍只做 IP 级
  判定（DNS rebinding / TOCTOU 闭合），不参与白名单比对——拨号层的 addr 已补齐
  默认端口，按字面比会误杀
零第三方依赖（pkg 硬规则），仅标准库 net/url + strconv + strings。
Co-Authored-By: Murphy-Yi <zy84338719@hotmail.com>

### 新增(pathx)：仓库相对路径清洗与 traversal 拒绝原语（SanitizeRel / SanitizeRelStrict）

pkg#46：全家族无对应件，消费方各写各的——实测同一仓内三档口径（宽松 / 严格 /
只看 Clean 后的结果），哪些差异必要、哪些抄漏无从判断。本原语给基线，调用方在
基线上选严格档。
- SanitizeRel：统一分隔符、消除 "." 与 ".." 段；绝对路径（/foo、UNC、
  Windows 盘符）、越出根目录（../x、a/../../x）、清洗后为空（"."、a/..）一律
  ok=false
- 分隔符口径："/" 与 "\" 同为分隔符。按平台解释会让同一段载荷在 Linux 与
  Windows 上得到不同结论，而 Linux 才是服务端主环境——那正是 "..\..\" 形
  绕过最容易漏掉的地方
- SanitizeRelStrict：额外拒绝输入中显式出现的 ".." 段（a/../b 直接拒绝），
  防符号链接等文本 Clean 简化不掉的绕过形态
- 表驱动测试：两档共享判定（严格档只可能更严）+ 清洗结果必为无 "." / ".."
  段的相对路径
零第三方依赖（pkg 硬规则），仅标准库 strings。
Co-Authored-By: Murphy-Yi <zy84338719@hotmail.com>

- 文档(readme)：同步 pathx / urlguard 包级描述

新增公共 API 后同步 README 的包级说明：pathx 的相对路径清洗与 traversal 拒绝、
urlguard 的精确 host 白名单。
Co-Authored-By: Murphy-Yi <zy84338719@hotmail.com>

- 文档(stringx)：明确 TruncEllipsis 的 n 语义（n 或 n+1 个 rune）

pkg#46 的附带缺口：TruncEllipsis 的 n 是"截断到 n 个 rune"，**不含省略号**，
因此返回值可达 n+1 个 rune。调用方若把 n 当"总长度预算"用（如提交状态描述的
140 字符上限）会超出预算 1 个字符。
- 文档写明该口径，并指出预算语义场景的替代做法
- 补测试把这条口径钉住（截断时 n+1、未截断时 n）
Co-Authored-By: Murphy-Yi <zy84338719@hotmail.com>

- 文档：版本引用对齐 v0.19.0

## [v0.19.0] — 2026-10-21

### 新增(errors)：补 Errorf——带格式化的构造原语，支持 %w 包装

#47：pkg/errors 只有 Wrap（msg 不可插值），消费方要拼带动态信息的错误链
只能二选一：先 Sprintf 再 Wrap（格式化动词与参数分离，易错），或绕回
stdlib fmt.Errorf（放弃 kv 上下文）。本仓自身 fmt.Errorf 有 76 处。
- Errorf(format, args...) 与 fmt.Errorf 同型，支持 %w（可出现多个）
- nil-safe：%w 实参为 nil 时不进包装链，消息按 %v 渲染为 "<nil>"——
  stdlib 在此产出 "%!w(<nil>)" 噪声并丢掉整条包装链
- 格式化结果为空串时返回 nil（与 Wrap(nil, ...) → nil 同一取向）
- 多个 %w 时包装链是 Unwrap() []error，errors.Is/As 对链上每个错误都生效
- 消息渲染与 fmt.Errorf 逐字一致（差分测试以 stdlib 为参照实现对照）
Closes #47

### 修复(httpjson)：指数退避改用乘法+饱和封顶，移位溢出不再产出负延迟

#45：DoJSONWithRetry 的指数退避用 base << (attempt-1) 计算，attempt 较大时
int64 溢出成负数——负数不满足「> maxDelay」，于是绕过 MaxDelay 封顶，
time.After(负数) 立刻返回，该轮退避彻底消失（默认 BaseDelay=200ms 时
溢出点落在 attempt=37/39/40），封顶承诺同时被破坏。
- 改用乘法 + 饱和封顶，先封顶再放大，MaxDelay 成为真正的上界
- 抖动叠加同样做饱和：delay 接近 MaxInt64 时相加会溢出成负 duration
- 与同族 concurrency/retry 的指数退避收敛到同一口径（一个饱和一个回绕）
Closes #45

- 文档(matrix)：矩阵 DESC 同步——registry 根仓回归零第三方依赖，Nacos 实现已隔离到嵌套模块
- 文档(AGENTS)：首行 DESC 行同步到矩阵真相源——原文是陈旧快照
- 文档(matrix)：矩阵 DESC 修正——hertz 没有 WithReadyAddr（那是 kitex 的 API）
- 文档：版本引用对齐 v0.18.0

- 矩阵总览表 DESC 口径同步：core 新增加载入口按来源拆开（LoadFile 任意路径 / Unmarshal 字节源，v0.11.0）——与 LoadBootstrap 共享同一条解码线，严格模式诊断面与来源无关。

由 matrix-tools/scripts/gen_tables.py 生成。Refs: oekit/core#25

- 矩阵总览表 DESC 口径同步：pkg 新增 jsonrepair 抽对象/抽数组对称原语（v0.18.0）、tracing 新增 OTLP 双协议导出（v0.3.0）

## [v0.18.0] — 2026-10-20

### 修复(pkg)：jsonrepair 补抽数组原语，数组面兜底与对象面对称

对象面有抽取件、数组面没有——同一原语只做了一半：
- `ParseLenient` 的最后一层兜底只有 `ExtractObject`，数组文本被抽成「数组里的
  第一个对象」，对数组语义是错的
- `ExtractJSON` 对数组是「贪心到末个 `]`」，多段数组被拼成非法 JSON
- 两者叠加：模型一次输出多段数组 → 整段作废。失败形态是**静默的数据丢失**
  （上层拿到空列表按「本轮无候选」正常返回），不是显式报错
修法（对象面/数组面同构，取值口径统一为「取首个」）：
- 新增 `ExtractArray(s)` —— 抽文本中第一个平衡的 JSON 数组，与 `ExtractObject`
  同款字符串感知扫描。用括号栈而非单一深度计数：数组可嵌套数组（`[[1,2],3]`）
  也可含对象字段数组（`[{"b":[2]}]`），只数 `[` 会把内层 `]` 当收尾切出半截
- `ParseLenient` 兜底链补 `ExtractArray` 分支。顺序固定「对象面先、数组面后」：
  目标类型决定哪个形状能解析成功，而 `any`/`json.RawMessage` 这类通吃形状的目标
  必须保住对象面既有行为（`{"items":[1,2]}` 仍抽成整个对象，不能被内层数组抢走）
- `UnmarshalLenient`/`UnmarshalWithOpts` 的宽松闭合候选补 `ExtractArray`：
  散文里先出现 `{` 时 `ExtractJSON` 会挑中散文那个 `{`，数组候选补位
既有语义零改动：`ExtractObject` / `ExtractJSON` 的行为一字未动，既有调用方
不受影响；新增导出符号属 0.x 阶段 minor。
验收：多段对象/多段数组/散文包裹（对象与数组各一组）表驱动测试 + 对象面回归
用例 + 宽松模式数组面覆盖；`GOWORK=off go test ./...` 全绿，`make zero-dep` OK
（零第三方依赖）。

- 文档：安装行/当前版本行对齐最新 tag（pkg v0.17.0、timertask v0.2.1）

- 矩阵总览表 DESC 口径同步：concurrency 新增 retry.DoValue 泛型伴生（带返回值的重试，Do 委托给它使退避语义单一实现）、core 新增 config 严格模式（未知键拒载 + 嵌套路径 + 合法键清单 + 近似拼写建议）。

由 matrix-tools/scripts/gen_tables.py 生成（matrix.py 的 DESC 是唯一真相源）。Refs: oekit/matrix-tools#50


## [v0.17.0] — 2026-10-20

### 修复(pkg)：Masker 短值全量打码（安全）+ Inspect 条数核对不再静默跳过 + NewWriter 时钟注入

对应 issue #31 / #32 / #33（pkg v0.16.0 新增能力的复审缺陷）：
- **#31（安全）** stringx.Masker 原先的 keep 收缩逻辑（min(Keep, n/2)）在
  n ≤ 2*Keep 时让头尾两段保留位首尾相接覆盖整段原文——原值被完整留在输出里，
  脱敏等于没生效（Keep=4 时 Mask("01234567") → 8 字符原文全部可见）。
  改判据：n < Keep*4 一律整段全量打码（对齐 guardx/logredact 的
  HintMinLenRatio=4 口径），n >= Keep*4 才保留首尾各 Keep 个。判据按**运行期
  实际命中长度**判定。
- **#32** pack.Inspect 的条数核对在 counts 键与文件名对不上时被静默跳过、体检
  报「通过」。改：文件名精确匹配 → 去扩展名基名宽松匹配 → 仍匹配不上则显式
  报出「条数未核对」，不再假通过。
- **#33** pack.NewWriter 的 created_at 写死 time.Now()，导出无法做确定性单测。
  加 WithClock(now func() time.Time) Option（对齐 pkg/cache 口径），两参写法
  向后兼容。
零第三方依赖保持（go.mod require 段仍为空）。

- 文档：安装行/当前版本行对齐最新 tag（pkg v0.16.0 / timertask v0.2.0 / data v0.15.0）
- 文档：矩阵总览表 DESC 口径同步（pkg v0.16.0 新增 cli/pack 子包与 stringx Masker、LocalCache 可选形状；timertask v0.2.0 Redis 后端迁嵌套模块 + local cron 调度器；data v0.15.0 壳同步）

## [v0.16.0] — 2026-10-19

### 新增(pkg)：新增 cli/pack 子包、stringx Masker，LocalCache 补可选形状

对应 issue #24 / #25 / #26 / #27：
- feat(cli)：可单测的子命令分发原语（App/Command + 退出码 0/1/2 写死在包里，
  不读 os.Args 不调 os.Exit，输出流全由调用方给）
- feat(pack)：迁移包容器原语（zip + manifest + 每文件 SHA-256 + JSONL 流式
  读写 + Inspect 体检一次列全问题，区分包损坏与版本不认识）
- feat(stringx)：正则脱敏 Masker（形态标签 + 保留首尾各 ≤4 字符定位线索 +
  不可复原，规则表由调用方完全提供）
- feat(cache)：LocalCache 可选形状（NoBackgroundSweep 无后台 goroutine +
  CloneOnRead 读命中返回副本 + EvictPolicy LRU + GetEx 命中标志）；
  顺序结构由切片 swap-remove 改 container/list（原实现删队头会打乱相对顺序）
- 文档：README 包清单与计数（23 合 1）、AGENTS 本仓包含清单、CHANGELOG v0.16.0
零第三方依赖硬规则保持（go.mod require 段仍为空）。
* fix(pack): 错误文案不以标点结尾（staticcheck ST1005）

- 文档：矩阵总览表依赖列修正——单行 require 解析（logx/bootstrap 补 core，matrix-tools 解析修复）
- 文档：矩阵总览表重生成（gen_tables 新格式对齐）
- 文档：矩阵总览表 DESC 口径同步（httpx v0.8.0 urlguard 收敛，matrix-tools#16）

## [v0.15.1] — 2026-10-19

### 修复(urlguard)：缺省阻断清单补齐保留段（TEST-NET/benchmarking/192.0.0.0/24）与 .localdomain

对齐 httpx#11 参考实现的保留段清单——这些段本就不是合法出站目标，漏判属防护缺口。
纯收紧型变更，无合理场景受影响。

- 文档：矩阵总览表 DESC 口径同步（httpx v0.8.0 urlguard 收敛 / pkg v0.15.0 urlguard）
- 文档：矩阵总览表 DESC 口径同步（pkg v0.15.0 urlguard，包清单 21 合 1）

## [v0.15.0] — 2026-10-19

### 新增(urlguard)：出站 SSRF 防护原语——两层防护下沉（httpx#11）

- 层 1 CheckURL：scheme 白名单 + host 字面量（私网/loopback/链路本地含云元数据/
  CGNAT/ULA/组播保留、.internal/.local/localhost 域名），IPv4-mapped IPv6 归一
- 层 2 DialFunc：解析 IP 全量预检 + 实际 RemoteAddr 端点核对（闭合重解析 TOCTOU，
  Happy-Eyeballs 覆盖），违规即关连接
- 构造态豁免 WithAllowPrivate/WithAllowCIDRs/WithAllowDomains/WithAllowedSchemes，
  无进程级全局开关；快捷入口 IsBlockedURL/SafeDialFunc；仅标准库

- 文档：矩阵总览表 DESC 口径同步（core v0.9.2 env 卫生改纯静态）
- 文档：矩阵总览表 DESC 口径同步（middleware v0.12.0 panic 恢复点访问记录 / concurrency v0.17.0 KeyedLimiter.Create）
- 文档：矩阵总览表 DESC 口径同步（concurrency v0.16.0 / redisx v0.9.0 / middleware v0.11.0 / core v0.9.x）
- 文档：AGENTS.md「版本与发版」改为 PR 流程口径

本仓 main 已开分支保护（要求 PR + `enforce_admins`），直推会被
`GH006 Changes must be made through a pull request` 拒掉。原文档写的
"发版 = 打 annotated tag 推远端"已不成立——tag 必须落在合入 main 之后，
否则 `go get` 拉到的是没进过 CI 的代码。
本文件由 matrix-tools#8 的 `gen_agents.py` 模板重新生成（全 org 内容一致）。


## [v0.14.0] — 2026-10-18

### 新增(cache)：dynamic backfill TTL + stale-if-error; feat(encoding): Base64 bytes siblings (Closes #11, Closes #12)

- pkg#11：SourceFuncWithTTL 动态回填 TTL（expires_in 场景）+ StaleIfError/OnStale
  fail-open（data#4 语义随 data v0.9.0 重构静默回归的本账）；GetOrLoad 首查改
  非删除式（stale 兜底前提），缺省行为逐位不变
- pkg#12：Base64DecodeBytes/Base64EncodeBytes 字节↔字节入口，零编译提示误用的
  类型级修复

- CI：tag 推送触发门禁（v* → 共享 go-ci，含 tidy/build/test/CHANGELOG 一致性——.github#5）

- 文档：矩阵总览表 concurrency v0.14.0 行 + AGENTS 当前版本行对齐（refresh_versions 新门禁）
- 文档：矩阵总览表 redisx 行同步 DESC v0.8.0 口径（Semaphore 信号量/锁续期）

## [v0.13.0] — 2026-10-18

### 新增(cache)：LocalCache injectable clock + cachetest RunWithClock suite (Closes #9)

- LocalConfig.Now 时钟注入：TTL/NullTTL 全路径（Get/GetOrLoad/Exists/MGet、
  Set/SetWithTTL/null marker、后台清扫）路由注入时钟，假时钟确定性测试
- cachetest：FakeClock + RunWithClock 假时钟 TTL 套件（过期闭边界/TTL=0
  永不过期/null 过窗重源/重写刷新过期）；清扫 ticker 只定节奏、判定走注入
  时钟（避免假时钟下按真实时间全量误删）

- CI：issue/PR 开闭即时入板 org 大盘（复用 oekit/.github sync-projects）
- CI：issue/PR 开闭即时入板 org 大盘（复用 oekit/.github sync-projects）

- 文档：矩阵总览表与 AGENTS.md 同步 matrix DESC 口径（core transport/chain/config、httpx Selector/p2c/codec、breaker 三态族、middleware chainmw、logx 新 API、bootstrap 就绪门）；AGENTS bootstrap 路径修正为 New + Run(fn)

## [v0.12.0] — 2026-10-18

### 新增(errors)：New/As/Is/Unwrap stdlib-shape shims (Closes #8)

- 503dde6

- 文档：矩阵总览表重新生成（matrix-tools）

- docs+ci: 零第三方依赖硬规则明文化并强制

- AGENTS.md/README:go.mod 永不出现 require,仅标准库;需要三方能力的基础工具拆独立仓(先例 kitx)
- Makefile: make zero-dep(GOWORK=off go list -m all 仅允许本模块一行),挂进 make ci
- CI: 新增 Zero Third-Party Deps job(分支保护 required check)
- gen_agents 模板补全矩阵口径:pkg 零三方依赖仓


## [v0.11.0] — 2026-10-17

### 新增(jsonrepair)：UnmarshalLenient/WithAllowPartialArray 宽松模式——尽力闭合截断的数组/对象（Closes #7）

- 补悬空字符串引号(含 EOF 悬挂转义)、键位截断补 :null({"Host 结尾)、
  键位串闭合后冒号缺失补 :null("c" 结尾)、丢悬空逗号、悬空冒号补 null、按栈闭括号
- partial 标记区分「完整解析」与「截断前缀」;顶层散串不救;纯垃圾仍报错
- 保守面零漂移:UnmarshalWithOpts 无选项 ≡ 既有 Unmarshal(复用三级链)
- 此前 Repair 只补括号,截断在字符串中段("HostName":"trunc 结尾)无解

### 新增(mathx)：Unstable 单边向上抖动 Upward/UpwardDuration/UpwardFloat64（Closes #6）

结果 ∈ [v, v(1+dev)]:重试退避「不得比基准更早」语义——对称抖动会把退避
压短成更密集的重试风暴。deviation=0 恒等返回,仅适用正数语义,与 Around 同口径。

- 文档：矩阵总览表与 AGENTS.md 同步（matrix-tools 24 模块）

## [v0.10.0] — 2026-10-17

### 新增：移除 uidgen/validator——拆分至 github.com/oekit/kitx（BREAKING）；第三方依赖归零

- uidgen（Snowflake/UUID/随机串）与 validator（go-playground 门面）整体迁出，机械替换 import 即可迁移，包名与行为契约不变
- go.mod 不再有任何 require（v0.9.3 拆出前有 go-playground 全链 9 个模块），go.sum 清空
- 详见 kitx v0.1.0

- CI：工具链降至 1.24 实测最低支持版本；changelog MultiError 段纠正回 v0.9.1（其代码随 v0.9.1 发布）

- 文档：安装行对齐 v0.9.3

## [v0.9.3] — 2026-10-17

- 构建：go 最低版本实际降至 1.24——锁定四个依赖的 go1.24 兼容版（v0.9.2 事故修正）
- 构建：go 最低版本降至 1.24（golangci 同步）

- 文档：矩阵总览表重新生成（matrix-tools）
- 文档：矩阵总览表重新生成（matrix-tools）
- 文档：安装行对齐 v0.9.1

## [v0.9.1] — 2026-10-17

### 修复(errors)：MultiError.Errors() 防御性拷贝——外部修改/append 不得影响内部状态与 COW 前提

Closes #5

- 文档：安装行对齐 v0.9.0
- 文档：矩阵总览表重新生成（matrix-tools）

## [v0.9.0] — 2026-10-17

### 新增(container)!：泛型容器包 container 收编原 collectionx——Set/Ring/Queue/SafeMap 签名不变仅换 import 路径，新增 Map（sync.Map 背书）/Slice（RWMutex）并发安全容器，collectionx 子包删除

- 4d85bc5

### 修复(lint)：singleflighttest 契约桩补 nolint:unparam（恒返回零值聚焦去重语义）

- 0e6d430

### 修复(lint)：singleflighttest 两处未用 ctx 形参改 _（unparam，v0.8.0 存量）

- 86db61c

## [v0.8.0] — 2026-10-17

### 修复(errors)!：奇数 kv 渲染 !BADKEY；feat singleflighttest 契约套件

formatKV 对齐 slog 约定不静默丢参（#3）；
singleflighttest.Run 固化双实现语义契约，供 concurrency 引用（#4）

## [v0.7.0] — 2026-10-16

### 新增(timex,stringx)：时间段边界函数 + trie 多模式替换器

timex: BeginningOfX/EndOfX 七对（分/时/日/周/月/季/年），纯函数保持时区；
EndOfX=下一周期起点-1ns；墙钟 time.Date 重建规避 Truncate 的非整小时
时区陷阱；月末/季末以月初为基准走 AddDate 规避大小月/闰月归一化；
周起始日显式传参无全局状态。
stringx: rune 级 trie 多模式替换器，同起点恒取最长匹配；pattern 空串/
非法 UTF-8 报错；Replace 按 rune 边界迭代，未命中原字节保留；构造后
只读可并发；NewMask 敏感词遮罩薄壳。附 fuzz 回归种子（非法 UTF-8
字节保留路径）。

### 修复(stringx)：golangci 门禁——文件头注释空格 + repls 索引 nolint:gosec（等长已校验）

- 500cde1

- 文档：README 矩阵表 regen + 安装行对齐最新 tag（matrix-tools）
- 文档(cache)：MGet 注释如实化——TTL=0 无 cleanup goroutine，过期条目仅经 Get/Exists 双检路径惰性回收
- 文档：README 矩阵表 regen + 安装行对齐最新 tag（matrix-tools）

- 格式(timex,stringx)：doc.go 注释 gofmt（CI fmt 门禁）

## [v0.6.0] — 2026-10-16

### 新增(cache)：BatchGetter 批量读取可选能力接口——MGet 缺席语义对齐 (nil,nil) 双关

- 接口契约：map 只含命中真值的键（不存在与 null marker 一律缺席，消费方
  回退 GetOrLoad 享受 marker 短路）；纯读不回源不回填不产生 marker；
  反序列化失败整批报错（与单键 Get 同口径）
- LocalCache：单读锁区间批量读（过期条目按缺席，回收留双检路径）
- SingleFlightCache：委托内层批量能力（装饰器不得退化批量），内层无批量
  时退化为逐键 Get

- 文档：矩阵总览表重新生成（matrix-tools）
- 文档：矩阵总览表重新生成（matrix-tools）
- 文档(readme)：开头行包计数 20 合 1 → 21 合 1（对齐矩阵表与 AGENTS.md）
- 文档：README 矩阵表 regen + 安装行对齐最新 tag（matrix-tools）

## [v0.5.0] — 2026-10-16

### 新增(collectionx)：泛型容器微件——Set/Ring/Queue/SafeMap

参考 go-zero collection 容器件全面泛型化，语义按 Go 习惯收敛：
- Set 集合代数（并/交/差），非并发安全
- Ring 定长环形缓冲：溢出覆盖最旧且保序 FIFO（保留最新 n 条），
  优于 go-zero 错位乱序语义
- Queue 无界阻塞队列（cond 实现，Close 唤醒全部等待者，幂等）
- SafeMap RWMutex 并发映射（GetOrSet 原子语义）

- 文档：CHANGELOG/AGENTS 补新能力条目 + 矩阵 README regen（matrix-tools DESC 同步）

## [v0.4.0] — 2026-10-16

### 新增(cache)：收编数据访问缓存域——CacheAside 接口 + LocalCache/SingleFlightCache + cachetest 契约套件

自 github.com/oekit/data/cache 整体下沉（data 将转门面引用各子仓，
接口必须先于实现下沉基础包，否则 data→redisx 与 redisx→data 成环）：
- Cache[T]/CacheAside[T]/SourceFunc[T] 接口 + 缓存键构造（原样搬迁）
- LocalCache/SingleFlightCache 实现瘦身以保 pkg 零 oekit 内部依赖：
  logx.Delegating → log/slog（uidgen 先例）、async.GoSafe → 包内 goSafe
  （内联 recover，pkg 不能反向依赖 concurrency）、singleflight fork 至
  internal/singleflight（stdlib-only 泛型实现，concurrency 侧修 bug 须同步）
- cachetest 契约套件随接口迁入 cache/cachetest；Checker 既有不变
- 矩阵 README 同步 data 行（上轮 regen 漏推补齐）

- 文档(release)：修正版本文档——v0.3.0（上一次批量脚本 fallback 笔误回填目录名）
- 文档(release)：版本文档对齐最新 tag pkg

## [v0.3.0] — 2026-10-16

### 新增(contextx)：ValueOnlyFrom

剥离 Done/Err/Deadline 的只传值轻量 ctx：Value 透传 parent，
请求取消/超时不传播到后台任务。

### 新增(encoding)：UnmarshalUseNumber 防 float 精度丢失

UnmarshalUseNumber/UnmarshalFromStringUseNumber 显式入口：数字解析为
json.Number 保留原文字面量，js 风格 53 位整数精度不再截断 >2^53 的
金额/雪花 ID；尾部残留数据仍视为错误（与 Unmarshal 同源同语义）。

### 新增(mathx)：Unstable 抖动值

NewUnstable deviation 夹到 [0,1]，deviation=0 恒等返回；
Around/AroundDuration/AroundFloat64 返回 v∈[v(1-dev), v(1+dev)]，
缓存 TTL 打散防集中失效。

### 新增(errors)：Chain/In 组合工具

Chain 顺序执行、首错即停并返回（前一步失败后续无意义的有序清理链）；
In 报告 err 是否 errors.Is 匹配任一 target。

### 新增(bloomx)：本地布隆过滤器

New(estElements, fpRate) 按标准公式算 m/k，非法入参归一为文档化默认值
（est<=0→1024、fpRate∉(0,1)→0.01），不返回 error；Kirsch-Mitzenmacher
双哈希（h1/h2 源自 encoding.Fnv1a64，消费前过 murmur3 finalizer——
FNV 低位雪崩弱，实测不过 finalizer 1% 目标误判实测 6.6%，过之后 0.28%）；
sync.RWMutex 并发安全，m 向上取 2 的幂配合奇数 h2 保证完整覆盖。

- 文档：CHANGELOG 版本节 v0.2.0 → v0.3.0（v0.2.0 tag 已被 bufiox/strmap 发布占用）

AGENTS.md 当前版本与 README 安装行同步 v0.3.0；AGENTS.md 中
「v0.2.0 吸收自 cloudwego/gopkg」为历史沿革标注（对应 bufiox/strmap
的 v0.2.0 发布），保持不动。

- 文档：小件批次文档同步——AGENTS/README 子包表 +2、CHANGELOG v0.2.0

bloomx/contextx 入表；errors(Chain/In)/mathx(Unstable)/encoding(UseNumber)
条目补记；包计数 18→20（19/19→21/21 测试包）；内部边补 bloomx→encoding。


## [v0.2.0] — 2026-10-16

### 新增：v0.2.0 吸收 cloudwego/gopkg——bufiox 零拷贝缓冲 IO + strmap GC 友好只读字符串 map

- 7d77556

### 修复：golangci-lint——strmap assignOp（gocritic）+ bufiox 测试变量遮蔽（govet shadow）

- 0a31e77

### 重构：来自 PigeonBox/kit 移植实践的两处反馈（validator 命名 + 测试 lint）

1. validator/playground.go: 文档注释与实际导出名对齐——
   方法签名本为 Validate_()，但注释写作「// Validate 底层验证器实例」，
   与包级快捷函数 validator.Validate(obj) 在阅读时极易混淆；
   改为「Validate_ 返回底层验证器实例」，显式表意底层实例，
   与包级函数/惯用调用区分开。
2. cache/checker_test.go: 裸 go c.Check(...) 丢弃 error 返回值，
   在 errcheck/golangci-lint 下报 lint；包一层 go func(){ _ = ... }()
   显式标注有意忽略，测试语义不变（该测试已验证通过）。
来源：PigeonBox/kit（Apache-2.0）自 oekit 移植后的实践反馈，
双向保持可同步。仅注释与测试代码变更，不改任何导出 API。

- 构建(deps)：bump github.com/go-playground/validator/v10

Bumps [github.com/go-playground/validator/v10](https://github.com/go-playground/validator) from 10.27.0 to 10.30.5.
- [Release notes](https://github.com/go-playground/validator/releases)
- [Commits](https://github.com/go-playground/validator/compare/v10.27.0...v10.30.5)
---
updated-dependencies:
- dependency-name: github.com/go-playground/validator/v10
  dependency-version: 10.30.5
  dependency-type: direct:production
  update-type: version-update:semver-minor
...
Signed-off-by: dependabot[bot] <support@github.com>
Co-authored-by: dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>


- CI：dependabot 全量分组更新（gomod weekly 单 PR，根治逐包 PR 洪峰）
- CI：CI 工具链 1.24 → 1.26（go.mod 语言版本不变；validator 新依赖链要求 go>=1.26）

- 文档：README 重写——写清仓库职责/核心能力/快速使用/模块边界

## [v0.1.3] — 2026-10-15

- 构建：最低 Go 版本降至 1.24（依赖相应调整；CI/lint 同步）

## [v0.1.2] — 2026-10-15

- 构建：最低 Go 版本降至 1.26（依赖相应调整；CI/lint 同步）

- 文档：README 深化——功能特性清单 + 分场景用法 + 注意事项
- 文档：README 增加 AI Agent 入口行
- 文档：README 增加快速使用示例与 AI Agent 入口
- 文档：新增 AGENTS.md——oekit 适配版开发规范（硬规则速览 + 本仓范围）

## [v0.1.0] — 2026-10-15

### 新增：初始版本——自 ekit v0.34.0 拆分稳定化

由 ekit v0.34.0（git.enjoye.top/enjoydream/ekit，冻结维护）拆分而来：
- 模块路径统一为 github.com/oekit/pkg
- 经多轮拆分/合并收敛至当前模块边界
- 组织基建就位：共享 CI workflow、golangci v2、Dependabot
