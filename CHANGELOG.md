## [v0.37.6] — 2026-11-08

- fix(jsonrepair): Repair 连续尾逗号一次清空——此前 truncAt 被同段第二个逗号覆盖，`{"a":1,,}` 只删一个、单次输出仍是非法 JSON（`{"a":1,}`），要修两轮以上才收敛；现在连续逗号段合并/整段删除一次到位，值间连续逗号（`{"a":1,,,"b":2}`）同样合并；EOF 悬空逗号（`[1,2,` → `[1,2]`）与尾逗号同判（#262）
- fix(jsonrepair): Repair 非法转义不再丢反斜杠——`\x41` 曾被改成 `x41`（值被改写成另一个且输出合法 JSON 无人能发现）；现在反斜杠转义为 `\\` 忠实保留「反斜杠 + 后续字符」字面量，残缺 `\u` 形态（`\uZZZZ`）与其它非法转义统一为同口径；EOF 悬挂 `\` 同样保留（closePartial 截断现场的「丢悬挂反斜杠」口径不变并补文档区分两者）（#263）
- fix(jsonrepair): Repair 缺值分支补 null——`{"a":` / `{"a":}` / `{"a": ,}` 此前只补括号不补值，`{"a":}` 是非法 JSON 的稳定不动点（再修不变）；现在 `:` 后缺值（对象上下文）在 `,`/`}`/`]`/EOF 处补 null，LLM 在 `{"key":` 被 max_tokens 掐断的最常见中点可解析；不变量测试钉住「Repair 输出必须满足 json.Valid」（#268）
- docs(stringx): LastN godoc 修正——边界口径段仍按 FirstN 修复前（#183 之前）的行为描述「两者有意不同」，而 FirstN 的 n<0 现已全截断，注释解释的差异不存在了；改为「与 FirstN 同口径」，用例把 n<=0 两函数逐字一致钉成回归防线（#264）
- fix(cli): 表格列宽剥离 ANSI 转义序列再计宽——此前 `\x1b[32m` 的序列内字符被当可见字符（彩色 "ok" 被算 9 列），彩色单元格被判「已超宽」不补空格，后续列整体左移；displayWidth 先剥 CSI（`ESC [` … 0x40–0x7E）与 OSC（`ESC ]` … BEL/ST）再走原 rune 计宽，emoji/ZWJ 逻辑不动（#265）
- fix(urlguard): 非 ASCII host 判定前先做确定性折叠、折叠不了的一律拒绝（fail-closed）——此前判定面只覆盖 ASCII：`127。0。0。1`（U+3002）/`127．0．0．1`（U+FF0E）/`１２７.0.0.1`（全角数字）经 WHATWG URL 规范化都是 127.0.0.1，net.ParseIP 与 exotic 词法判定对它们全部失明 ⇒ 判定放行、客户端连回环；折叠覆盖 WHATWG 四域分隔符（。．｡→.）与全角数字/拉丁字母/连字符（NFKC 兼容映射），圈号等不可折叠形态直接拒；allowPrivate 语义不回归——折叠成标准点分十进制的形态与等价 ASCII 形态同判，exotic 字面量闸门照旧不受豁免（#266）
- fix(pathx): SanitizeFileName 处理 Windows 保留设备名——CON/PRN/AUX/NUL/COM1-9/LPT1-9 此前原样返回（上传原始文件名落盘时 ERROR_INVALID_NAME 或打开到设备句柄）；现在按主名（去扩展名）大写比对、命中加 "_" 前缀改形（con.txt → _con.txt，大小写不敏感），截断把主名削成保留名时（CONtract… 卡预算 → CON.txt）截断后再核一次并按同一上限收口（#267）

关联问题单：pkg#262、pkg#263、pkg#264、pkg#265、pkg#266、pkg#267、pkg#268。

## [v0.37.5] — 2026-11-08

- fix(convert): OptBool 数值/json.Number 分支与 AnyToBool 对齐——anyToBoolOK 此前经 AnyToInt64 中转，|v|<1 的浮点被截断成 0 ⇒ OptBool(0.5) 返回 &false 而 AnyToBool(0.5) 是 true，同一值两个答案且 nil/非 nil 叠加「字段不存在」的第二层降级（#205 的同源漏网）；判定逐字共用 AnyToFloat64 != 0，两入口对任何值不再分叉（#255）
- fix(convert): AnyToUint64 的 json.Number 分支复用 float64ToUint64Safe——此前直接 uint64(fv)，1e22 饱和成 uint64 最大值（`if id > 0` 之类校验全通过）而同值走 float64 分支是 0；encoding.Unmarshal 用 UseNumber，这条是主路径不是边角。有符号整数的按位重解释是文档明写的既有契约，不动（#256）
- fix(convert): BytesToInt64 / BytesToFloat32 / BytesToFloat64 判 err 返回 0——strconv 范围错误（ErrRange）同时返回钳制后的饱和值（"99999999999999999999" → MaxInt64、"1e999" → ±Inf），丢弃 err 就把饱和值当合法值，与文档「失败返回 0」及同文件 BytesToInt 相反；顺带关闭 AnyToInt64 的 []byte 与 string 两条入口对同一文本给出不同答案（#257）
- fix(container): Queue.TryTake 关闭即返回 false——文档写「已关闭返回零值与 false」，实现却无条件 popLocked：Put(1) 后 Close，TryTake 照样把最后一条出队，按「关闭即空」写收尾逻辑的调用方会重复处理最后一条或在 defer Close 后误投递；Close 文档同步写死两入口口径（Take 排空余量、TryTake 关闭即停）（#258）
- fix(stringx): SplitCSVNonEmpty 补 RFC 4180 引号处理——此前是裸 strings.Split：`"a,b",c` 被静默切成 `"a`/`b"`/`c`（含逗号的地址、JSON 片段、SQL 条件拆成两段且不报错），引号原样保留（`"x"` → `"x"`）。引号字段内逗号不切分、`""` 是字面引号、引号剥离、引号内空白不 trim；未闭合引号与闭合后残余宽松兜底不丢字；与 stdlib encoding/csv 交叉验证用例钉住（#259）
- fix(encoding): UnmarshalFromReader 不再把整个输入攒进内存——此前 TeeReader 把每个流过的字节复制进 strings.Builder，成功路径也照攒（8 MiB 报文 ~50 MiB 堆）；错误摘录只需开头 256 字节，改用带上限的 excerptWriter 只驻留前 256 字节，错误信息契约（有界、带输入开头）不变（#260）
- fix(timex): Date2Stamp 在 DST 前跳日不再把日期归一到前一天——Santiago 2026-09-06 00:00:00 这个墙钟时刻不存在，ParseInLocation 归一到 09-05 23:00（时间戳偏移一整天且不报错），GetCurrentDayStartTime 的日切窗口随之整体偏移；对齐 boundary.go dayStart 的既有口径：解析结果墙钟日期与输入不一致时锚定目标日首个存在瞬间并补回时刻偏移（缺口内 00:30 顺延到 01:30，日期不丢）；layout 含时区元素时跳过修正（偏移在串里，无本地歧义）（#261）

关联问题单：pkg#255、pkg#256、pkg#257、pkg#258、pkg#259、pkg#260、pkg#261。

## [v0.37.4] — 2026-11-08

- fix(httpjson): NormalizeBaseURL scheme 前缀判定大小写不敏感（EqualFold，RFC 3986 §3.1）+ 命中后 scheme 归一小写——`HTTPS://EXAMPLE.COM` 不再被二次补 `http://` 产出 `http://HTTPS://EXAMPLE.COM`（scheme 静默降级 https→http、host 截成 `HTTPS:`）；只认 http/https 两个 scheme，`host:port` 等其余形态仍按无 scheme 补 `http://`（#239）
- fix(httpjson): NormalizeBaseURL 尾斜杠裁剪收窄到 authority+path 段——先剥 scheme 前缀再裁（scheme 的 `//` 永不动：`http://`、`http:///` 保持 `http://`，不再截成 `http:`），首个 `?`/`#` 之后的 query/fragment 原样保留（`http://host/?x=/` 的 query 值 `/` 不再被整串 TrimRight 静默吃成空串）；刻意不走 url.Parse 重组以保「小写输入零变化」（#240）
- fix(jsonrepair): StripCodeFence 弃整串正则改逐行状态机——非贪婪 `(.*?)` 把围栏内容里的 ``` 当结束标记，嵌套围栏（JSON 字符串带代码块、4 反引号外栏包 3 反引号内栏）被腰斩致下游 Unmarshal 报「无法解析」而数据其实完整；闭栏按 markdown 规则要求行首、反引号个数 ≥ 开栏，「多块取第一块」契约不变，只有开栏没有闭栏时返回开栏后全部（#241）
- refactor(stringx): Contains 委托 slicex.Contains[string]——删掉与泛型版逐字同构的第二份循环体（两份副本改口径时只改一处、另一处静默保持旧行为）；签名不变调用面零改动，doc 注明语义单源与保留理由，用例逐项钉住两函数一致（#242）
- feat(shellx): Split 分词器——Quote/QuoteArgv 的逆运算（`Split(QuoteArgv(args...))` 逐项还原 args，往返判据用例钉住）：单引号原样（含反斜杠）、双引号内反斜杠仅转义 `" \ $ \`` 与换行（`\x` 非 escape 字面保留）、未加引号的 `; | &` 独立 token 且相邻同类合并 `&&`/`||`、未闭合引号（ErrUnclosedQuote）与尾随转义（ErrTrailingEscape）报错不做尽力解析、空参数保留；只拆词不展开（变量/命令替换/glob 不碰——与 Quote「只转义、不判策略」同一立场）（#243）

关联问题单：pkg#239、pkg#240、pkg#241、pkg#242、pkg#243。

## [v0.37.3] — 2026-11-08

- chore: 发版链修复重发——v0.37.2 tag 先于 CHANGELOG 落地，触发 tag 检出的「CHANGELOG/tag 一致性」门禁红（protect-release-tags 规则下 tag 不可移动，红 run 不可恢复）；本 tag 为同内容的干净发版，代码与 v0.37.2 完全一致（#244 的 merge commit）

## [v0.37.2] — 2026-11-08

- fix(urlguard): WithAllowPrivate 不再跳过 exotic IPv4 字面量闸门——豁免语义收敛为只豁免网段（CIDR）判定，十进制/八进制/hex 整型字面量（2852039166 = 169.254.169.254）无条件拒，allowPrivate 消费方经云 metadata 的绕过路径关闭；拨号层 resolve 同口径（#236）
- fix(errors): MultiError.Append 的 copy-on-write 路径先 slices.Clone 再 append——backing array 有余量时不再写进共享数组，并发 Append 同一实例的潜在竞态关闭（#237）
- feat(cache): LocalCache 新增 MGetEx——GetEx 的批量形态，BatchEntry{Value, Hit} 让批量读也能区分「nil 真值命中」与「缺席」（MGet 的 map 形态二者不可区分）；走「可选能力接口 + 类型断言」哲学（BatchHitGetter），MGet 既有签名不变（#238）

关联问题单：pkg#236、pkg#237、pkg#238。

## [v0.37.1] — 2026-11-07

- fix(mathx): Clamp 对 lo>hi fail-fast panic——静默产生区间外结果关闭，配置上下限写反即刻暴露（#228）
- fix(mathx): RandIntn 对 maxInt<=0 显式 panic——替代 math/rand 裸 panic，消息带上下文（#229）

关联问题单：pkg#228、pkg#229。

## [v0.37.0] — 2026-11-07

- feat(timex): ParseFirst 多 layout 容错解析原语——first-match 语义，同一时间字段多平台格式不一的通用形态（#197）
- feat(bufiox): TailLines 行粒度尾部窗口读取原语——1MB 单行上限、budget 软上限、truncated 诚实信号、CRLF 归一（#198）
- feat(container): Slice.RemoveIf 原子谓词删除——AppendIfAbsent 的删除侧对偶（#199）
- fix(cache): SingleFlightCache 写后失效与在途回源竞态——per-key 世代号：Del 后在途加载不再把旧值/null marker 回填（#200）
- feat(ctxscope): 新子包——命名空间+值类型双隔离的泛型 Key/With/From/MustFrom，请求级作用域传递不再手搓 WithValue（#201）
- fix(errors): BizError 包装链字符串形态自洽——嵌套只贡献 message、String()≡Error()、WithStatus 带 cause（#202）
- fix(cache): SingleFlightCache「命中但值为 nil」双重计 hit 关闭——hit 即早退（#203）
- docs(cache): (nil,nil) 契约补全「显式存入的 nil 真值」第三种情形（#209）
- docs(pathx): MatchSimpleGlob 补「* 跨 /」语义声明——与 MatchGlob 同、与 filepath.Match 反（#210）
- feat(pagination): WalkEach 流式行走器——短页停止/yield false 终止/budget 预算哨兵；与 #226 的 Walk（收集全量）分工：流式 vs 全量（#211）
- feat(secretx): NewPassphraseBox 派生前置形态——随机 salt + PBKDF2-HMAC-SHA256 600k 迭代（stdlib crypto/pbkdf2，零依赖红线）+ 版本位 v2 跨形态拒绝（#212）
- feat(cache): SingleValue[T] 单值 TTL 缓存——单值形态不再自造常量键，无真值返回 ErrNoValue（#213）
- fix(timex/cron): NewRandomDelaySchedule/NewDelaySchedule 构造期校验——min>max/负值 fail-fast panic，不再静默退化固定延迟/过去时刻（#227）

关联问题单：pkg#197、pkg#198、pkg#199、pkg#200、pkg#201、pkg#202、pkg#203、pkg#209、pkg#210、pkg#211、pkg#212、pkg#213、pkg#227。

## [v0.36.0] — 2026-11-07

- feat(secretx): 内存态敏感凭证持有原语 `SecureString`/`SecureBytes`——XOR 混淆存储防内存明文扫描 + Set/Clear 双向零化 + Get 每次独立副本解出（外部改写不穿透）+ `EqualConstantTime` 走 crypto/subtle（时序侧信道）；随机密钥生成失败降级 hex 自混淆而非 panic（持有器不因环境退化拒绝服务）。设计立场写进源码：**定位是内存扫描缓解而非加密**，能读进程内存的攻击者只多一步还原，静态落库走本包 Box（AES-GCM 信封），两套原语不互相冒充。收编自 zentao-mini/zentao-max 逐字节同源的 secure_string.go（上游 zentao-mini#22 下沉，下游各删 -183 行），原实现零用例，本版补齐 roundtrip/Set 替换/Clear 复用/常量时间比较/副本隔离/并发 race 七面
- feat(pagination): `Walk` 翻页聚合单源——固定 pageSize 从第 1 页逐页 fetch，**短页即末页**（len < pageSize 含 0 即停），按页序拼接；逐页 ctx 取消检查；任何一页失败立即返回带页码上下文的错误，已聚合部分不返回（要么全量要么错误，没有半截结果）。`MaxWalkPages`（10000）防呆把「上游越界页恒返整页」这类契约违例从死循环变 `ErrTooManyPages` 明确报错；`ErrBadPageSize`/`ErrNilFetcher` 构造期哨兵。此前 zentao-mini 的 client_* 五个「翻页直到 len<pageSize」循环各写一份（页长 100/200/500 各异），本轮下游收编
- feat(httpjson): `NormalizeBaseURL`——SDK base 配置规范化（补 `http://` scheme——不猜 https，内网自建占多数；结尾斜杠全去；首尾空白去除；空串原样返回不吞「未配置」语义）。字符级规则不解析 URL，畸形输入走同样规则。收编 zentao-mini/max 四份同源内联规范化（上游 zentao-mini#22 下沉）

关联上游：上游 zentao-mini#22（SecureString/URL 规范化/翻页 helper 下沉本仓，common 已归档由本仓接档）、上游 zentao-mini#20（翻页循环泛型化落地依赖）。

## [v0.35.2] — 2026-11-07

- fix(timex/cron): Next 在夏令时回拨/前跳日退化成过去时刻或内部死循环（[#214](https://github.com/oekit/pkg/issues/214)）——根因是每次推进都「绝对时间 Add 后按墙钟重建」，而 time.Date 对**重复**墙钟取较早一次、对**不存在**墙钟归一到前一天 23:00：回拨日 01:59 EDT +1min 重建回 01:00 EDT（倒退 59 分钟）⇒ Next 返回**早于入参**的时刻，调度器 `next := Next(prev) → sleep → prev = next` 立即热转且永不前进；小时字段受限时更糟——从 02:00 EDT 推 03:00，绝对时间先落 01:00 EST、重建回 01:00 EDT ⇒ **Next 内部死循环**，调用方连返回都拿不到。前跳日同理：time.Date 与 AddDate 同落 09-05 23:00 ⇒ 日期循环原地打转、永不返回（#214 原文只说「补用例」，实测出第二个独立缺陷）。改法：推进一律走绝对时间（nextHour/nextMinute 天然单调，现代时区偏移都是整分钟 ⇒ 绝对整点必落墙钟整分），日期加减改以**正午**为锚点、落到目标日最早存在的时刻，外层再加最多 3 次单调性兜底；语义写入 doc.go——回拨日两次出现都会各自被推进到、表达式要求的墙钟时刻当天不存在则顺延到次年命中
- fix(timex/cron): dow 字段的 7≡Sunday 在列表/范围/步长等**所有形态**下生效（[#215](https://github.com/oekit/pkg/issues/215)、[#206](https://github.com/oekit/pkg/issues/206)）——此前只有整串恰为 "7" 时归一，`6,7` / `5-7` / `7/2` 仍按越界报错；而 vixie cron / crontab(5) / robfig 都接受 7=周日 ⇒ 从系统 crontab 迁移 `0 2 * * 6,7` 加载即失败且不提示方言差异。修法：归一**下沉到 token 落位层**——范围按端点原值遍历、落位时 7→0，故 `5-7` 天然是环形 {5,6,0} 而不撞 start>end；文本层 ReplaceAll 会把 `5-7` 改写成 `5-0`（撞非法区间），只在 getField 层做数值归一同样撞 start>end，两条死路都已反向验证。顺带修正注释：0=Sunday 编号下 `5-7` 是**周五到周日**（issue 原文写「周六-周日」是错的）
- fix(convert): AnyToBool 数值分支改经 float64 判定（[#205](https://github.com/oekit/pkg/issues/205)）——此前走 `AnyToInt64(val) != 0`，`|v|<1` 的浮点被截断成 0 ⇒ `0.5` 与 `json.Number("0.5")` 被判为 false；浮点只在自身为 0 时才舍入到 0，走 float64 不引入新错误。顺带修正同源失真：`uint64 > MaxInt64` 经旧路径按 AnyToInt64 自身契约返回 0 ⇒ 修复前也是 false；按 AnyToBool 自己的文档（非零为 true）这才是对的
- fix(convert): ConvertStruct 对非法入参报明确错误，不再静默 no-op（[#207](https://github.com/oekit/pkg/issues/207)）——九种形态（dst 为 nil 接口值 / nil typed pointer / 非指针 / 指针非结构体，src 为 nil 接口值 / nil typed pointer / 非结构体 / 指针非结构体）此前全部静默返回 nil，调用方以为复制成功。字段层面仍**尽力而为**：src 独有、dst 独有、类型不一致、不可导出字段一律跳过不报错——这条边界写进 godoc，并用成对正例钉住，避免后续把契约收得过紧破坏既有调用方（旧用例 `TestConvertStruct_NilDst` 曾把这个缺陷固化成预期，已改为断言必须报错）
- fix(bufiox): Release 兑现 e 参数语义，残留归位、旧视图失效、大缓冲不再滞留（[#216](https://github.com/oekit/pkg/issues/216)）——此前 `Release(e)` 忽略 e 一律整段丢弃：socket 已读出的字节**无法重读**，丢弃即丢数据（残留往往是同一 TCP 段里下一条报文的开头）。现在 e!=nil 整段丢弃；e==nil **保留未消费残留并把游标归零**（原地左移、零分配；cap 超过残留 4 倍时换右尺寸数组，收缩大缓冲）。`BytesReader` 的 e==nil 故意不同——回到**起点**：固定帧的「重置」自然含义是重新解析同一帧，可续读的流不能这么定义。接口文档改为「共同部分 + e==nil 时未消费数据的去留由实现定义」，并把「Release 后不得使用旧视图」明确为**契约要求而非运行时保证**（Go 里被释放的切片不会变野指针）
- fix(urlguard): DialFunc 的 host 判定改走 checkHostLiteral，与 CheckHost 同源（[#204](https://github.com/oekit/pkg/issues/204)）——拨号层此前只查 allowPrivate/IpRanges，漏掉 CheckHost 的四条内置规则（约定域后缀 / exotic IPv4 / allowDomains 优先 / allowPrivate 短路）⇒ **写面拒、拨号面放**，域名解析一变（DNS rebinding）即穿透。改法是**复用而非重写**：两份规则各写一遍必然漂移，漂移形态正是这个样子。新增用例用固定 lookup 解析出公网 IP，把域名规则与 IP 范围判定隔离开，并逐条比对 CheckHost 与拨号层结论（含 `allowDomains("svc.local")` 不得放行 `other.local`）
- fix(mathx): ProbabilityElems 对权重跨度加动态范围判据，溢出不再静默失真（[#208](https://github.com/oekit/pkg/issues/208)）——桶边界按 `total * p^k` 指数展开，跨度大时连指数本身都溢出：`p = ceil(log10(precise/minW))`，`precise=1, minW=1e-320` 时 `1/1e-320` 已是 +Inf ⇒ `int(+Inf)` 实现相关（arm64 给 MaxInt64、amd64 给 MinInt64）⇒ **两平台走进完全不同的分支**（该缺陷由 CI 的 amd64 抓出，本机 arm64 不区分）。不溢出的区间同样失真：`[1e-30,1e-30,1e3]` 第二个桶被高估约 17 个数量级，而 `band[0] > 0` 使既有退化检查拦不住。修法：指数改 `ceil(log10(precise) - log10(minW))`（数学等价、不产生溢出中间量）+ precise 有限性纳入校验 + 末端 `total*p` 动态范围 panic（用 `!(x <= max)` 而非 `x > max`，让 NaN 也触发）。选 panic 而非钳制——与既有输入校验（0/负权重、precise<1）同口径，概率路由静默出错比快速失败危险得多

关联问题单：pkg#214、pkg#215、pkg#206、pkg#205、pkg#207、pkg#216、pkg#204、pkg#208。

## [v0.35.1] — 2026-11-06

- fix(mathx): `Round`/`Floor` 补 n>308 溢出防护——返回原值而非 NaN/Inf（[#182](https://github.com/oekit/pkg/issues/182)）——`math.Pow10(400)`=+Inf ⇒ `x*Inf`=±Inf（x=0 时 NaN）⇒ `toFixed` 内 ParseFloat("NaN") 竟成功 ⇒ **静默返回 NaN**，指标/金额进库不报错；同文件 `DivisionN` 有同款 IsInf 防护而这两函数没有（同包两套哲学）。附：`ToFixed(x,-1)` 的负精度 panic 在 doc 声明（行为不变）
- fix(stringx): `FirstN` n<0 对齐 Trunc 家族「非法上限=全截断」口径（[#183](https://github.com/oekit/pkg/issues/183)）——此前循环永不命中截断点 ⇒ 静默返回整串（截断护栏整段失效）；自家 `LastN` 的 doc 已白纸黑字指认「那是实现怪癖不是语义」——属于修复而非行为变更
- fix(stringx): `Union` 改保序实现（先 first 后 second 扫重）（[#187](https://github.com/oekit/pkg/issues/187)）——此前从 map 迭代构造结果，每次调用顺序不同（Go map 遍历随机化）；同库 `slicex.Unique` 明确「保持原始顺序」——两族口径相反。合并结果进 UI 列表/审计日志/签名串/缓存 key 时顺序抖动 = diff 噪声、快照比对 flaky。成本一致（O(n+m)+map 查重）
- fix(timex/cron): 方言差异收口——dow 支持 7≡Sunday、无解表达式/亚秒钐制 doc 声明（[#184](https://github.com/oekit/pkg/issues/184)、[#185](https://github.com/oekit/pkg/issues/185)、[#186](https://github.com/oekit/pkg/issues/186)）——doc.go 宣称「标准 5 字段」但 dow=7 判越界（vixie cron/crontab(5)/robfig 均接受）、MON/JAN 名字不支持：从系统 crontab 迁移 `0 2 * * 7` 加载即报错且不提示方言差异。修法：dow=7 归一为 0 + 报错信息点名方言差异；`Every` 的三件套语义（<1s 钝 1s/亚秒截断/秒对齐）与 `SpecSchedule.Next` 的无解零值返回 doc 声明（调度器消费 Next 前应判 IsZero）
- fix(cache): `Key` doc 补碰撞警示与「段内不得含 ':'」契约（[#188](https://github.com/oekit/pkg/issues/188)）——同文件 HashKey 用整篇幅幅记录过 #146 同款事故并上了长度前缀编码，但更常用的 Key 入口零警示：`Key("a","b:c")==Key("a:b","c")` ——段值来自用户输入含冒号时静默串缓存
- fix(httpjson): 2xx 空 body 且 out!=nil 时跳过解码（[#189](https://github.com/oekit/pkg/issues/189)）——#158（out=nil 侧）的对偶面：DELETE/POST 端点返回 204、调用方习惯性传 `&resp` 时 `json.Unmarshal([]byte{}, out)` 必报「响应解析失败」——**把成功误报为失败**，重试即重复施加副作用；204 语义上就没有 body

关联问题单：pkg#182、pkg#183、pkg#184、pkg#185、pkg#186、pkg#187、pkg#188、pkg#189。

## [v0.35.0] — 2026-11-06

- feat(pathx): 新增 MatchSimpleGlob 单星名字通配原语——只有第一颗 * 是通配符（第二颗起按字面量）、空串判不匹配、? 非通配，与 full-backtracking 的 MatchGlob 是反语义对（来源：kuboard-go simpleGlobMatch 已验证实现上移，2,000,000 例差分 fuzz 0 差异）。feat(stringx): 新增 MaskJSONFields JSON 字段级脱敏原语——顶层对象字段值替换为固定文案，字段缺省时补上，解析失败透传 json 包原始错误不包装（消费方透传错误文本到响应）

## [v0.34.1] — 2026-11-05

- fix(jsonrepair): `StripCodeFence` 剥掉任意语言标记（[#180](https://github.com/oekit/pkg/issues/180)）——正则从 `(?:json)?` 白名单改为 `[a-zA-Z0-9_+-]*` 字符集，\`\`\`yaml/\`\`\`toml/\`\`\`sql 等围栏的语言名不再残留进结果污染下游解析（下游实证 oagentkit/textx#21）；大小写变体（\`\`\`JSON）一并覆盖，\`\`\`json{...}\`\`\` 同行形态语义不变

关联问题单：pkg#180。

## [v0.34.0] — 2026-11-05

- feat(secretx): 新增静态敏感凭证的信封加密原语（[#177](https://github.com/oekit/pkg/issues/177)）——domain-manage/zeus/user/octopus/fuxi_flow 四仓各自手搓 AES 已漂移出四个形态（fuxi 的 AES-CFB 无认证密文可篡改不报错、octopus 缺钥明文透传入库、三家无版本前缀真要轮换时密文无法分发），本包收成一份正确默认：`ParseKey`（base64 + 16/24/32B 校验，错误带实际长度上下文）+ `NewBox` 构造期 fail-fast（缺钥 `ErrNoKey`、长度非法 `ErrBadKeySize`——**不提供缺钥明文透传降级**，本地开发请自行用恒等实现绕过）+ `Box.Seal/Open`（`"v1:" + base64(nonce‖ct)`，AES-GCM 必带认证、crypto/rand 随机 nonce、与 domain-manage 手搓形态**逐字节兼容——存量数据零迁移**）+ `SealWithAAD/OpenWithAAD`（绑定行 id/用途，防密文在行间挪用）+ `ErrUnsupportedVersion` 版本前缀识别位（v2 主密钥落地后下游按前缀选钥重封的缝线）+ `ErrBadCiphertext` 统一表述（密钥不匹配与密文损坏不可区分，不向日志泄露「哪条密文用哪把钥封」）。零第三方依赖（crypto/* 标准库），Box 不可变可并发
- feat(container): `Slice.AppendIfAbsent` 原子「查重 + 追加」（[#178](https://github.com/oekit/pkg/issues/178)）——有序注册表场景（先注册先命中 + 查重 fail-fast，如 host 别名表、协议分发表）的 GetOrSet 对应物：先 Range/Snapshot 查重再 Append 是两步，重复注册的 fail-fast 窗口里会插进两个同名条目；`eq` 为 nil 按 == 比较（interface 相等，元素动态类型不可比较时 panic，与 map 访问不可比较键同一运行时契约），非 nil 时自定义等价关系（大小写归一等）。查重与追加在同一把写锁内完成，-race 下并发注册同 key 恰好一个成功

关联问题单：pkg#177、pkg#178。

## [v0.33.0] — 2026-11-03

- feat(cache): 选择性 stale——回源后重捕获兜底资格，Source 内 Del 即取消 + ErrNoStale 哨兵（#172）
- ⚠️ breaking feat(stringx): Filter 语义对齐 slicex.Filter（保留命中），新增 KeepFunc/DeleteFunc 迁移位（#173）

关联问题单：pkg#172、pkg#173。

## [v0.32.0] — 2026-11-03

- stringx.LastN 保尾截断原语（#167，与 FirstN 镜像：保尾、省略号拼头，边界口径对齐 TruncRunes）；cache.NXSetter 可选能力接口 + LocalCache.SetNX 原子 NX 写入（#168，同 Redis SET NX：过期条目与 null marker 按不存在、失败零副作用不刷 TTL、ttl<=0 不过期、写路径不计 Stats）；SingleFlightCache.SetNX 透明转发（内层无能力 fail-loud：ErrNXUnsupported）；cachetest 补 RunSetNXWithClock 硬失败契约套件（六情形，直连与装饰两形态）

## [v0.31.2] — 2026-11-02

- fix(cli): Table 按序列算宽度——emoji 状态符号不再错位（#162）

## [v0.31.1] — 2026-11-02

- fix(httpjson): `DoJSON` 对 `out==nil` 跳过响应体解码（#158）——2xx 成功请求不再被误报失败

此前 `DoJSON` 无条件 `json.Unmarshal(raw, out)`，out 为 nil 时恒定拿到
`json: Unmarshal(nil)` 的 InvalidUnmarshalError ⇒ **服务端已返回 2xx 的成功请求
被误报为失败**，真失败反而淹没在假失败里。下游实证：pigeonbox/core 的 webhook
推送渠道自 2026-10 起一直以 out=nil 调用 ⇒ 每次成功送达都记一条
"webhook push failed" 假失败日志。

out 为 nil 是 fire-and-forget 调用方的语义：只关心请求送达与状态码，不关心
响应体 ⇒ 2xx 即返回 nil。非 2xx 的判定与 out 无关：照样返回 StatusError（错误体
摘要照旧带上）。`DoJSONWithRetry` 经 `DoJSON` 透传，同款语义。out 非 nil 时
行为不变：解码失败仍返回「响应解析失败」错误。

守卫位置刻意放在状态码检查**之后**——挪到之前会让非 2xx 连错误一起放过 ⇒ 真失败
被静默。补 5 组用例钉住（含表驱动矩阵覆盖 out 两态），并做变异验证：删守卫 /
守卫挪位 / 守卫条件取反，三种变异均被新断言抓住转红（#158）

## [v0.31.0] — 2026-10-29

- feat(cli): 子命令别名 + 默认子命令 + 内建 help/version —— 三件不必每个消费方各写一遍的事。此前每个用本包的程序都要自己写一段 workaround 才能支持 `--help`（因为本包没有内建帮助，未知子命令被报成用法错误，于是每个消费方都得在分发前先拦一次 `--help` 自己转交），而那段 workaround 在每个消费方里长得都不一样：**问帮助返回的退出码各不一致**（有的返回 2「用法错误」——问帮助不是错误）、帮助输出走 stdout 还是 stderr 各不一致、别名（同一个动作的旧叫法，如 `--doctor`）只能靠消费方手写 if 拦、无参启动的程序（以 stdio 提供服务、没人给它传子命令名的那种）只能自己写 `if len(args)==0 { os.Exit(serve()) }`。现在：`Command.Aliases` 认别名（**名字优先于别名**，所以宿主自己声明了名为 `help` 的子命令时它照常工作）、`App.Default` 让无参等于执行默认子命令、`App.Version` 非空时提供 `--version`/`version`、内建 `--help`/`-h`/`help`（**打印用法到 stdout、退出码 0**）。默认行为不变：`Default` 为空则无参仍是用法错误，`Version` 为空则 `version` 仍是未知子命令 ⇒ 既有消费方不受影响
- feat(cli): `Table` —— 列宽自适应的文本表格。此前 CLI 的表格输出是最容易各写各的形状：列宽写死一份、换一份就歪、中文列按字节算宽度比实际宽出一倍（`len()` 数的是字节，一个中文字符 3 字节 ⇒ 按它算列宽会让中文列参差不齐）。`Table` 按显示列宽算（中日韩全角算两列、组合记号不算列），行数不定、列宽自适应
- feat(cli): `Report` —— 自检报告（doctor 式清单）的渲染原语。doctor 这类输出几乎每个程序都要写一遍，而每个程序都写得不太一样：状态标记三种宽度、检查项名称列宽写死一份、修复建议单独缩进一行但缩进量和名称列对不上（⇒ 修复建议看不出挂在哪一项下面）。`Report` 统一形状：状态标记定宽、检查项名称列自适应、修复建议缩进到名称列起点、结论行排在结果行之前。**它只渲染不判断**——哪些项算失败由调用方通过 `Failures()`/`Warnings()` 自己看，退出码也由调用方定（报告给人看，退出码给脚本看，两者口径不必相同）

## [v0.30.0] — 2026-10-28

- fix(cache): `HashKey` 的参数边界歧义 ⇒ 不同参数组算出同一个缓存 key ⇒ 串数据——此前每个参数用 `%v:` 直接拼进 SHA-256 的摘要输入，参数值里的 `:` 与「参数边界」不可区分：`HashKey("p","a","b") == HashKey("p","a:b")`、`HashKey("p","a:b","c") == HashKey("p","a","b:c")`、`HashKey("p",1) == HashKey("p","1")` 三组全部同键（第三条是 int 与 string 互撞）。故障形态最坏：静默（没有报错、没有 panic，只是命中率异常地高）、越权面（租户名/用户 ID/关键词/路径进参数时就是跨租户串数据）、难复现（只有参数含 `:` 才触发，而测试数据通常不带）。改为**长度前缀的无歧义编码**（类型名长度:类型名 + 值长度:值）⇒ 参数边界与值内容不再混淆，类型不同的参数组（1 与 "1"）也不同键。key 为进程/缓存生命周期内临时值、无持久化契约 ⇒ 换编码不构成破坏性变更；但**存量 key 的取值会变**（升级后旧缓存条目不再命中，属预期内的失效）（#146）
- fix(singleflight): `pkg/internal/singleflight` 的清理改用 `CompareAndDelete`——此前是无条件 `Delete`，`Forget` 之后注册的同 key **新** call 会被一并删掉 ⇒ 第三个调用方另起一次 fn，「同 key 只执行一次 fn」的承诺被打破；`concurrency/singleflight` 早已修同一处缺陷，两套语义不同的实现却在跑**同一套契约套件** ⇒ 给出「都通过了」的假保证。保留 pkg 侧原有的「先摘键、后唤醒」顺序（x/sync/singleflight 同款；反过来会留一个「陈旧结果被当成新鲜结果返回」的窗口）（#147）
- feat(singleflighttest): 契约套件补「Forget-竞态」用例（`Forget` → 新 call 仍在飞行 → 旧 call 收尾 → 第三个调用方）——旧套件只断言「Forget 后单次新 Do 会重跑 fn」，**不构造**旧 call 与新 call 交叠的时序 ⇒ 该缺陷在套件里不可观测 ⇒ 两个语义已漂移的实现都能通过；补上后 `pkg/internal` 与 `concurrency/singleflight` 两侧同时被钉住（#147）
- feat(convert): 补 nil 安全的可选转换入口——`AnyToStr(nil)` 返回字符串 `"<nil>"`，它不是空串、不是零值、不是 error，而是一个**长得像值**的哨兵：过得了任何 schema 校验（就是个 string）、被当成「这个字段有内容」送到模型面前 ⇒ 模型拿到一个**不存在的名字**当作真实值；「没有这个字段」与「这个字段的值是 `<nil>` 字符串」在下游必须分得开，但用 `AnyToStr` 之后分不开。新增（与既有 `Any*` 并列，**不改既有语义**）：`AnyToStrOr(v, def)`（nil ⇒ def，默认 `""` ⇒ nil 读成空串而不是 `"<nil>"`）、`OptStr`/`OptBool`/`OptInt32`/`OptInt64`（**没有就是没有**：缺失或无法表示为目标类型时返回 nil 指针；不返回 `&false`/`&0` 那种「长得像值」的伪装）、`AnyToInt32`（补上 int32 这一档：JSON 解码出的数字一律是 float64，进 int32 契约前必须转；装不下按「无法转换」返回 0，**不发生回绕**）（#143）

## [v0.29.1] — 2026-10-28

- fix(container): `Slice.Delete`/`Clear` 缩容时把底层数组内容清零——此前只改 len，被删元素仍留在底层数组 ⇒ GC 收不走（大对象滞留）
- fix(cli): `NewFlagSet(name, nil)` 的输出改写 `io.Discard`——此前不调 `SetOutput` 时标准库默认写 `os.Stderr` ⇒ 错误信息与用法逃出调用方给的输出流
- fix(contextx): `ValueOnlyFrom(nil)` 立即 panic（与标准库 `context.WithValue` 的 nil parent 口径一致）——此前 nil 延后到某次 `Value` 调用才炸，堆栈离「传错了」很远
- fix(enumx): `NewRegistry` 遇重复 `Value()` 立即 panic——此前后者静默覆盖前者（连 labels 一起覆盖）⇒ 取到哪个枚举、看到哪个标签全看传入顺序
- fix(pack): 摘要清单的「条数」改为非空行数，与写入侧（每条一行）和读取侧（ReadJSONL 跳过空行）同一口径——此前数物理行数，含空行的包被误报「条数不符」，而读出来的条数恰好等于清单里的数

## [v0.29.0] — 2026-10-28

- fix(urlguard): IPv6 zone 后缀击穿全部 IP 字面量判定（SSRF 绕过）——`http://[::1%25lo]/`、`[fe80::1%25eth0]/`、`[fc00::1%25eth0]/`、`[::ffff:127.0.0.1%25eth0]/` 此前**全部放行**（无 zone 的 `[::1]` 正确阻断）：`net.ParseIP` 不认 zone，而 `url.Parse` 后的 `u.Hostname()` 保留它 ⇒ IP 字面量判定整段跳过。zone 只决定走哪块网卡、不改变连的是哪个地址 ⇒ 判定按地址部分做；顺带覆盖 UDP/IP 连接的端点复核（此前只认 `*net.TCPAddr`）与两个漏掉的 IPv6 保留段（`::/96`、`fec0::/10`）
- fix(errors): `Errorf("%w", 空报文错误)` 返回 **nil** —— 被包装错误连同整条链一起消失，调用方的 `if err != nil` 分支整个跳过，**失败被静默吞成成功**；同时修 typed-nil `*BizError`（`Error()` 空指针崩溃、`AsBizError` 报 `(nil, true)`）与 `MultiError.Unwrap()` 泄漏内部切片（绕过 Append 的 copy-on-write 保护）
- fix(cache): 6 处——① `StaleIfError` 被缺省后台清扫打断（过期真值一并被删，兜底只在「[过期, 下次清扫)」窗口内有效，与文档承诺相反）；② `Checker` 一次 refresh panic 即**永久卡死**（inflight 未清理 + done 未 close，后续 Check 全挂在 `<-call.done`）；③ 回填路径把缓存条目指针直接交给调用方，绕过 `CloneOnRead`；④ 一次 stale 读取同时计命中与未命中，命中率虚高；⑤ `cacheLog` 在包初始化时求值 ⇒ `slog.SetDefault` 汇流不到缓存日志；⑥ `TTL=0` 且 `NullTTL>0` 时过期的 null marker **永不回收**（无界增长）
- fix(timex): 夏令时窗口内的边界不变量不成立——回拨日 `EndOfHour` 落在输入**之前**（区间连 t 都不含）、午夜不存在的时区（America/Santiago）`BeginningOfDay` 落到**前一天**；另修 cron 月份扫描跳过目标月（`0 0 0 1 2 *` 从 1/31 求值下次触发晚一年）、`a-b/n` 区间带步长被拒、`NextSchedule(n<=0)` panic
- fix(mathx): 概率路由选错元素——`ProbabilityElems` 内部排序后丢弃了置换关系，返回的 band 对应排序后顺序而调用方拿它索引自己的 `elems`（权重被静默置换）；`Probability` 的 `r <= v` 差一让**最后一个桶永远选不到**（等权时分布 2/3、1/3、0）；`McNemarExact` 大 n 返回 **NaN**（组合数溢出）、`WilsonCI` 越界计数返回 `(NaN,NaN)`、`DivisionN` 经 int64 中转**回绕**、`toFixed` 吞掉 ParseFloat 错误返回 0、`NewUnstable(NaN)` 产生垃圾抖动值
- fix(strmap): 重载**就地改写**底层缓冲 ⇒ 之前返回的键/值字符串内容被改写（破坏 Go 字符串不可变保证）；改为每次重载重新分配缓冲，旧视图继续指向旧缓冲
- fix(stringx): `ToCamelCase` 遇非法 UTF-8 **panic**（`utf8.RuneLen(U+FFFD)`=3 而非法字节实际只占 1 字节 ⇒ 切片越界）；`Pluralize(count, "")` 丢掉计数返回字面量 "0"
- **BREAKING**: fix(httpjson): `DoRaw` 签名变更 `(int, []byte, error)` → `(RawResponse, error)`——新增 `RawResponse.Truncated`，此前报文被静默截断（文档既写「不截断报文」又在下一段写了 limit，自相矛盾）且调用方**无法区分**半截报文与完整报文；`RawResponse` 同时承载 StatusCode/Body。org 内零消费方（v0.28.1 才发布）
- fix(httpjson): 响应体未排空 ⇒ 连接无法被 transport 复用（报文超 limit 时每次调用都要重建 TCP + TLS 握手）
- fix(jsonrepair): 截断数组的元素串被误判成键位 ⇒ 闭合出 `["a","b":null]` 这种非法 JSON ⇒ 宽松解析整体失败；判据从「引号前那个字节」改成「开口时所处的括号上下文」
- fix(bufiox): `ReadBinary` 直读路径（缺口 ≥4KiB）**漏记 ReadLen**；`rdErr` 只写不读 ⇒ 文档承诺的「粘滞终态错误」根本没实现（缺陷实现每次操作都重读底层）；`acquire` 倍增溢出成负数 ⇒ 极大长度请求**无限挂起**
- fix(container): `SafeMap.Range` 遍历期间一直持着读锁 ⇒ 回调里写同一个 map **死锁**（RWMutex 不可重入），且文档承诺的「遍历期间其他 goroutine 的写入不阻塞」并不成立；改为先取快照再锁外遍历（与 `Slice.Range` 同构）
- fix(pagination): 偏移量溢出保护只兜住一半情形——`int` 溢出回绕后的结果有一半是**正数**（`page=1<<62+2` 返回垃圾偏移量 500），而 `page` 来自 query/form 参数（外部可控）⇒ 改为**乘法前**判（`PageRequest` 与 `Policy` 两处同修）
- fix(pathx): 截断时留下**半个扩展名**（`"中.txt"` + 上限 5 字节 → `"中.t"`）——正是本函数文档明令禁止的形态（扩展名骗人，下游按它判类型会判错）；改为丢弃扩展名、只留截断后的主名
- fix(pack): 迁移包体检三处缺口——① zip 内**同名条目并存**不被报出 ⇒ 清单只校验了第一份、第二份不被任何检查覆盖（按 zip 条目遍历的消费方读到的是未校验的那份）；② 数据条目**没有解压上限**（zip bomb：几 KB 的包能让体检无限期烧 CPU）；③ `ReadJSONL` 单行无上限（`bufio.ReadBytes` 为超长行无限增长）
- fix(singleflighttest): 契约套件的等待**无界**（缺陷实现把 CI 挂到 go test 的 10 分钟超时，而不是几秒内干脆失败）；补上「飞行结束后新调用必须重新执行 fn」这条可观测不变量
- fix(container): `Set` 的零值口径与同包其它容器不一致——`Set` 本身是 map 类型、零值即 nil map，`Add` 直接 panic，而同包的 Map / SafeMap / Slice 都写明「零值不可直接使用」⇒ 使用者按同包口径推断「零值可用」就踩坑；补文档并加用例钉住
- fix(slicex): `Chunk` 的分片与入参**共享底层数组**（与 stdlib `slices.Chunk` 同语义），此前文档没说清；同包的 `Reverse` 明确「返回新切片，不修改原始切片」⇒ 两个 API 口径不一致。补文档并加用例钉住两种语义

## [v0.28.1] — 2026-10-28

- feat(httpjson): 加 DoRaw——返回原始状态码与原文报文（不判 2xx、不截断），供要按状态码映射自己语义、并把原文报文带进错误文案的调用方

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

- fix(cache): SingleFlightCache 包装 LocalCache 时静默废掉 StaleIfError（#72）——前置检查走 Get（LocalCache 的惰性删除路径）把 stale 兜底需要的过期真值提前删掉；新增 LocalCache.Peek 非删除式读（可选接口 PeekGetter 外置，不破坏其它后端实现）+ SingleFlightCache 前置检查改走 Peek；cachetest 补 RunStaleIfErrorWithClock / RunStaleIfErrorRepeatedWithClock 两条契约（钉住「装饰器不得改变内层语义」）

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

- 原实现 `keep = min(Keep, n/2)` 的收缩逻辑（#31），在 `n ≤ 2*Keep` 时让**头尾两段保留位
  首尾相接覆盖整段原文**——掩码主体 `****` 只是插在中间的装饰，原值被完整留在输出里
- 实测（Keep=4）：`Mask("01234567")` → `0123«probe:****»4567`（8 个字符原文全部可见）；
  `Mask("0123")` → `01«probe:****»23`（4 个字符原文全部可见）——**脱敏原语等于没生效**
- 修法：对齐同族 `guardx/logredact.MaskHint` 的 `HintMinLenRatio=4` 口径——
  命中长度 `n < Keep*4` 一律**整段全量打码**（不做头尾保留）；`n >= Keep*4` 才保留首尾各 Keep 个
- 判据是**运行期按实际命中长度**判定（不是声明比声明）：`n=4/7/8` 全掩、`n=16` 起保留
- 影响：依赖旧短值输出的消费方需同步——「留 8 藏 1 等于没藏」

### fix(pack): Inspect 的「条数不符」核对不再被静默跳过（#32）

- `counts` 的键口径此前未被强制（#32）；键与文件名对不上时整条检查被跳过且**无任何提示**，
  体检报「通过」但那条检查根本没跑
- 修法：先按文件名精确匹配 → 再按「去扩展名」基名宽松匹配 → 仍匹配不上则**显式报出**
  「条数未核对」与「清单 counts 的键 X 未匹配任何数据文件（该键的条数未核对）」
- 键对不上时不再出现假通过，也不再出现「条数不符」的假结论

### feat(pack): NewWriter 支持时钟注入（#33）

- `NewWriter(w io.Writer, formatVersion int, opts ...Option)`（#33）+
  `WithClock(now func() time.Time) Option`（对齐 `pkg/cache` 的 `WithClock` 口径）
- 清单的 `created_at` 时刻来源可注入 → 导出逻辑可做**确定性单测**；
  同一份数据用同一固定时钟导出两次，清单**逐字节相等**
- 向后兼容：两参写法 `NewWriter(w, 1)` 仍可用；`WithClock(nil)` 退化为当前时间

## [v0.16.0] — 2026-10-19

### feat(cli)：可单测的子命令分发原语（[#27](https://github.com/oekit/pkg/issues/27)）
- `App`/`Command` 骨架（#27）：退出码约定写死在包里（`ExitOK`=0 / `ExitError`=1 / `ExitUsage`=2），
  未知子命令与参数不合法均归 2；usage 由 `Command.Summary` 自动生成，不要求调用方手写
- `Run` 不读 `os.Args`、不调 `os.Exit`：参数与 stdout/stderr 全部由调用方给——命令逻辑因此
  可被单元测试直接调用（不必起子进程、不必解析 os.Args、不必捕获 stdout）
- `NewFlagSet` 把 `flag` 的错误输出重定向到调用方给的 stderr（默认 flag 打 os.Stderr，
  会破坏「输出流由调用方给」的约定）
- 契约测试：未知子命令→2 / 空 args→2 且打印 usage / 子命令返回码原样透传 /
  AST 源码断言（代码真调用 os.Exit·os.Args 才报，文档注释里的字样不误伤）

### feat(stringx)：正则脱敏 Masker——形态标签 + 保定位线索 + 不可复原（[#26](https://github.com/oekit/pkg/issues/26)）
- `NewMasker(rules []Rule) (*Masker, error)` + `Mask(text string) string`（#26）；
  `Rule{Name 形态标签, Re *regexp.Regexp, Keep 保留位数}`
- 掩码形如 `«name:****»` / `head«name:****»tail`：形态标签进掩码供审计读「打的是哪一类」
- 不可复原：保留位数封顶、不随原值长度变化（实际保留位 = min(Keep, n/2)），掩码主体恒为
  4 个 `*`；保留位按 rune 边界切（中文不切半个字）
- 单次扫描合并所有规则（起点最左 → 长度最长 → 注册顺序最前），替换结果不再参与匹配——
  形态标签不会被后一条规则二次处理
- 规则表完全由调用方提供，库不内置任何业务词表；`Name` 重复/为空、`Re` 为 nil 一律报错
  （形态标签重复会让审计结果有歧义）

### feat(cache)：LocalCache 可选形状——无后台 goroutine / 读命中返回副本 / LRU（[#25](https://github.com/oekit/pkg/issues/25)）
- `NoBackgroundSweep`（#25）：不起后台清扫 goroutine（嵌入式/短命命令行场景不该为缓存起常驻件），
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
- zip 容器 + manifest 清单（格式版本 / 创建时间 / 各类条数 / 每文件 SHA-256）（#24）
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