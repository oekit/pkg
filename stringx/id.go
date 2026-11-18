package stringx

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/oekit/pkg/errors"
)

// GenerateID 生成基于时间戳的唯一 ID，格式: 20060102150405-<8位hex>。
// 适用于非严格唯一性要求的场景（如日志关联、临时标识）。
// 如需全局唯一，请使用 UUIDv4。
func GenerateID() string {
	now := time.Now()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// 熵源不可用时降级：用纳秒时间戳的低 32 位。
		// 碰撞概率高于 crypto/rand 但仍可接受（同一秒内不同纳秒）。
		binary.LittleEndian.PutUint32(b, uint32(now.UnixNano()))
	}
	return now.Format("20060102150405") + "-" + hex.EncodeToString(b)
}

// UUIDv4 生成 RFC 4122 version 4 随机 UUID（#531）：crypto/rand 16 字节，
// b[6]&0x0f|0x40 置版本位、b[8]&0x3f|0x80 置变体位，标准
// 8-4-4-4-12 连字符格式（小写 hex）。
//
// 全矩阵 UUIDv4 的**单源**：kitx/uidgen 已委托本函数；pkg/webhook
// （NewEventID）走 [UUIDv4Err] 错误返回变体（其契约是返回 error）。
//
// 随机源失败直接 panic（与 tracing/traceid.NewID 同理，仓规总则③）：
// UUID 的存在意义就是全局唯一，生成失败静默回退（空串/零串/时间戳）会让
// 两个调用方拿到同一个"UUID"，撞车没有下界——这类失败必须显式炸出来。
// crypto/rand.Read 自 Go 1.24 起保证填满且不返回错误（熵源不可用直接
// panic 在其内部），此分支仅为显式契约，理论不可达。
func UUIDv4() string {
	b, err := uuidv4Bytes()
	if err != nil {
		panic(fmt.Sprintf("stringx: UUIDv4 crypto/rand failed: %v", err))
	}
	return formatUUIDv4(b)
}

// UUIDv4Err 是 [UUIDv4] 的错误返回变体（#550 收割）：生成语义逐字相同，
// 区别仅在随机源失败时返回 error 而非 panic——供 webhook.NewEventID 这类
// 契约就是「返回 error」的调用方收编（幂等键生成失败必须显式带回调用方，
// 不该打死进程）。两形态共用同一内核（uuidv4Bytes/formatUUIDv4），不存在
// 第二份字节布局/格式化实现可漂移。
func UUIDv4Err() (string, error) {
	b, err := uuidv4Bytes()
	if err != nil {
		return "", errors.Newf("stringx: UUIDv4 crypto/rand failed: %v", err)
	}
	return formatUUIDv4(b), nil
}

// uuidv4Bytes 生成 UUIDv4 的 16 字节随机载荷并置版本/变体位（两种返回
// 形态的共同内核）。
func uuidv4Bytes() ([16]byte, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return b, err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx
	return b, nil
}

// formatUUIDv4 输出标准 8-4-4-4-12 连字符格式（小写 hex）。
func formatUUIDv4(b [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ContainsAny 检查 text 中是否包含 keywords 中的任意一个关键词（不区分大小写）。
//
// 用法:
//
//	stringx.ContainsAny("Security vulnerability found", "security", "vuln") // true
//	stringx.ContainsAny("normal text", "critical", "major")                 // false
func ContainsAny(text string, keywords ...string) bool {
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// IndentText 为多行文本的第二行及之后每行添加缩进前缀。
// 第一行不缩进，适用于续行对齐场景。
//
// 用法:
//
//	stringx.IndentText("line1\nline2\nline3", "  ")
//	// => "line1\n  line2\n  line3"
func IndentText(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
