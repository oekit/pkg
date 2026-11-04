// Package secretx 提供静态敏感凭证的信封加密原语（AES-GCM + 密文版本前缀 +
// 主密钥解析）：把一段敏感字符串（云账号 SecretKey、LDAP bind 密码、渠道
// token、git provider token 等）用主密钥封起来存库——此前每个服务各自手搓
// AES，密钥/算法语义漂移出四个形态（CFB 无认证、缺钥明文透传、密文无版本位），
// 本包把它们收成一份正确默认（#177）。
//
// 密文格式（与 domain-manage 手搓形态兼容，存量数据零迁移）：
//
//	"v1:" + base64( nonce(12B) ‖ ciphertext+tag )
//
// 设计立场：
//   - **必须带认证**（GCM），CFB 这类无完整性保护的原语不在可选项里；
//   - **缺钥 fail-fast**：构造期校验，不提供「缺钥明文透传」降级——那是
//     octopus 手搓版的真实事故形态（漏到生产即裸奔）；本地开发想绕过请
//     自行用恒等实现，不该由库默认承担；
//   - **密文版本前缀**是主密钥轮换的识别位：Open 遇到未识别前缀返回哨兵
//     ErrUnsupportedVersion，下游可按前缀选钥解密重封（v2 落地时零破坏）；
//   - **错误语义统一**：解密失败（密钥不匹配 vs 密文损坏）一律 ErrBadCiphertext
//     ——不向日志/调用方泄露「哪条密文是用哪把钥封的」这类信息。
//
// 典型用法：
//
//	key, err := secretx.ParseKey(os.Getenv("APP_MASTER_KEY")) // base64(16/24/32B)
//	if err != nil { panic(err) }                             // 构造期 fail-fast
//	box, err := secretx.NewBox(key)
//	if err != nil { panic(err) }
//
//	sealed, err := box.Seal([]byte(plaintext))     // "v1:xxxx" 直接入库
//	pt, err := box.Open(sealed)                    // 读出解封
//
//	// 可选：绑定行 id/用途做 AAD，防密文在行间挪用（换行 AAD 解封必失败）
//	sealed, err := box.SealWithAAD(pt, []byte("credential:"+rowID))
//	pt, err := box.OpenWithAAD(sealed, []byte("credential:"+rowID))
package secretx
