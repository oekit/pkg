package secretx

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// 哨兵错误（errors.Is 判定）。
var (
	// ErrNoKey 主密钥缺失（空串/空切片）。构造期 fail-fast，不存在「缺钥
	// 明文透传」的降级路径——本地开发需要绕过时请自行用恒等实现。
	ErrNoKey = errors.New("secretx: master key missing")
	// ErrBadKeySize 主密钥长度不在 16/24/32 字节（AES-128/192/256）范围内。
	ErrBadKeySize = errors.New("secretx: master key size must be 16, 24 or 32 bytes")
	// ErrBadCiphertext 密文损坏或主密钥不匹配（GCM 认证失败）。两种情形
	// 统一表述，不泄露细节——「哪条密文是用哪把钥封的」不该可被日志反推。
	ErrBadCiphertext = errors.New("secretx: bad ciphertext")
	// ErrUnsupportedVersion 密文版本前缀未识别（主密钥轮换识别位：v2 密钥
	// 落地后，下游可按前缀选钥解密重封）。
	ErrUnsupportedVersion = errors.New("secretx: unsupported ciphertext version")
)

// VersionV1 原始主密钥形态的密文版本前缀（"v1:"）。与 domain-manage 手搓
// 形态逐字节兼容，存量数据零迁移。
const VersionV1 = "v1"

// VersionV2 passphrase 派生形态的密文版本前缀（"v2:"，#212）：主密钥由
// KDF 从 passphrase + 每密文随机 salt 派生，salt 随密文明文前置。KDF 代际
// 升级（换算法/换参数）走本版本位——升 v3 后旧密文按前缀识别、拒绝解封
// 并提示重新加密（ErrUnsupportedVersion），迁移路径与主密钥轮换同构。
const VersionV2 = "v2"

// versionSep 版本前缀与密文负载的分隔符。
const versionSep = ":"

// nonceSize GCM 标准 nonce 长度。
const nonceSize = 12

// ParseKey 解析 base64 编码的主密钥：解码 + 16/24/32 字节校验，错误带实际
// 长度上下文。供「主密钥从环境变量/配置文件进来」的消费方一行完成入口校验。
func ParseKey(encoded string) ([]byte, error) {
	if encoded == "" {
		return nil, ErrNoKey
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("secretx: master key 不是合法 base64: %w", err)
	}
	if !validKeySize(len(key)) {
		return nil, fmt.Errorf("secretx: master key 解码后 %d 字节: %w", len(key), ErrBadKeySize)
	}
	return key, nil
}

// validKeySize 判定 AES 合法密钥长度。
func validKeySize(n int) bool {
	return n == 16 || n == 24 || n == 32
}

// Box 信封盒：两种构造形态共用一个类型（#212）——
//
//   - NewBox(masterKey)：原始主密钥形态，Seal 产出 "v1:" 密文；
//   - NewPassphraseBox(passphrase)：passphrase 派生形态（KDF 前置），
//     Seal 产出 "v2:" 密文（per-ciphertext 随机 salt 前置）。
//
// 主密钥/passphrase 不可变；可并发使用（两形态的加密路径均无共享可变
// 状态——派生形态每次 Seal 现派现用）。跨形态解封按版本位拒绝
// （ErrUnsupportedVersion），见 OpenWithAAD。
type Box struct {
	aead cipher.AEAD // 原始主密钥形态的 AEAD（passphrase 形态为 nil）
	pass string      // passphrase 派生形态（原始主密钥形态为空）
}

// NewBox 以原始主密钥（16/24/32 字节 AES 钥）创建信封盒——"v1:" 形态。
// 消费方拿到的是 passphrase 时改用 NewPassphraseBox（派生前置形态，#212）。
// 缺钥返回 ErrNoKey、长度非法返回 ErrBadKeySize（构造期 fail-fast——
// 加密原语没有「带病运行」的合法形态）。
func NewBox(masterKey []byte) (*Box, error) {
	if len(masterKey) == 0 {
		return nil, ErrNoKey
	}
	if !validKeySize(len(masterKey)) {
		return nil, fmt.Errorf("secretx: master key %d 字节: %w", len(masterKey), ErrBadKeySize)
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("secretx: new cipher: %w", err) // 长度已校验，防御性
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretx: new gcm: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Seal 封装明文：原始主密钥形态返回 "v1:" + base64(nonce‖ciphertext+tag)，
// passphrase 派生形态返回 "v2:" + base64(salt‖nonce‖ciphertext+tag)（#212，
// 见 passphrase.go）。随机 nonce 由 crypto/rand 生成（读取失败返回错误，
// 绝不降级复用 nonce——GCM 复用 nonce 等于交出明文）。
func (b *Box) Seal(plaintext []byte) (string, error) {
	return b.SealWithAAD(plaintext, nil)
}

// SealWithAAD 同 Seal，额外绑定附加认证数据（行 id、用途标签等）：AAD 不入
// 密文但参与认证——密文挪到别的行/别的用途上解封必失败。
//
// 版本路由（#212）：passphrase 派生形态（NewPassphraseBox）产出 "v2:" 密文
// （salt 前置，见 passphrase.go）；原始主密钥形态产出 "v1:"（历史行为不变）。
func (b *Box) SealWithAAD(plaintext, aad []byte) (string, error) {
	if b == nil {
		return "", errors.New("secretx: Box 未初始化（须由 NewBox/NewPassphraseBox 创建）")
	}
	if b.pass != "" {
		return b.sealPassphrase(plaintext, aad)
	}
	a, err := b.aeadOrErr()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secretx: read nonce: %w", err)
	}
	// Seal 返回 append(dst, ciphertext+tag...) ⇒ nonce‖ct
	sealed := a.Seal(nonce, nonce, plaintext, aad)
	return VersionV1 + versionSep + base64.StdEncoding.EncodeToString(sealed), nil
}

// Open 解封 Seal 的产物。
func (b *Box) Open(ciphertext string) ([]byte, error) {
	return b.OpenWithAAD(ciphertext, nil)
}

// OpenWithAAD 解封带 AAD 的密文（AAD 必须与 SealWithAAD 时逐字节一致）。
//
// 版本路由（#212）：按版本前缀选解封路径——"v1" 原始主密钥形态、
// "v2" passphrase 派生形态（salt 前缀取盐重派生）。跨形态解封（原始钥
// Box 开 v2 密文 / passphrase Box 开 v1 密文）返回 ErrUnsupportedVersion
// ——按版本位选 Box 是轮换识别位的设计用法。
//
// 错误语义：版本前缀缺失/未识别/跨形态 → ErrUnsupportedVersion（轮换
// 识别位）；base64 非法、负载过短、GCM 认证失败 → 一律 ErrBadCiphertext
// （统一表述——不向日志泄露「哪条密文是用哪把钥封的」）。
func (b *Box) OpenWithAAD(ciphertext string, aad []byte) ([]byte, error) {
	if b == nil {
		return nil, errors.New("secretx: Box 未初始化（须由 NewBox/NewPassphraseBox 创建）")
	}
	version, payload, ok := strings.Cut(ciphertext, versionSep)
	if !ok {
		return nil, fmt.Errorf("secretx: 密文缺少版本前缀: %w", ErrUnsupportedVersion)
	}
	switch version {
	case VersionV1:
		if b.pass != "" {
			return nil, fmt.Errorf("secretx: 密文是原始主密钥形态(%s)，当前 Box 由 NewPassphraseBox 构造（无原始主密钥）: %w", VersionV1, ErrUnsupportedVersion)
		}
		return b.openV1(payload, aad)
	case VersionV2:
		if b.pass == "" {
			return nil, fmt.Errorf("secretx: 密文是 passphrase 派生形态(%s)，当前 Box 由 NewBox 构造（无 passphrase）: %w", VersionV2, ErrUnsupportedVersion)
		}
		return b.openPassphrase(payload, aad)
	default:
		return nil, fmt.Errorf("secretx: 密文版本 %q: %w", version, ErrUnsupportedVersion)
	}
}

// openV1 是 "v1:" 原始主密钥形态的解封路径（历史行为原样收拢）。
func (b *Box) openV1(payload string, aad []byte) ([]byte, error) {
	a, err := b.aeadOrErr()
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("secretx: %w", ErrBadCiphertext)
	}
	if len(raw) < nonceSize+a.Overhead() {
		return nil, fmt.Errorf("secretx: %w", ErrBadCiphertext)
	}
	plaintext, err := a.Open(nil, raw[:nonceSize], raw[nonceSize:], aad)
	if err != nil {
		return nil, fmt.Errorf("secretx: %w", ErrBadCiphertext)
	}
	return plaintext, nil
}

// aeadOrErr 零值/nil Box 的防误用出口：加密原语的零值不可用必须显式报错，
// 而不是静默产出空密文。
func (b *Box) aeadOrErr() (cipher.AEAD, error) {
	if b == nil || b.aead == nil {
		return nil, errors.New("secretx: Box 未初始化（须由 NewBox 创建）")
	}
	return b.aead, nil
}
