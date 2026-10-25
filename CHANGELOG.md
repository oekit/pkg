## [v0.28.0] — 2026-10-25

- feat(pagination): 补 Policy.NormalizeLimit——条数口径的归一化入口，替代 Normalize(1, v) 那种把页长当条数用的绕法（#115）

## [v0.27.0] — 2026-10-25

- feat(shellx): 补 POSIX shell 参数转义原语——把外部输入（文件名、路径、主机名、用户给的参数）拼进 shell 命令时，仓里只有 pathx 的「路径合法吗」，**「怎么写进命令行才不被 shell 解释」这一层是空的**，消费方于是各写一份或干脆不写；不写的后果是实测级的：`"a.txt; rm -rf /"` 是**两条命令**而不是一个文件名，含空格的路径被拆成**两个参数**（文件写到别处，而命令看起来「成功」了），`../etc/x` 落到预期之外的目录。新增 `pkg/shellx`：`Quote(s)` 把任意字符串转义成**一个** shell word（POSIX 单引号包裹——单引号内 shell 不做任何解释，唯一要照顾的是引号本身：关掉、转义、重新打开；**空串返回 `''`**，空参数仍是一个参数，丢掉会让 `cmd '' arg` 变成 `cmd arg`、参数整体错位），`QuoteArgv(args...)` 逐个转义后空格连接；纯转义、不做策略判断（换行/越界各有各的规则，由调用方决定）。与 pathx 是两个不同问题、互补而非重复：pathx 问「路径合法吗」，shellx 问「怎么写进命令行不被解释」，路径类输入通常两道都要过（#112）
- **下游怎么改**：把外部输入拼进 shell 命令的调用点，改为 `shellx.Quote(v)` / `shellx.QuoteArgv(args...)`；本地已有的兜底转义函数可直接删除（回收后只有一处口径）。⚠️ **只加转义不加路径闸门不够**：引号能防注入，但 `../` 仍能写到别处；只加闸门不加转义则 `;` 照样注入。⚠️ **转义会改变合法输入的行为**：含空格的路径之前是「坏」的（被拆成两个参数、写错地方），修完才真正写到目标 —— 这类修复在真实调用里才看得出来，单测覆盖不到。

## [v0.26.0] — 2026-10-25

- feat(convert): 客户端 IP 解析收紧——**默认不采信 X-Real-IP**（单跳头无法区分「代理写的」与「客户端自带的」，网关默认透传客户端自带的 ⇒ 客户端可任意改写自己的客户端 IP，限流/锁定绕过 + 审计 IP 可改写，#106）：ResolveClientIP 去掉 xri 入参（BREAKING），确需采信改用 ResolveClientIPWithXRI / GetClientIPWithXRI / GetClientIPWithProxiesAndXRI；**代理头候选值做合法性校验与端口归一**（任意字符串进限流/锁定键 = 计数桶绕过 + 日志注入；带端口的候选与裸 IP 是两个分桶键，非法段不再进返回值，#104）；**可信代理裸 IP 条目归一为单主机网段**（此前被静默忽略：配置不生效且无任何告警，#104），NewTrustedProxySet 构造期对非法条目 fail-fast；**可信代理预解析** TrustedProxySet（此前每请求逐条 ParseCIDR 整份清单，#107），ResolveClientIPWithSet 与清单形态采信结果逐字一致；feat(encoding): 补 base64 无填充 URL 安全变体的字节入口 Base64RawURLEncodeBytes / Base64RawURLDecodeBytes（#105）
- **下游怎么改**：① 本就**不采信** X-Real-IP 的部署（如先前为绕开本缺口而显式传空串 xri 的）⇒ **删掉那个实参**即可（`ResolveClientIP(remote, xff, trusted)`）；**不要**改用 `ResolveClientIPWithXRI`——那等于把「客户端可任意改写自己的客户端 IP」这个洞请回来。② 确需采信 X-Real-IP 的部署才用 `ResolveClientIPWithXRI` / `GetClientIPWithXRI` / `GetClientIPWithProxiesAndXRI`，并自行承担「该头无法区分代理与客户端」的风险。

## [v0.25.2] — 2026-10-25

- feat(convert): 抽出框架无关的客户端 IP 解析纯函数 ResolveClientIP——GetClientIP/GetClientIPWithProxies 的入参都吃 *http.Request，非 net/http 框架（Hertz 等）用不了，于是每个仓都自己手写一份等价实现、可信代理采信口径逐仓漂移；新增纯函数承载全部采信判定（XFF 从右向左跳过可信代理、伪造值不采信、直连不可信时忽略一切代理头），net/http 适配层改为委托它，既有签名与行为逐字不变；另导出 DefaultTrustedProxies()（返回副本）供框架适配层取同一份默认可信网段（#95）
- feat(pagination): 分页口径可按服务声明——新增 Policy（默认页长/上限/超限语义由服务自己声明，零值口径等同包级默认），超限的两种语义都表达（OverMaxClamp 钳到上限 / OverMaxFallback 回退默认页长，响应条数不同属可观测差异），MaxPageSize < 0 表示不限上限；此前默认页长/上限写死在包级常量、超限只有一种语义，导致没有任何一个消费方能逐字等价切到上游实现（#100）

## [v0.25.1] — 2026-10-25

- feat(encoding): 补 base64 无填充（Raw）变体——现有四个变体全是**带填充**形态（base64.StdEncoding / base64.URLEncoding），没有 Raw（无填充）那一套；而 JWT / OIDC / PKCE 协议用的就是无填充 base64url（base64.RawURLEncoding，RFC 7515 §2：base64url 编码时省略 '=' 填充，填充字符在 JWT 里不允许出现）——拿现有的 Base64URLDecode 去解 JWT 段会返回 "illegal base64 data at input byte N"，**包里的方法名与协议实际用到的变体不对应**，收编时极易踩坑（把 RawURLEncoding 换成 Base64URLDecode 就静默解不出来）。新增 Base64RawEncode / Base64RawDecode / Base64RawEncodeBytes / Base64RawDecodeBytes（base64.RawStdEncoding）与 Base64RawURLEncode / Base64RawURLDecode（base64.RawURLEncoding），命名与 stdlib 对齐；现有带填充方法的 godoc 点明「本方法是带填充变体，JWT/OIDC 协议段请用 Raw 变体」。纯新增，现有方法签名与行为不动（#96）

## [v0.25.0] — 2026-10-24

- fix(cache): SingleFlightCache 把一次未命中计成两次 Misses（#90）
- feat(urlguard): 补自定义 IP/host 判定钩子（pkg#91）

## [v0.24.0] — 2026-10-24

- feat(cache): 补观测计数面 Stats 与按调用注入回源——新增 cache.Stats 观测计数快照（Hits/Misses/Evictions/Expirations/Size）与可选能力接口 cache.StatsProvider（读路径计数，null marker 按未命中口径，容量淘汰与 TTL 过期回收分开计）；新增可选能力接口 cache.SourceInjector 与 GetOrLoadWithSource / GetOrLoadWithSourceTTL（回源函数由调用方在调用点给出，SingleFlightCache 保持单飞语义）；cachetest 补 RunStatsWithClock 与 RunSourceInjectionWithClock 两条契约（#76、#77）

## [v0.23.1] — 2026-10-24

- feat(jsonrepair): 导出 StripCodeFence 原语——围栏语义单源从下游副本落回上游，下游 3 处 textutil.StripCodeFence 改为委托即可（#74）

## [v0.23.0] — 2026-10-24

- fix(cache): SingleFlightCache 包装 LocalCache 时静默废掉 StaleIfError——前置检查走 Get（LocalCache 的惰性删除路径）把 stale 兜底需要的过期真值提前删掉；新增 LocalCache.Peek 非删除式读（可选接口 PeekGetter 外置，不破坏其它后端实现）+ SingleFlightCache 前置检查改走 Peek；cachetest 补 RunStaleIfErrorWithClock / RunStaleIfErrorRepeatedWithClock 两条契约（钉住「装饰器不得改变内层语义」）

## [v0.22.1] — 2026-10-24

- fix(urlguard): exotic IPv4 字面量形态一律拒——0x7f000001 / 0177.0.0.1 / 127.1 / 2130706433 等形态 net.ParseIP 一律返回 nil，旧实现把它们当普通域名放行，但宽松解析器（cgo resolver 的 inet_aton 语义、浏览器 URL 解析器）会当地址，写面放行 + 拨号层放行 = 一条回环绕过路径；新增 looksLikeExoticIPv4 词法级判定，命中即拒（fail-closed——不尝试解码成 IPv4 再走范围判定），拨号层 resolve 同步同源判定，不把它推给 DNS（#73）
- fix(urlguard): 缺省阻断清单补齐 IPv6 特殊用途段与 NAT64 段——64:ff9b::/96（NAT64 well-known prefix，64:ff9b::7f00:1 即 127.0.0.1 的 IPv6 写法）、64:ff9b:1::/48（NAT64 local-use）、100::/64（discard-only）、2001::/32（Teredo）、2001:2::/48（benchmarking）、2001:10::/28（ORCHID）、2001:20::/28（ORCHIDv2）、2001:db8::/32（文档段）、2002::/16（6to4，可内嵌任意 IPv4 含私网/回环）此前一律放行；纯收紧型变更，无合理场景受影响（#75）

## [v0.22.0] — 2026-10-22

- feat(pathx): 补两个缺失原语——名字通配匹配 MatchGlob 与单段文件名合法性判定 IsSafeFileName；feat(stringx): 补 CSV 拆分原语 SplitCSVNonEmpty——拆 + trim + 丢空项，空串返回 nil（#63、#64、#65）

## [v0.21.0] — 2026-10-22

### feat(pathx)：文件名消毒原语 `SanitizeFileName`——展示 / 存储 / 上传原始名这条面此前是空的（[#58](https://github.com/oekit/pkg/issues/58)）
- 展示名 / 存储名 / 上传原始名这条面此前是空的：pathx 只覆盖**路径**语义（`SanitizeRel` / `SanitizeRelStrict` 答的是「这路径合法吗、越不越界」），而文件名要答的是另一套问题（「这名字能不能安全地显示和存储」）——目录成分丢弃、控制字符丢弃、Windows 保留字符替换、结尾点去除、超长截断，与 traversal 无关
- 与 `SanitizeRel` 语义显式区分：同一段输入在两者下结论必然不同——`C:\Users\me\报告.txt` 在 `SanitizeRel` 下是越界拒绝（`ok=false`），在 `SanitizeFileName` 下是 `报告.txt`；文件名这条面没有 `ok=false`——消毒后总有名字可给（空则回退）
- 消毒口径（顺序即口径）：
  - **剥掉目录成分**：只取最后一段，`/` 与 `\` 同为分隔符（跨平台同判——上传文件名常来自 Windows 客户端或浏览器，只认 `/` 会让盘符路径原样落进存储名）
  - **丢弃控制字符**：r < 0x20 或 r == 0x7f
  - **Windows 保留字符**（`: * ? " < > |`）替换为 `_`（保留可读性与扩展名结构）
  - **去首尾空白与结尾的点**：尾部的点与空白在 Windows 上是目录项陷阱（`file.` 落盘成 `file`）；首部只去空白——前导点是合法名字的一部分（`.gitignore`）
  - **空名 / 纯符号名回退**：清洗后为空，或通篇没有字母与数字（`***` → `___`）⇒ 回退为 fallback（缺省 `file`）；纯符号名落盘无碍但对展示无意义
  - **超长截断**：按**字节**上限（缺省 255，ext4 单目录项口径——按字符数卡在中文名下会超限落盘失败）截断且**保留扩展名**，截断点回落到 rune 边界不产出半个 UTF-8 字符；扩展名自己装不下时**丢弃扩展名**而不是留半个（`a.tx` 的扩展名是骗人的，下游按它判类型会判错）
- 非 ASCII 字符（中日韩等）一律保留——展示名与存储名的核心诉求就是别把中文名弄丢；非法 UTF-8 字节替换为 U+FFFD，保证输出是合法 UTF-8
- 选项：`WithMaxFileNameLength(n)`（n <= 0 不限长，单位**字节**）、`WithFileNameFallback(s)`（回退名原样透传，不参与消毒、不受长度上限约束）
- 零第三方依赖（#58）
- 测试：7 例（口径表驱动 + 输出必为合法 UTF-8 + 字节上限截断 + 扩展名保留与丢弃 + fallback 逐字透传 + 输入无副作用 + 缺省上限）

## [v0.20.0] — 2026-10-21

- feat(pathx): 仓库相对路径清洗与 traversal 拒绝原语 SanitizeRel / SanitizeRelStrict——全家族无对应件，消费方各写各的（实测同一仓内三档口径漂移：宽松 / 严格 / 只看 Clean 后的结果），本原语给基线、调用方在基线上选严格档；统一分隔符（正斜杠与反斜杠同为分隔符，跨平台同判——按平台解释会让同一段载荷在 Linux 与 Windows 得到不同结论，而 Linux 才是服务端主环境，那正是双点反斜杠形绕过最容易漏掉的地方）、消除 . 与 .. 段，绝对路径（/foo、UNC、Windows 盘符）、越出根目录（../x、a/../../x）、清洗后为空（.、a/..）一律拒绝；严格档额外拒绝输入中显式出现的 .. 段（a/../b 直接拒绝），防符号链接等文本 Clean 简化不掉的绕过形态；零第三方依赖（#46）
- feat(urlguard): 精确 host 白名单 WithExactHosts——WithAllowDomains 是后缀豁免，只能放宽某域及其全部子域，无法钉死到具体 host；而 urlguard 是放行型模型（默认只阻断私网/回环/保留段与约定域名），白名单外的任意公网域默认放行，于是「host 必须完全等于管理员配的那个 host」（webhook 出口签名绑 host、内部平台多一个子域就多一个未审计入口、凭据与端点绑定）在既有配置里根本没有对应表达；authority（host[:port]）必须完全等于列表中某项，与后缀豁免独立可叠加（域豁免不解除钉死），命中后仍走 scheme 白名单与私网判定（钉死 host 不等于放行私网）；host 大小写与尾点不敏感、端口按数值比较且参与比较、userinfo 不参与比较（url.Parse 已把 https://h@evil.com/ 的 userinfo 拆开，authority 是 evil.com——正是钉死 host 要挡的逃逸形态）；条目文法非法时 New 报错 fail-fast，未设置时行为与既有完全一致（#48）
- docs(stringx): 明确 TruncEllipsis 的 n 语义——n 是「截断到 n 个 rune」、不含省略号，因此返回值可达 n+1 个 rune；调用方若把 n 当「总长度预算」用（如提交状态描述的 140 字符上限）会超预算 1 个字符，此前文档未写明，现写进文档并补测试钉住该口径（#46）

## [v0.19.0] — 2026-10-21

- fix(httpjson): 指数退避改用乘法+饱和封顶，移位溢出不再产出负延迟——base << (attempt-1) 在 attempt 较大时 int64 溢出成负数，负数不满足 > maxDelay 于是绕过 MaxDelay 封顶，time.After(负数) 立刻返回，该轮退避彻底消失（默认 BaseDelay=200ms 时溢出点落在 attempt=37/39/40），封顶承诺同时被破坏；改用乘法+饱和封顶，先封顶再放大，MaxDelay 成为真正的上界，抖动叠加同样饱和，与同族 concurrency/retry 收敛到同一口径（#45）

- feat(errors): 补 Errorf——带格式化的构造原语，支持 %w 包装——pkg/errors 此前只有 msg 不可插值的 Wrap，消费方拼带动态信息的错误链只能先 Sprintf 再 Wrap（格式化动词与参数分离，易错）或绕回 stdlib fmt.Errorf（放弃 kv 上下文），本仓自身 fmt.Errorf 有 76 处；Errorf 与 fmt.Errorf 同型、支持 %w（可出现多个）、nil-safe（%w 实参为 nil 时不进包装链、消息按 %v 渲染为 "<nil>"，不产出 "%!w(<nil>)" 噪声）、格式化结果为空串时返回 nil、多个 %w 时包装链为 Unwrap() []error，消息渲染与 fmt.Errorf 逐字一致（差分测试以 stdlib 为参照实现对照）（#47）

## [v0.18.0] — 2026-10-20

- jsonrepair 补抽数组原语 ExtractArray，数组面兜底与对象面对称——多段数组与散文含 '{' 的数组文本不再整段作废（取首个口径与对象面一致）(#37)

## [v0.17.0] — 2026-10-20

### BREAKING: stringx.Masker 的短值改整段全量打码（#31）

- 原实现 `keep = min(Keep, n/2)` 的收缩逻辑，在 `n ≤ 2*Keep` 时让**头尾两段保留位
  首尾相接覆盖整段原文**——掩码主体 `****` 只是插在中间的装饰，原值被完整留在输出里
- 实测（Keep=4）：`Mask("01234567")` → `0123«probe:****»4567`（8 个字符原文全部可见）；
  `Mask("0123")` → `01«probe:****»23`（4 个字符原文全部可见）——**脱敏原语等于没生效**
- 修法：对齐同族 `guardx/logredact.MaskHint` 的 `HintMinLenRatio=4` 口径——
  命中长度 `n < Keep*4` 一律**整段全量打码**（不做头尾保留）；`n >= Keep*4` 才保留首尾各 Keep 个
- 判据是**运行期按实际命中长度**判定（不是声明比声明）：`n=4/7/8` 全掩、`n=16` 起保留
- 影响：依赖旧短值输出的消费方需同步——「留 8 藏 1 等于没藏」

### fix(pack): Inspect 的「条数不符」核对不再被静默跳过（#32）

- `counts` 的键口径此前未被强制；键与文件名对不上时整条检查被跳过且**无任何提示**，
  体检报「通过」但那条检查根本没跑
- 修法：先按文件名精确匹配 → 再按「去扩展名」基名宽松匹配 → 仍匹配不上则**显式报出**
  「条数未核对」与「清单 counts 的键 X 未匹配任何数据文件（该键的条数未核对）」
- 键对不上时不再出现假通过，也不再出现「条数不符」的假结论

### feat(pack): NewWriter 支持时钟注入（#33）

- `NewWriter(w io.Writer, formatVersion int, opts ...Option)` +
  `WithClock(now func() time.Time) Option`（对齐 `pkg/cache` 的 `WithClock` 口径）
- 清单的 `created_at` 时刻来源可注入 → 导出逻辑可做**确定性单测**；
  同一份数据用同一固定时钟导出两次，清单**逐字节相等**
- 向后兼容：两参写法 `NewWriter(w, 1)` 仍可用；`WithClock(nil)` 退化为当前时间

## [v0.16.0] — 2026-10-19

### feat(cli)：可单测的子命令分发原语（[#27](https://github.com/oekit/pkg/issues/27)）
- `App`/`Command` 骨架：退出码约定写死在包里（`ExitOK`=0 / `ExitError`=1 / `ExitUsage`=2），
  未知子命令与参数不合法均归 2；usage 由 `Command.Summary` 自动生成，不要求调用方手写
- `Run` 不读 `os.Args`、不调 `os.Exit`：参数与 stdout/stderr 全部由调用方给——命令逻辑因此
  可被单元测试直接调用（不必起子进程、不必解析 os.Args、不必捕获 stdout）
- `NewFlagSet` 把 `flag` 的错误输出重定向到调用方给的 stderr（默认 flag 打 os.Stderr，
  会破坏「输出流由调用方给」的约定）
- 契约测试：未知子命令→2 / 空 args→2 且打印 usage / 子命令返回码原样透传 /
  AST 源码断言（代码真调用 os.Exit·os.Args 才报，文档注释里的字样不误伤）

### feat(stringx)：正则脱敏 Masker——形态标签 + 保定位线索 + 不可复原（[#26](https://github.com/oekit/pkg/issues/26)）
- `NewMasker(rules []Rule) (*Masker, error)` + `Mask(text string) string`；
  `Rule{Name 形态标签, Re *regexp.Regexp, Keep 保留位数}`
- 掩码形如 `«name:****»` / `head«name:****»tail`：形态标签进掩码供审计读「打的是哪一类」
- 不可复原：保留位数封顶、不随原值长度变化（实际保留位 = min(Keep, n/2)），掩码主体恒为
  4 个 `*`；保留位按 rune 边界切（中文不切半个字）
- 单次扫描合并所有规则（起点最左 → 长度最长 → 注册顺序最前），替换结果不再参与匹配——
  形态标签不会被后一条规则二次处理
- 规则表完全由调用方提供，库不内置任何业务词表；`Name` 重复/为空、`Re` 为 nil 一律报错
  （形态标签重复会让审计结果有歧义）

### feat(cache)：LocalCache 可选形状——无后台 goroutine / 读命中返回副本 / LRU（[#25](https://github.com/oekit/pkg/issues/25)）
- `NoBackgroundSweep`：不起后台清扫 goroutine（嵌入式/短命命令行场景不该为缓存起常驻件），
  公开 `Sweep(ctx)` 手动清扫口；关闭后 `NewLocalCache` 之后进程内不新增 goroutine
- `CloneOnRead func(*T) *T`：读命中返回副本，调用方可随意改写返回值而不污染缓存条目
  （覆盖 Get / GetOrLoad 命中与 stale 兜底 / MGet；克隆在锁外执行）
- `EvictPolicy`：可选 `EvictLRU`（读命中也提升顺序），默认 `EvictFIFO` 行为与现状一致
- `GetEx(ctx, key) (*T, bool, error)`：区分「未命中」与「命中但值为 nil」；既有 `Get` 签名与行为不变
- 新增能力以可选接口 `Sweeper`/`HitGetter` 外置，不加入 `Cache[T]`——不破坏其它后端实现
- fix(cache)：顺序结构由「切片 + swap-remove」改为 `container/list`——原实现在删除队头时用
  末位元素填补空位，会打乱相对顺序（本就偏离文档声明的 FIFO 口径）；既有淘汰测试原样通过
- cachetest 契约套件新增 5 个条目（Sweep / 命中标志 / 读隔离 / 读提升 / 无后台 goroutine）

### feat(pack)：迁移包容器原语（[#24](https://github.com/oekit/pkg/issues/24)）
- zip 容器 + manifest 清单（格式版本 / 创建时间 / 各类条数 / 每文件 SHA-256）
- `AddJSONL(name, rows)` 逐行编码流式写出（不整包进内存）；`Close()` 写回 manifest 自身
- `Inspect` 体检**一次列全**问题（缺文件 / 摘要不符 / 条数不符 / 多余文件 / 非法文件名），
  而不是遇到第一个错误就停；哨兵错误 `ErrCorrupt` / `ErrUnsupportedVersion` 区分
  「包损坏」与「版本不认识」两类不可导原因，报错文案带实际与期望版本号
- 文件名净化防 zip 路径穿越（`..` / 绝对路径 / 路径分隔符一律拒绝）
- 摘要统一走 `encoding.Sha256Hex`（不各自实现）；`Open`/`ReadJSONL` 读回逐条相等

## [v0.15.1] — 2026-10-19

### fix(urlguard)：缺省阻断清单补齐保留段与 .localdomain（对齐 httpx#11 参考实现）
- 并入 IETF 协议指定段 192.0.0.0/24、TEST-NET 文档保留 192.0.2.0/24 / 198.51.100.0/24 /
  203.0.113.0/24、基准测试保留 198.18.0.0/15，域名后缀补 `.localdomain`——这些段本就不是
  合法出站目标，漏判属防护缺口（参考实现清单更全，收敛时补齐）
- 纯收紧型变更：原先放行这些段的配置不存在合理场景

## [v0.15.0] — 2026-10-19

### feat(urlguard)：出站 SSRF 防护原语（[httpx#11](https://github.com/oekit/httpx/issues/11)——webhook 接收器类服务的两层防护下沉）
- **层 1 `CheckURL(raw)`**：URL 字面量校验——scheme 白名单（缺省 http/https）、host 直接写
  私网/loopback/链路本地（含云元数据端点 169.254.169.254）/CGNAT（100.64/10）/ULA/组播保留、
  未指定地址、`.internal`/`.local`/`localhost`（含子域）一律拒绝；IPv4-mapped IPv6
  （::ffff:10.0.0.1）先归一化为 IPv4 再判定，防 mapped 绕过
- **层 2 `DialFunc(next)`**：拨号层包装——host 解析出的**每一个** IP 预检（任一命中即不建连），
  建连成功后核对**实际 RemoteAddr** 并在违规时关闭连接：预检解析与拨号器真实解析之间的
  重解析窗口（TOCTOU）被端点核对闭合，Happy-Eyeballs 双栈竞争的胜出路径同样被覆盖；
  与 httpx `WithDialFunc` 一行组合，next 为 nil 时用 net 默认拨号
- 构造态豁免（不做进程级全局开关，与矩阵其它原语注入风格一致）：
  `New(WithAllowPrivate())` 测试打本机 mock、`WithAllowCIDRs` 内网端点豁免、
  `WithAllowDomains` 域名豁免（点分边界，`example.com` 放行子域但不误放 `notexample.com`）、
  `WithAllowedSchemes` 覆盖 scheme 白名单；CIDR 文法非法在 New fail-fast
- 快捷入口 `IsBlockedURL` / `SafeDialFunc`（缺省 Guard）；Guard 只读可并发共享；
  仅标准库（零第三方依赖红线不受影响）
- 测试 12 例：阻断清单全表 / scheme / mapped 归一 / 豁免矩阵 / 预检拒绝不建连 /
  TOCTOU 端点核对关连接 / 合规直通 / WithAllowPrivate 真实 DNS / 并发 -race

## [v0.14.0] — 2026-10-18

### feat(cache)：动态回填 TTL + stale-if-error（[#11](https://github.com/oekit/pkg/issues/11)——data#4 语义随 data v0.9.0 重构静默回归的本账）
- `SourceFuncWithTTL[T]`（`(ctx, key) (*T, ttl, error)`）：回填 TTL 动态化——
  ttl > 0 按其缓存、<=0 回退配置 TTL；返回 nil 值仍走 null marker（ttl 不生效，
  NullTTL 治理）。典型场景：第三方 token 的 expires_in。`LocalConfig.SourceWithTTL`
  字段接入，与 Source 同时配置时优先
- `StaleIfError` + `OnStale`：回源失败且存在已过期历史真值时返回陈旧值
  （fail-open——防上游抖动放大为拒绝服务）；只认 TTL 过期的真值（null marker
  不参与 stale、TTL=0 永不过期条目无 stale 语义）；过期条目在兜底路径保留于表内
  （回收交给后台清扫），可反复兜底直至上游恢复；OnStale 回调供打点/告警
- 实现细节：GetOrLoad 首查改为**非删除式**查看（原惰性删除会让过期真值在回源
  前就消失、stale 无从兜底），回收职责让位给后台清扫；Get/Exists/MGet 读路径与
  惰性删除行为不变；缺省不开启时行为逐位不变

### feat(encoding)：Base64 字节↔字节兄弟方法（[#12](https://github.com/oekit/pkg/issues/12)）
- `Base64DecodeBytes(s) ([]byte, error)`：结果按字节使用（凭据还原/图片/PEM DER）
  从类型层面表达意图——`Base64Decode` 返回 string，字节场景经它中转既引入多余
  拷贝、又让 `[]byte`↔`string` 的误用零编译提示地下沉到运行期（ekit v0.34
  改签名时 new-api 单仓炸 5 处的教训）
- `Base64EncodeBytes(b) string`：字节→Base64 免一次 string 拷贝的对称入口；
  既有签名零改动

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