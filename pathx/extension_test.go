package pathx

import (
	"strings"
	"testing"
)

// TestSanitizeFileName_NoHalfExtension 回归测试：截断后不得留下**半个扩展名**。
//
// 缺陷成因：主名预算连一个完整 rune 都放不下时，回退分支是「整体截断 s」——
// 那会砍进扩展名（"中.txt" + 上限 5 字节 → "中.t"）。本函数自己的文档写着
// 「留半个扩展名（a.tx）比没有扩展名更糟——扩展名是骗人的，下游按它判类型会
// 判错」，实现却正好产出它。
func TestSanitizeFileName_NoHalfExtension(t *testing.T) {
	got := SanitizeFileName("中.txt", WithMaxFileNameLength(5))
	if got == "中.t" {
		t.Fatalf("SanitizeFileName(\"中.txt\", 5) = %q：留下了半个扩展名", got)
	}
	// 结果要么保留完整扩展名，要么**完全没有**扩展名。
	ext := fileNameExt(got)
	if ext != "" && ext != ".txt" {
		t.Fatalf("SanitizeFileName(\"中.txt\", 5) = %q：扩展名既不是完整的 %q 也不是空", got, ".txt")
	}
	// 还要保证不是空名。
	if got == "" {
		t.Fatal("SanitizeFileName 不应返回空串")
	}
}

// TestSanitizeFileName_LongExtensionDropped 扩展名本身超限时丢弃扩展名、
// 只截主名（既有口径）。
func TestSanitizeFileName_LongExtensionDropped(t *testing.T) {
	got := SanitizeFileName("a.verylongextension", WithMaxFileNameLength(10))
	if strings.Contains(got, ".") {
		t.Fatalf("SanitizeFileName = %q：扩展名超限时应被丢弃", got)
	}
	if len(got) > 10 {
		t.Fatalf("SanitizeFileName = %q：超出字节上限", got)
	}
}

// TestSanitizeFileName_KeepsFullExtensionWhenItFits 预算够时保留完整扩展名
// （既有口径，回归护栏）。
func TestSanitizeFileName_KeepsFullExtensionWhenItFits(t *testing.T) {
	got := SanitizeFileName("报告.pdf", WithMaxFileNameLength(10))
	if got != "报告.pdf" {
		t.Fatalf("SanitizeFileName = %q，期望 %q", got, "报告.pdf")
	}
}
