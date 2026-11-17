package stringx

import (
	"regexp"
	"testing"
)

// #531：UUIDv4 RFC 4122 形态契约（全矩阵 UUIDv4 单源）。

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestUUIDv4_Format(t *testing.T) {
	// 8-4-4-4-12 小写 hex；版本位恒 '4'（第 3 段首字符）；变体位恒
	// [89ab]（第 4 段首字符，10xx）。
	for i := 0; i < 100; i++ {
		id := UUIDv4()
		if len(id) != 36 {
			t.Fatalf("UUID 长度应 36, got %d (%q)", len(id), id)
		}
		if !uuidV4Pattern.MatchString(id) {
			t.Fatalf("不符合 RFC 4122 v4 形态: %q", id)
		}
	}
}

func TestUUIDv4_Unique(t *testing.T) {
	// 122 bit 随机性下 500 连发撞车概率工程上为零；这是护栏而非统计证明
	// ——若版本/变体位被错置或随机源退化为固定值，这里立刻红。
	seen := make(map[string]bool, 500)
	for i := 0; i < 500; i++ {
		id := UUIDv4()
		if seen[id] {
			t.Fatalf("UUID 撞车: %q", id)
		}
		seen[id] = true
	}
}
