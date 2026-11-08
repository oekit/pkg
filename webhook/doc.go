// Package webhook server 侧 webhook 验签原语族：验证器接口 + HMAC 家族
// （sha256/384/512 多算法、hex/base64 多编码、可选前缀、header 回退链）
// + 时间戳防重放 HMAC + 静态令牌 + 回退组合子 + per-platform 注册表
// （#270）。收编自 go-git-platform provider/webhook.go 的五个验签器形态
// （GitHub sha256=<hex> 双形态 / Forgejo·Gitea 双 header 回退 / GitCode
// 签名+静态令牌双模 / Gitee 时间戳+密钥 HMAC-base64 含时间漂移防重放 /
// 任意算法任意 header 的泛型形态），下游可整文件删除私有实现。
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
// 错误语义（errors.Is 判定）：
//
//   - ErrMissingSignature：验签 header 整体缺席——Fallback 据此落下一
//     个候选模式，调用方一般视作「非本平台/未配签名」拒绝；
//   - ErrBadSignature：签名在场但不匹配（含解码失败）——硬失败；
//   - ErrStaleTimestamp：时间戳缺席/非法/超出容忍窗——硬失败（防重放）；
//   - ErrUnknownPlatform：注册表查无此平台。
//
// 本包只做「验」，不读请求体、不判 IP 白名单、不解析事件类型——服务端
// 已把 body 读成 []byte 后传进来（框架各异，读体不是原语的职责）。
package webhook
