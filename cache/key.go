package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Key 用冒号连接各部分生成缓存 key。
//
//	cache.Key("user", "123", "profile") // "user:123:profile"
func Key(parts ...string) string {
	return strings.Join(parts, ":")
}

// HashKey 基于参数生成 hash key，适用于复杂参数的缓存 key。
//
// 摘要输入采用**长度前缀的无歧义编码**：每个参数写成两段——「类型名长度:类型名」
// 与「值长度:值」⇒ 参数值里的 ':' 与「参数边界」不再不可区分。
//
// 此前用 "%v:" 直接拼接，两种参数组会算出**完全相同**的 key：
//
//	HashKey("p", "a", "b") == HashKey("p", "a:b")   // 值里的 ':' 撞上参数边界
//	HashKey("p", 1)        == HashKey("p", "1")      // int 与 string 互撞
//
// ⇒ 语义不同的两组参数命中同一条缓存 ⇒ A 的结果被当成 B 的返回（静默、且在
// 参数含 ':' 时才触发 ⇒ 租户名/关键词/路径进参数时就是跨租户串数据）(#146)。
//
// 摘要取 SHA-256 前 16 位 hex（2026-09-15 治理轮：md5 → sha256，缓存 key
// 属非加密场景但无需为弱哈希留误用空间）。key 为进程/缓存生命周期内临时值，
// 无持久化契约 ⇒ 换编码方式不构成破坏性变更。
//
//	cache.HashKey("search", "keyword", 1, 20) // "search:a1b2c3d4e5f6g7h8"
func HashKey(prefix string, args ...any) string {
	h := sha256.New()
	for _, arg := range args {
		typ := fmt.Sprintf("%T", arg)
		val := fmt.Sprintf("%v", arg)
		// 长度前缀（字节数）后跟内容：按长度取字节 ⇒ 解析无歧义。
		// 类型名一并写入 ⇒ 1 与 "1"（不同类型的参数组）也不同键。
		fmt.Fprintf(h, "%d:%s%d:%s", len(typ), typ, len(val), val)
	}
	return prefix + ":" + hex.EncodeToString(h.Sum(nil))[:16]
}

// UserKey 生成用户维度的缓存 key。
//
//	cache.UserKey(123, "profile") // "user:123:profile"
func UserKey(userID int64, suffix string) string {
	return "user:" + strconv.FormatInt(userID, 10) + ":" + suffix
}

// TenantKey 生成租户维度的缓存 key。
//
//	cache.TenantKey("tenant1", "config") // "tenant:tenant1:config"
func TenantKey(tenantID, suffix string) string {
	return "tenant:" + tenantID + ":" + suffix
}
