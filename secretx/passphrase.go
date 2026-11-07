package secretx

// passphrase 派生形态（#212）：消费方拿到的通常是 passphrase（环境变量
// ENCRYPTION_KEY、配置文件口令）而非原始主密钥——从 passphrase 到主密钥
// 这一步若无原语，消费方只能手搓 KDF，而手搓史（下游 v1：单轮 SHA-256、
// 无 salt、无 KDF 强度、无版本位——弱口令可离线暴力，算法升级时存量密文
// 无法识别代际）正是教科书里每样不该写的。本文件把这一步收成正确默认。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

// ErrNoPassphrase passphrase 缺失（空串）。与 ErrNoKey 同 fail-fast 口径：
// 「环境变量没配就落库明文」不存在合法形态。
var ErrNoPassphrase = errors.New("secretx: passphrase missing")

const (
	// passphraseSaltSize 每密文随机 salt 长度（16B，NIST 建议下限）。
	passphraseSaltSize = 16

	// passphraseKeySize 派生主密钥长度（32B ⇒ AES-256——passphrase 的
	// 熵本就低于随机钥，不再降位）。
	passphraseKeySize = 32

	// gcmTagSize GCM 认证标签长度（负载长度预检用，避免先派生再发现
	// 密文过短——KDF 是本形态的主要开销）。
	gcmTagSize = 16
)

// PassphraseKDFIterations 是 passphrase 派生的 PBKDF2-HMAC-SHA256 迭代数
// （OWASP 2023 对该组合的建议值 600,000）。
//
// KDF 选型说明（与 issue 建议的 argon2id 之别）：本仓是**零第三方依赖仓**
// （go.mod 零 require，CI 强制——x/crypto 进不来），argon2id 无标准库实现；
// PBKDF2-HMAC-SHA256 是 Go 1.24 标准库（crypto/pbkdf2）提供的 KDF，带 salt、
// 可调强度、NIST 认可——「salt 随机 + 强度参数 + 版本位」的三件套纪律与
// issue 的诉求一致。迭代数与算法代际的升级走 VersionV2 → v3 版本位（改本
// 常量会使新密文与存量 v2 密文不可互解——必须升版本前缀，存量按前缀
// 识别后重封）。
const PassphraseKDFIterations = 600_000

// NewPassphraseBox 从 passphrase 构造信封盒（派生前置形态，#212）：
// Seal 产出 "v2:" 密文——主密钥由 PBKDF2-HMAC-SHA256 从 passphrase +
// **每密文随机 salt** 派生（32 字节 AES-256），salt 随密文明文前置
// （解密侧按前缀取 salt 重派生），调用方无需管理 salt 生命周期。
//
// 与 NewBox 的关系：不是替代——本构造是 NewBox 的**派生前置形态**，内部
// 产出主密钥后走完全相同的 GCM Seal/Open + 版本前缀路径；存量原始密钥
// 调用方不受影响。跨形态解封按版本位拒绝（见 OpenWithAAD）。
//
// 设计取舍：
//   - **每密文随机 salt**（而非每 Box 一个固定 salt）：同 passphrase 加密
//     两条明文得到不同派生键，跨密文并链攻击面更小；代价是每条密文多
//     16 字节 salt 前缀与每次 Seal/Open 各一次 KDF（~百毫秒级，凭证量级
//     下可忽略）；
//   - **版本位复用既有纪律**：KDF 代际升级走 VersionV2 → v3，存量密文按
//     前缀识别（ErrUnsupportedVersion），迁移路径与主密钥轮换同构；
//   - **不做 passphrase 强度校验**：KDF 就是护栏——弱口令变慢不是变安全，
//     强度校验是调用方（配置侧）的职责。空串除外：那是「没配」，返回
//     ErrNoPassphrase（fail-fast，同 ErrNoKey 口径）。
//   - passphrase 以 Go string 驻留于 Box 生命周期（不可擦除——string
//     不可变；需要可擦除形态的调用方自行派生字节钥后走 NewBox）。
func NewPassphraseBox(passphrase string) (*Box, error) {
	if passphrase == "" {
		return nil, ErrNoPassphrase
	}
	return &Box{pass: passphrase}, nil
}

// derivePassphraseAEAD 从 passphrase + salt 派生主密钥并构造 AEAD。
// 相同 (passphrase, salt, iterations) 确定性地得到同一 AEAD——解密侧凭
// 密文前缀里的 salt 重放同一派生。
func derivePassphraseAEAD(passphrase string, salt []byte, iterations int) (cipher.AEAD, error) {
	key, err := pbkdf2.Key(sha256.New, passphrase, salt, iterations, passphraseKeySize)
	if err != nil {
		return nil, fmt.Errorf("secretx: kdf: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretx: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretx: new gcm: %w", err)
	}
	return aead, nil
}

// sealPassphrase 封装 "v2:" 密文：salt(16B) ‖ nonce(12B) ‖ ciphertext+tag。
// salt 与 nonce 均由 crypto/rand 生成（读取失败返回错误，绝不降级复用——
// salt 复用把不同密文并成同一派生键，nonce 复用直接交出明文）。
func (b *Box) sealPassphrase(plaintext, aad []byte) (string, error) {
	salt := make([]byte, passphraseSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("secretx: read salt: %w", err)
	}
	a, err := derivePassphraseAEAD(b.pass, salt, PassphraseKDFIterations)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secretx: read nonce: %w", err)
	}
	buf := make([]byte, 0, passphraseSaltSize+nonceSize+len(plaintext)+a.Overhead())
	buf = append(buf, salt...)
	buf = append(buf, nonce...)
	buf = a.Seal(buf, nonce, plaintext, aad)
	return VersionV2 + versionSep + base64.StdEncoding.EncodeToString(buf), nil
}

// openPassphrase 解封 "v2:" 密文：按前缀取 salt 重派生主密钥后走 GCM Open。
// 错误语义与 v1 同口径：base64 非法、负载过短、GCM 认证失败（含 passphrase
// 不匹配——派生钥不同认证必失败）一律 ErrBadCiphertext，不区分「钥不对」
// 与「密文坏了」。
func (b *Box) openPassphrase(payload string, aad []byte) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("secretx: %w", ErrBadCiphertext)
	}
	if len(raw) < passphraseSaltSize+nonceSize+gcmTagSize {
		return nil, fmt.Errorf("secretx: %w", ErrBadCiphertext)
	}
	a, err := derivePassphraseAEAD(b.pass, raw[:passphraseSaltSize], PassphraseKDFIterations)
	if err != nil {
		return nil, err
	}
	nonce := raw[passphraseSaltSize : passphraseSaltSize+nonceSize]
	plaintext, err := a.Open(nil, nonce, raw[passphraseSaltSize+nonceSize:], aad)
	if err != nil {
		return nil, fmt.Errorf("secretx: %w", ErrBadCiphertext)
	}
	return plaintext, nil
}
