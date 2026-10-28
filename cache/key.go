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
// 哈希取 SHA-256 前 16 位 hex（2026-09-15 治理轮：md5 → sha256，缓存 key
// 属非加密场景但无需为弱哈希留误用空间；摘要值较 v0.28.x 变化，key 为
// 进程/缓存生命周期内临时值，无持久化契约）。
//
// # 编码方式（2026-10-28 修，见 #146）
//
// 每个参数按「**类型标签 + 长度前缀 + 值**」写入摘要输入，**参数边界与值内容都不可歧义**：
//
//	arg  ⇒  len("%T=%v") || "%T=%v"
//
// 旧写法是 `fmt.Fprintf(h, "%v:", arg)`——参数值里的 `:` 与「参数边界」
// 不可区分 ⇒ 不同参数组会算出**完全相同**的摘要输入：
//
//	HashKey("p", "a", "b") == HashKey("p", "a:b")        // 旧写法：同键
//	HashKey("p", 1)        == HashKey("p", "1")         // 旧写法：同键
//
// ⇒ 语义不同的查询命中同一条缓存 ⇒ 串数据（跨租户/跨用户时即越权）。
// 新写法下这两组都产出不同 key。摘要值随之变化（key 无持久化契约）。
//
//	cache.HashKey("search", "keyword", 1, 20) // "search:<16 位 hex>"
func HashKey(prefix string, args ...any) string {
	h := sha256.New()
	for _, arg := range args {
		// 类型标签区分 int 与 "1" 这类「值相同但类型不同」的参数；
		// 长度前缀让参数边界不可混淆（"a","b" 与 "a:b" 必须不同）。
		s := fmt.Sprintf("%T=%v", arg, arg)
		fmt.Fprintf(h, "%d:", len(s))
		h.Write([]byte(s))
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
