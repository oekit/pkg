// Package licensx 提供 Ed25519 license 签发/验签机制内核：JSON 载荷 + Ed25519
// 签名（覆盖除 signature 外全部字段）+ 到期/吊销判定 + 类型化哨兵 + 公钥注入。
// 纯 stdlib（crypto/ed25519 + encoding/json）。载荷结构是宿主产品决策——
// customer/features/实例数等业务字段不入本包，内核收任意 JSON 载荷（固定序
// 序列化钩子：宿主用 json.Marshal 宿主 struct 产出，禁 map）。
//
// 包名循 pathx/stringx/secretx 的 x 后缀惯例（#486）；另一层原因：子包目录
// 名 license 会与 macOS/Windows 大小写不敏感文件系统上的仓根 LICENSE 文件
// 冲突，无法共存检出。
//
// # 密钥保管边界
//
// 签发私钥只在运营方（签发侧）手里，永不入仓、不进配置中心、不下发到验签侧。
// 本包不持有任何密钥常量：验签公钥由宿主注入（编译进二进制是防篡改惯例——
// 改环境变量、换文件、改配置都拿不到授权能力；「公钥怎么交付」是宿主策略，
// 包只提供注入点）。轮换：Keygen 生成新密钥对，私钥落仓外路径，公钥随宿主
// 重新编译/分发——旧 license 将全部失效（签名对不上），轮换窗口要与客户约定。
//
// # fail-loud
//
// 验签失败一律返回类型化哨兵错误（ErrSignatureInvalid / ErrExpired /
// ErrRevoked），消费方一律 errors.Is 判定，不要匹配错误文本。本包不做任何
// 静默降级：「降级不静默」的机制语义由 Load 兑现——非有效形态必带 Reason
// 可观测；「无效 license 拿不到授权能力」由宿主把 Status.Valid 映射到自己的
// 档位来兑现（community/enterprise 分支与能力词表是产品决策，留在宿主）。
//
// # 典型用法
//
//	// 签发侧（运营方工具，私钥不出仓）：
//	pub, priv, err := licensx.Keygen()
//	payload, err := json.Marshal(hostClaims{Customer: "acme", Features: []string{"sso"}})
//	lic := &licensx.License{
//	    Serial:   "2026-001",
//	    IssuedAt: time.Now().UTC(),
//	    Payload:  payload, // 固定序序列化：json.Marshal 宿主 struct
//	}
//	err = licensx.Sign(lic, priv)
//
//	// 验签侧（产品二进制，公钥编译进宿主）：
//	st := licensx.Load("/etc/app/license.json", compiledPub, time.Now())
//	if !st.Valid {
//	    // community 档位运行，st.Reason 透出原因（不静默）
//	}
//	var claims hostClaims
//	err = json.Unmarshal(st.Payload, &claims) // 载荷按宿主结构解码
package licensx
