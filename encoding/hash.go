package encoding

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// Fnv1a64 返回数据的 64 位 FNV-1a 哈希值（非加密哈希，用于缓存分片/内容粗校验）。
// 跨服务持久化过哈希值的场景请勿更改算法假设（历史名 Hash，2026-09 改为如实命名）。
func Fnv1a64(data []byte) uint64 {
	// FNV-1a 64 位参数：offset basis 14695981039346656037，prime 1099511628211
	var h uint64 = 14695981039346656037
	for _, b := range data {
		h ^= uint64(b)
		h *= 1099511628211
	}
	return h
}

// Sha256Hex 返回数据的 SHA-256 十六进制字符串。
// 适用于内容校验和、缓存键等非加密签名场景（2026-09-15 治理轮：
// 移除 Md5/Md5Hex/Md5Bytes/Sha1 包装——共享 SDK 提供弱哈希入口容易被
// 误用到凭据/签名场景；确有 MD5/SHA-1 合规需求的业务自行引 crypto 标准库）。
func Sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Sha256 返回字符串的 SHA-256 十六进制字符串。
func Sha256(str string) string {
	return Sha256Hex([]byte(str))
}

// Base64Encode 返回字符串的 Base64 编码。
//
// ⚠️ 本方法是**带填充**变体（base64.StdEncoding，末尾补 '='）——与 stdlib 默认
// 口径一致。JWT / OIDC / PKCE 协议段用的是**无填充**变体，请用 Base64RawEncode。
func Base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// Base64Decode 解码 Base64 编码的字符串。
//
// ⚠️ 本方法是**带填充**变体（base64.StdEncoding）——输入必须带 '=' 填充。
// JWT / OIDC / PKCE 协议段（无填充）请用 Base64RawDecode：拿本方法解无填充串
// 会报 "illegal base64 data at input byte N"。
func Base64Decode(s string) (string, error) {
	rs, err := base64.StdEncoding.DecodeString(s)
	return string(rs), err
}

// Base64EncodeBytes 返回字节切片的 Base64 编码（字节→字节方向的免拷贝入口）。
//
// ⚠️ 带填充变体；无填充请用 Base64RawEncodeBytes。
func Base64EncodeBytes(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// Base64DecodeBytes 解码 Base64 为原始字节（pkg#12）。
// base64 本质是字节↔字节编码——结果按字节使用（凭据还原/图片/PEM DER 等）
// 用本方法从类型层面表达意图；Base64Decode 返回 string，供结果按文本处理的
// 场景，字节场景经它中转会引入一次多余拷贝且编译器无法发现误用。
//
// ⚠️ 带填充变体；无填充请用 Base64RawDecodeBytes。
func Base64DecodeBytes(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// Base64URLEncode 返回字符串的 Base64 URL 安全编码。
//
// ⚠️ 本方法是**带填充**变体（base64.URLEncoding，末尾补 '='）——与 stdlib 默认
// 口径一致。JWT / OIDC / PKCE 协议段用的是**无填充**变体，请用
// Base64RawURLEncode。
func Base64URLEncode(s string) string {
	return base64.URLEncoding.EncodeToString([]byte(s))
}

// Base64URLDecode 解码 Base64 URL 安全编码的字符串。
//
// ⚠️ 本方法是**带填充**变体（base64.URLEncoding）——输入必须带 '=' 填充。
// JWT / OIDC / PKCE 协议段（无填充）请用 Base64RawURLDecode：拿本方法解无填充串
// 会报 "illegal base64 data at input byte N"。
func Base64URLDecode(s string) (string, error) {
	rs, err := base64.URLEncoding.DecodeString(s)
	return string(rs), err
}

// Base64RawEncode 返回字符串的 Base64 编码（**无填充**变体，base64.RawStdEncoding）。
//
// 与 Base64Encode 的差别只有一处：末尾不补 '='。命名与 stdlib 对齐
// （base64.RawStdEncoding）。
func Base64RawEncode(s string) string {
	return base64.RawStdEncoding.EncodeToString([]byte(s))
}

// Base64RawDecode 解码 Base64 编码的字符串（**无填充**变体，base64.RawStdEncoding）。
//
// 与 Base64Decode 的差别只有一处：输入不带 '=' 填充——JWT / OIDC / PKCE 协议段
// 正是这个形态（RFC 7515 §2：base64url 编码时省略 '=' 填充）。
func Base64RawDecode(s string) (string, error) {
	rs, err := base64.RawStdEncoding.DecodeString(s)
	return string(rs), err
}

// Base64RawEncodeBytes 返回字节切片的 Base64 编码（**无填充**变体，
// base64.RawStdEncoding；字节→字节方向的免拷贝入口）。
func Base64RawEncodeBytes(b []byte) string {
	return base64.RawStdEncoding.EncodeToString(b)
}

// Base64RawDecodeBytes 解码 Base64 为原始字节（**无填充**变体，
// base64.RawStdEncoding）。与 Base64DecodeBytes 的差别只有一处：输入不带
// '=' 填充。
func Base64RawDecodeBytes(s string) ([]byte, error) {
	return base64.RawStdEncoding.DecodeString(s)
}

// Base64RawURLEncode 返回字符串的 Base64 URL 安全编码（**无填充**变体，
// base64.RawURLEncoding）。
//
// 与 Base64URLEncode 的差别只有一处：末尾不补 '='。这是 JWT / OIDC / PKCE
// 协议段的实际形态——base64url 的填充字符 '=' 在协议里不允许出现
// （RFC 7515 §2）。
func Base64RawURLEncode(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

// Base64RawURLDecode 解码 Base64 URL 安全编码的字符串（**无填充**变体，
// base64.RawURLEncoding）。
//
// 与 Base64URLDecode 的差别只有一处：输入不带 '=' 填充——JWT / OIDC / PKCE
// 协议段正是这个形态。拿带填充的 Base64URLDecode 解它会报
// "illegal base64 data at input byte N"。
func Base64RawURLDecode(s string) (string, error) {
	rs, err := base64.RawURLEncoding.DecodeString(s)
	return string(rs), err
}

// Base64RawURLEncodeBytes 返回字节切片的 Base64 URL 安全编码（**无填充**变体，
// base64.RawURLEncoding；字节→字节方向的免拷贝入口）。
//
// 与 Base64RawURLEncode 的差别只有一处：入参是字节切片，不经一次
// string↔[]byte 拷贝。JWT / OIDC / PKCE 协议段的原文常以 []byte 形态在手
// （签名校验、密钥材料），走字节入口省掉一次拷贝（pkg#105）。
func Base64RawURLEncodeBytes(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// Base64RawURLDecodeBytes 解码 Base64 URL 安全编码为原始字节（**无填充**变体，
// base64.RawURLEncoding）。
//
// 与 Base64RawURLDecode 的差别只有一处：出参是字节切片——结果按字节使用的场景
// （凭据还原 / PEM DER / 签名原文比对）不必再转一次 string，也不必担心把二进制
// 塞进 string 时的不可见字符问题（pkg#105）。
func Base64RawURLDecodeBytes(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
