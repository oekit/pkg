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
	headerGiteeToken       = "X-Gitee-Token"       // Gitee 签名/密码双模共用
	headerGiteeTimestamp   = "X-Gitee-Timestamp"   // Gitee 签名模式附带
	headerGitCodeStaticTok = "X-GitCode-Token"     // GitCode 静态令牌模式
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
	return Fallback(sig, NewStaticTokenValidator(headerGitCodeStaticTok, staticToken))
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
		headerGiteeTimestamp, headerGiteeToken, opts...)
	if len(password) == 0 {
		return sign
	}
	return Fallback(sign, NewStaticTokenValidator(headerGiteeToken, password))
}
