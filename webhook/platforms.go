package webhook

// 平台约定构造器（#270）：header 名与签名形态是各平台的历史约定，写死在
// 具名构造器里——「谁在用谁」的消歧知识单源在此，下游不再各自拼 header。
// 非主流平台用泛型原语（NewHMACValidator + options）自行组装。

import "crypto"

const (
	headerGitHubSignature  = "X-Hub-Signature-256" // GitHub（sha256=<hex>）
	headerForgejoSignature = "X-Forgejo-Signature" // Forgejo 新版
	headerGiteaSignature   = "X-Gitea-Signature"   // Gitea / Forgejo 老版本
	headerGitCodeSignature = "X-GitCode-Signature" // GitCode
	// headerGiteeSign Gitee 的验签载体 header——签名模式装 HMAC-base64、
	// 密码模式装密码明文（双模共用一个 header，靠 X-Gitee-Timestamp 在场
	// 与否消歧）。命名避开 "token" 字样（gosec G101 对含 token 的常量名
	// 报硬编码凭证误报，这里只是 header 名）。
	headerGiteeSign      = "X-Gitee-Token"
	headerGiteeTimestamp = "X-Gitee-Timestamp" // Gitee 签名模式附带
	headerGitCodeStatic  = "X-GitCode-Token"   // GitCode 静态令牌模式
)

// NewGitHubValidator GitHub 形态：X-Hub-Signature-256，值 sha256=<hex>
// （前缀大小写不敏感剥离）或裸 hex 双形态，HMAC-SHA256 常量时间比对。
func NewGitHubValidator(secret []byte) *HMACValidator {
	return NewHMACValidator(secret, crypto.SHA256, headerGitHubSignature,
		WithSignaturePrefix("sha256="))
}

// NewForgejoValidator Forgejo 形态：X-Forgejo-Signature，回退老分叉沿用的
// X-Gitea-Signature——同签名格式（裸 hex HMAC-SHA256），谁的 header 在场
// 裁谁。
func NewForgejoValidator(secret []byte) *HMACValidator {
	return NewHMACValidator(secret, crypto.SHA256, headerForgejoSignature,
		WithFallbackHeaders(headerGiteaSignature))
}

// NewGiteaValidator Gitea 形态：X-Gitea-Signature，裸 hex HMAC-SHA256。
func NewGiteaValidator(secret []byte) *HMACValidator {
	return NewHMACValidator(secret, crypto.SHA256, headerGiteaSignature)
}

// NewGitCodeValidator GitCode 形态：X-GitCode-Signature，回退老分叉沿用的
// X-Gitea-Signature（裸 hex HMAC-SHA256）。**签名与静态令牌双模并存**：
// 再回退 X-GitCode-Token 静态令牌比对——签名模式优先（Fallback 语义：
// 签名 header 在场即裁决，错签硬失败），令牌为空即双模退化为纯签名。
func NewGitCodeValidator(secret []byte, staticToken []byte) Validator {
	sig := NewHMACValidator(secret, crypto.SHA256, headerGitCodeSignature,
		WithFallbackHeaders(headerGiteaSignature))
	if len(staticToken) == 0 {
		return sig
	}
	return Fallback(sig, NewStaticTokenValidator(headerGitCodeStatic, staticToken))
}

// NewGiteeValidator Gitee 形态：签名模式（X-Gitee-Timestamp + X-Gitee-Token，
// base64(HMAC-SHA256(key=secret, message=timestamp+"\n"+secret))，容忍窗
// 缺省 1h 防重放）优先；**无时间戳时**按密码模式消歧（X-Gitee-Token 为
// 密码明文，常量时间比对）。
//
// AllowPasswordMode 显式 opt-in：密码为空即纯签名模式；密码非空才组合
// 密码候选——「默认拒绝密码模式」写死在构造期（密码模式只证明「知道
// 密码」，不绑定报文完整性，能不用就不用）。
func NewGiteeValidator(secret []byte, password []byte, opts ...TimestampOption) Validator {
	sign := NewTimestampHMACValidator(secret, crypto.SHA256,
		headerGiteeTimestamp, headerGiteeSign, opts...)
	if len(password) == 0 {
		return sign
	}
	return Fallback(sign, NewStaticTokenValidator(headerGiteeSign, password))
}

// 发出侧平台签名器（#325/#328）：下方每个构造器与上方同名 Validator 是
// 严格配对——同 secret 下 Sign 产出的 header 喂给对应 Validate 必过（配对
// 表测试 platforms_test.go 钉住）。跨服务对接按这张表取对：
//
//	发出侧                  收侧
//	NewGitHubStyleSigner ↔ NewGitHubValidator
//	NewForgejoSigner     ↔ NewForgejoValidator
//	NewGiteaSigner       ↔ NewGiteaValidator
//	NewGitCodeSigner     ↔ NewGitCodeValidator
//	NewGiteeSigner       ↔ NewGiteeValidator
//	NewHMACSigner + SignerWith* ↔ NewHMACValidator + With*
//	NewTimestampHMACSigner ↔ NewTimestampHMACValidator

// NewGitHubStyleSigner GitHub 形态发出侧：X-Hub-Signature-256，值
// sha256=<hex>——与 NewGitHubValidator 同款形态。
func NewGitHubStyleSigner(secret []byte) *HMACSigner {
	return NewHMACSigner(secret, crypto.SHA256, headerGitHubSignature,
		SignerWithSignaturePrefix("sha256="))
}

// NewForgejoSigner Forgejo 形态发出侧：X-Forgejo-Signature，裸 hex——与
// NewForgejoValidator 同款（收端自带 X-Gitea-Signature 回退，发端只写
// 主 header）。
func NewForgejoSigner(secret []byte) *HMACSigner {
	return NewHMACSigner(secret, crypto.SHA256, headerForgejoSignature)
}

// NewGiteaSigner Gitea 形态发出侧：X-Gitea-Signature，裸 hex——与
// NewGiteaValidator 同款。
func NewGiteaSigner(secret []byte) *HMACSigner {
	return NewHMACSigner(secret, crypto.SHA256, headerGiteaSignature)
}

// NewGitCodeSigner GitCode 形态发出侧：X-GitCode-Signature，裸 hex——与
// NewGitCodeValidator 的签名模式同款。静态令牌模式没有「生成」面（令牌
// 是明文常量，发出就是 req.Header.Set("X-GitCode-Token", token) 一行），
// 不设对应构造器。
func NewGitCodeSigner(secret []byte) *HMACSigner {
	return NewHMACSigner(secret, crypto.SHA256, headerGitCodeSignature)
}

// NewGiteeSigner Gitee 形态发出侧：X-Gitee-Timestamp（毫秒）+ X-Gitee-Token
// （base64(HMAC-SHA256(key=secret, message=timestamp+"\n"+secret))）——与
// NewGiteeValidator 的签名模式同款。opts 透传 SignerWithMessage /
// SignerWithClock / SignerWithURLEncoded（须与收端同配）。
func NewGiteeSigner(secret []byte, opts ...TimestampSignerOption) *TimestampHMACSigner {
	return NewTimestampHMACSigner(secret, crypto.SHA256,
		headerGiteeTimestamp, headerGiteeSign, opts...)
}
