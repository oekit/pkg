// Package webhook webhook 签约原语族：「收」（验签）与「发」（签名）两侧
// 的字段约定单源。
//
// # 收侧（验签原语族，#270）
//
// 验证器接口 + HMAC 家族（sha256/384/512 多算法、hex/base64 多编码、可选
// 前缀、header 回退链）+ 时间戳防重放 HMAC + 静态令牌 + 回退组合子 +
// per-platform 注册表。收编自 go-git-platform provider/webhook.go 的五个
// 验签器形态（GitHub sha256=<hex> 双形态 / Forgejo·Gitea 双 header 回退 /
// GitCode 签名+静态令牌双模 / Gitee 时间戳+密钥 HMAC-base64 含时间漂移
// 防重放 / 任意算法任意 header 的泛型形态），下游可整文件删除私有实现。
//
// 服务端把 body 读成 []byte 后传进来（框架各异，读体不是原语的职责）：
//
//	v := webhook.NewGitHubValidator(secret)
//	if err := v.Validate(r, body); err != nil { ... }
//
// 实现纪律（每条都是现场教训，逐条搬进源码）：
//
//  1. **常量时间比较**：全部签名/令牌比对走 crypto/hmac.Equal——普通
//     bytes.Equal 有时序侧信道，攻击者可逐字节爆破签名；
//  2. **空密钥 fail-closed**：构造期空 secret 直接 panic（「空密钥使
//     HMAC 验证对可预测载荷形同虚设」）——与 StaticToken 同口径，绝无
//     「没配密钥就静默放行」的降级路径；
//  3. **签名解码双形态**：GitHub 带 sha256= 前缀、部分平台裸 hex/base64
//     ——剥离可配前缀（大小写不敏感）后统一解码；
//  4. **双 header 回退**：分叉平台的 header 约定漂移（Forgejo 老版本用
//     Gitea 的 header）——header 链按序消歧，「谁在用谁」的约定写进
//     平台构造器；
//  5. **多模消歧的优先序**：签名模式优先于密码模式（Fallback 组合子的
//     语义保证：签名 header 在场即裁决、错签硬失败不降级；仅当签名
//     header 缺席才落到密码/静态令牌模式）——防同 secret 交叉认证。
//
// 收侧错误语义（errors.Is 判定）：
//
//   - ErrMissingSignature：验签 header 整体缺席——Fallback 据此落下一
//     个候选模式，调用方一般视作「非本平台/未配签名」拒绝；
//   - ErrBadSignature：签名在场但不匹配（含解码失败）——硬失败；
//   - ErrStaleTimestamp：时间戳缺席/非法/超出容忍窗——硬失败（防重放）；
//   - ErrUnknownPlatform：注册表查无此平台。
//
// # 发侧（签名原语族，#325/#328）
//
// 验签族的对称面：任何往外发 webhook 的服务（事件转发器、平台对端）与
// 每个测 webhook 接收的测试夹具，都不必再手算 HMAC——签名 message 形态
// （hmacSum）、前缀/编码（encodeSignature，与收侧 decodeSignature 镜像）、
// 平台 header 名与收侧共用同一份实现，「发」与「收」的字段约定没有第二源，
// 跨服务对接时两侧字段对不上的事故面从源头消除。
//
// HMACSigner 是 HMACValidator 的逐参数对偶（同 secret 同选项下互逆）；
// TimestampHMACSigner 是 TimestampHMACValidator 的对偶（发出时打时间戳）；
// SignHMAC / SignTimestampHMAC 是两条纯函数 helper（测试夹具一行替代手写
// hmac 块）；NewEventID + EventEnvelope 给出幂等事件 ID 与标准事件信封的
// 推荐形态。边界哲学与收侧一致：Sign 只产出可直接套上出站请求的 header 集、
// 信封只产出 payload 字节，HTTP 发送归调用方——纯函数零状态，不读体不发送。
//
// 发出侧 ↔ 收侧配对表（同 secret 下 Sign 喂 Validate 必过，配对测试钉住）：
//
//	发出侧                            收侧
//	NewGitHubStyleSigner              NewGitHubValidator
//	NewForgejoSigner                  NewForgejoValidator
//	NewGiteaSigner                    NewGiteaValidator
//	NewGitCodeSigner                  NewGitCodeValidator
//	NewGiteeSigner                    NewGiteeValidator
//	SignHMAC / NewHMACSigner          NewHMACValidator（同组选项同配）
//	SignTimestampHMAC / NewTimestampHMACSigner  NewTimestampHMACValidator
//
// 注意事项（与收侧纪律成对）：secret 空串同样 panic fail-closed（无密钥
// 签名等于裸奔）；前缀/编码/message 组装在两侧必须**同配**（发侧带
// sha256= 前缀、收侧没配剥前缀 = 无故拒绝，配对表与同组选项就是为了把
// 这类联调事故挡在构造期）；SignerWith* 是发侧选项前缀，与收侧 With*
// 一一对应成对。
//
// # 转发面（游标式 at-least-once 批量转发器，#482）
//
// 审计/事件外发到 SIEM 的「取批 → 全 sink 投递 → 推进游标」不变量单源：
// Forwarder 机制只收不变量，游标存储（CursorStore，宿主的设置表/持久
// KV）与批源（BatchSource，如 WHERE seq > ? ORDER BY seq LIMIT ?）由宿主
// 注入，本包不假设任何存储形态；内置 MemoryCursorStore（契约测试与骨架
// 用）与两个 sink——WebhookSink（整批一个 POST，body 为载荷 JSON 数组，
// 可选 HMAC 签名，复用发侧 Signer——签的就是发出的 body 字节）与
// SyslogSink（RFC5424 一事件一帧，逐轮连接断链下轮重试）。
//
// at-least-once 语义（宁重不丢）：游标只在**全部 sink 成功且游标落库
// 成功**后推进到本批最大 Seq；任一失败游标不动、下轮重取同窗重发**整批**
// ——重投的最小单位是一整批（sink A 成功 B 失败的那轮，A 下轮收到重复
// 事件是有意为之）；先投递后记账的顺序保证「丢」只可能表现为「重」。
// 接收端必须幂等：按载荷内稳定事件 ID（推荐 EventEnvelope.ID / 载荷自有
// id，或签名头的 DefaultDeliveryHeader 投递 ID）在解包/落库之前去重；
// 重投只重复不乱序，不得依赖「恰好一次」。毒批（非法 payload/坏批序）
// fail-loud 卡住游标——跳过=有意丢数据，是宿主的决策不是转发器的。
//
// RFC5424 首版承诺：PRI + 版本 1 + 时间戳（UTC 毫秒）+ 主机 + APP-NAME +
// MSG，STRUCTURED-DATA 恒 NILVALUE；分帧用非透明式（行=一帧，MSG 内换行
// 净化为空格），octet-counting/TLS 不在首版。
//
// # IM 机器人载荷构造器（#485）
//
// 企业微信/钉钉/飞书 IM 机器人的出向 JSON 载荷单源：msgtype/markdown/
// text 三家的字段约定与 header 名同属「平台历史约定」，写死在具名构造器
// （WecomMarkdownPayload/DingtalkMarkdownPayload/FeishuTextPayload）里，
// BuildIMPayload 按渠道名分发替代宿主各自的策略表。边界哲学与签名器
// 一致：纯函数零网络——发送、鉴权（钉钉 URL 签名/飞书 payload 签名随
// 宿主发送面走）、超长截断都不在本包（不做有损截断：静默截断把「没发
// 全」伪装成「发完了」）。钉钉 markdown 的 title 是宿主内容（构造器收参
// 不写死）；变体范围收 issue 原样三种，卡片深水区按需再进。
//
// 本包做「签」「验」「转发」「IM 载荷构造」四面的字段约定单源；不读请求
// 体/不判 IP 白名单/不解析事件类型/不做重试退避——时间戳防重放窗的判定
// 在收端（发出端只按约定打时间戳与幂等 ID，消费端按 DefaultDeliveryHeader
// 去重），转发面的轮询节奏与告警归宿主。
package webhook
