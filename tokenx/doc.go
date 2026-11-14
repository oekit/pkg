// Package tokenx 提供「签名能力令牌」内核：HMAC 载荷绑定 + 命令集规范化
// 哈希。「控制面签发 → 工具体执行」的双进程架构（签发端与校验端可以是
// 两个独立部署的二进制）共用同一份令牌契约：短命、载荷绑定、常数时间
// 比对、fail-closed。stdlib-only 是立包理由——校验端二进制不因共享令牌
// 契约引入任何平台依赖（pkg 零第三方依赖红线）。
//
// # 与 middleware JWTAuth 的分工
//
// 本包是**轻量自持令牌**：两段式 `payload.mac`、无 JWT 头/算法协商面/
// claims 体系/签发方元数据。适用面是「同一控制平面内的点对点授权」——
// 签发端与校验端共享一个 secret、载荷字段由两端代码共同约定（载荷绑定
// 校验就是这段约定的执行点）。需要标准 SSO、多租户、算法协商、过期
// 刷新等生态能力时用 JWT（middleware.JWTAuth），两者不互相替代。
//
// # 令牌形态与时效/绑定边界
//
// 令牌 = `base64url(payload) + "." + base64url(HMAC-SHA256(secret, payload
// 的 base64 文本))`，两段均 Raw URL 字母表（无填充）；MAC 罩住的是载荷
// 的 base64 **文本**，载荷一个字节都改不动。
//
// 载荷是 JSON 的 [Claims]（键值对）：map 键序由 encoding/json 字典序
// 钉死——同样的 claims 无论构造顺序，签出**逐字节相同**的令牌；`exp`
// 是保留键（unix 秒），由 [Sign] 写入，有效期含端点（校验时刻
// `now > exp` 才判过期，`now == exp` 仍有效）。时效边界只管「这条令牌
// 还活着」；令牌无撤销机制（短命 + 一次性消费语义由宿主的进程内 nonce
// 消费表承担），长期有效的授权面请走策略面（pkg/canon 的版本指纹 +
// 宿主吊销）而不是拉长 TTL。
//
// [Verify] 三段判定，次序钉死：
//
//  1. **常数时间 MAC**（crypto/hmac Equal）——非本控制面签发的令牌
//     在这里死，不泄露任何载荷信息；
//  2. **载荷绑定**——校验端要求「这条令牌是为本次执行而签的」：bind
//     中每个键在载荷中存在且值相等（approval_id/case_id/cmd_hash/host
//     等由两端共同约定），挪用他人的令牌在这里死；
//  3. **时效**——活着的令牌才谈得上新鲜，过期在这里死。
//
// fail-closed：secret 为空时 [Sign] 与 [Verify] 一律拒绝（ErrNoSecret），
// 不存在「无钥降级放行」的形态。
//
// # 命令集规范化哈希
//
// [CanonicalCmdHash] 是 approve-what-you-execute 的锚点：审批人看到的
// 命令集与工具体实际执行的命令集必须是同一串字节。规范化规则钉死：
//
//   - **键序（命令序）敏感**：命令是**序列**不是集合——"a\nb" 与
//     "b\na" 哈希不同（与策略面的集合语义相反，那边用 pkg/canon 的
//     SortDedup 归一，这边保留顺序）；
//   - **空白规则**：逐词 TrimSpace（首尾空白不进哈希），词内空白原样
//     保留；不合并重复命令、不丢空词——落进哈希的就是所写的序列；
//   - 词以 "\n" 连接后取 sha256 十六进制（全宽 64 字符）。
//
// 同一命令集在任何一台机器、任何一次签发上哈希一致；签发端与校验端
// 各算一遍并经载荷绑定比对，命令集在两进程之间被改写即失败。
//
// # 用法
//
//	// 签发端（控制面）：审批通过后
//	tok, err := tokenx.Sign(secret, tokenx.Claims{
//		"approval_id": "ap-42",
//		"case_id":     "cs-7",
//		"host":        "worker-1",
//		"cmd_hash":    tokenx.CanonicalCmdHash([]string{"kubectl get pods", " systemctl restart api"}),
//	}, time.Now().Add(5*time.Minute))
//
//	// 校验端（工具体，可为独立部署二进制）：
//	claims, err := tokenx.Verify(secret, tok, tokenx.Claims{
//		"approval_id": "ap-42", // 本次执行期待被绑定的键值
//		"host":        "worker-1",
//		"cmd_hash":    tokenx.CanonicalCmdHash(cmdsFromLocalPlan),
//	})
//	// err 为 nil 才执行；ErrBinding/ErrExpired/ErrBadMAC 各形态由 errors.Is 判定
package tokenx
