package secretx

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"sync"
)

// 内存态敏感凭证持有原语（XOR 混淆 + 用后即清 + 常量时间比较）。
//
// 设计立场（与本包信封加密的分工）：
//   - **定位是内存扫描缓解，不是加密**——明文生命周期内的 heap/core dump
//     里拿不到连续明文，但对能读进程内存的攻击者只多一步还原；静态落库
//     请走本包 Box（AES-GCM 信封），两套原语别互相冒充；
//   - **Set/Clear 双向清零**：旧值与新值的存储都做零化覆盖，Get 每次在
//     独立副本上解出，副本交还给 GC 前的窗口是本原语的固有残留面；
//   - **EqualConstantTime** 走 crypto/subtle，凭证比对不上 hand-rolled
//     ==（时序侧信道）；
//   - 随机密钥生成失败（crypto/rand 几乎不可能）降级为 hex 自混淆而不是
//     panic——持有器不因环境退化拒绝服务。
//
// 收编自 zentao-mini/zentao-max 同源实现（上游 zentao 矩阵#22 下沉，逐字节等价
// 迁移后补齐用例）。

// SecureString 安全字符串持有器：XOR 混淆存储，防止内存明文扫描。
type SecureString struct {
	obfuscated []byte // 混淆后的数据
	key        []byte // XOR密钥
	mu         sync.RWMutex
}

// NewSecureString 创建安全字符串持有器。
func NewSecureString(plaintext string) *SecureString {
	s := &SecureString{}
	s.Set(plaintext)
	return s
}

// Set 设置字符串值（旧值零化后重存；空串等价 Clear）。
func (s *SecureString) Set(plaintext string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 清除旧数据
	if s.obfuscated != nil {
		for i := range s.obfuscated {
			s.obfuscated[i] = 0
		}
	}
	if s.key != nil {
		for i := range s.key {
			s.key[i] = 0
		}
	}

	if plaintext == "" {
		s.obfuscated = nil
		s.key = nil
		return
	}

	// 生成随机密钥
	key := make([]byte, len(plaintext))
	if _, err := rand.Read(key); err != nil {
		// 随机数生成失败降级为 hex 自混淆——持有器不拒绝服务
		key = []byte(hex.EncodeToString([]byte(plaintext)))[:len(plaintext)]
	}

	// XOR混淆
	obfuscated := make([]byte, len(plaintext))
	for i := 0; i < len(plaintext); i++ {
		obfuscated[i] = plaintext[i] ^ key[i]
	}

	s.obfuscated = obfuscated
	s.key = key
}

// Get 获取字符串值（每次在独立副本上解出）。
func (s *SecureString) Get() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.obfuscated == nil || s.key == nil {
		return ""
	}

	// XOR解密
	plaintext := make([]byte, len(s.obfuscated))
	for i := 0; i < len(s.obfuscated); i++ {
		plaintext[i] = s.obfuscated[i] ^ s.key[i]
	}

	return string(plaintext)
}

// Clear 清除敏感数据（存储零化 + 置空）。
func (s *SecureString) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.obfuscated != nil {
		for i := range s.obfuscated {
			s.obfuscated[i] = 0
		}
		s.obfuscated = nil
	}
	if s.key != nil {
		for i := range s.key {
			s.key[i] = 0
		}
		s.key = nil
	}
}

// EqualConstantTime 常量时间比较，防止时序攻击。
func (s *SecureString) EqualConstantTime(other string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.obfuscated == nil || s.key == nil {
		return other == ""
	}

	// 解密当前值
	plaintext := make([]byte, len(s.obfuscated))
	for i := 0; i < len(s.obfuscated); i++ {
		plaintext[i] = s.obfuscated[i] ^ s.key[i]
	}

	// 常量时间比较
	return subtle.ConstantTimeCompare(plaintext, []byte(other)) == 1
}

// SecureBytes 安全字节持有器：副本隔离存取（Get 返回副本，外部改写不穿透）。
type SecureBytes struct {
	data []byte
	mu   sync.RWMutex
}

// NewSecureBytes 创建安全字节持有器。
func NewSecureBytes(data []byte) *SecureBytes {
	s := &SecureBytes{}
	s.Set(data)
	return s
}

// Set 设置字节数组（旧值零化后重存；nil 等价 Clear）。
func (s *SecureBytes) Set(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 清除旧数据
	if s.data != nil {
		for i := range s.data {
			s.data[i] = 0
		}
	}

	if data == nil {
		s.data = nil
		return
	}

	// 复制数据
	s.data = make([]byte, len(data))
	copy(s.data, data)
}

// Get 获取字节数组（返回副本，外部修改不影响持有值）。
func (s *SecureBytes) Get() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.data == nil {
		return nil
	}

	// 返回副本，防止外部修改
	result := make([]byte, len(s.data))
	copy(result, s.data)
	return result
}

// Clear 清除数据（零化 + 置空）。
func (s *SecureBytes) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data != nil {
		for i := range s.data {
			s.data[i] = 0
		}
		s.data = nil
	}
}
