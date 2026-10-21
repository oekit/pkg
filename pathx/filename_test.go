package pathx

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeFileName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// 普通名字原样保留
		{"plain", "report.txt", "report.txt"},
		{"no extension", "report", "report"},
		{"with spaces inside", "my report.txt", "my report.txt"},

		// 目录成分剥离（"/" 与 "\" 同为分隔符）
		{"unix dir stripped", "a/b/c.txt", "c.txt"},
		{"windows dir stripped", `C:\Users\me\报告.txt`, "报告.txt"},
		{"mixed separators", `a\b/c.txt`, "c.txt"},
		{"trailing separator", "a/b/", "file"},    // 最后一段为空 → 回退
		{"only separator", "/", "file"},           //
		{"dot segment", "a/./c.txt", "c.txt"},     //
		{"parent segment", "a/../c.txt", "c.txt"}, // 文件名不看 traversal

		// 控制字符丢弃
		{"nul dropped", "a\x00b.txt", "ab.txt"},
		{"newline dropped", "a\nb.txt", "ab.txt"},
		{"tab dropped", "a\tb.txt", "ab.txt"},
		{"del dropped", "a\x7fb.txt", "ab.txt"},
		{"esc dropped", "a\x1bb.txt", "ab.txt"},

		// Windows 保留字符替换为下划线
		{"colon", "a:b.txt", "a_b.txt"},
		{"star", "a*b.txt", "a_b.txt"},
		{"question", "a?b.txt", "a_b.txt"},
		{"quote", `a"b.txt`, "a_b.txt"},
		{"lt gt", "a<b>c.txt", "a_b_c.txt"},
		{"pipe", "a|b.txt", "a_b.txt"},
		{"all reserved", `:*?"<>|`, "file"}, // 纯符号 → 回退

		// 首尾空白与结尾的点
		{"trailing space", "file.txt ", "file.txt"},
		{"leading space", " file.txt", "file.txt"},
		{"both spaces", "  file.txt  ", "file.txt"},
		{"trailing dot", "file.txt.", "file.txt"},
		{"trailing dots", "file.txt...", "file.txt"},
		{"dot and space", "file. ", "file"},
		{"leading dot kept", ".gitignore", ".gitignore"},
		{"dotfile with ext", ".bashrc", ".bashrc"},
		{"interior dots kept", "a.b.c.txt", "a.b.c.txt"},

		// 空名 / 纯符号名回退
		{"empty", "", "file"},
		{"whitespace only", "   ", "file"},
		{"dots only", "...", "file"},
		{"underscores only", "___", "file"},
		{"stars only", "***", "file"},
		{"control only", "\x00\x01", "file"},

		// 非 ASCII 保留
		{"chinese", "报告.txt", "报告.txt"},
		{"japanese", "日本語のファイル.txt", "日本語のファイル.txt"},
		{"emoji", "🎉party.txt", "🎉party.txt"},
		{"cjk with reserved", "报告:终稿.txt", "报告_终稿.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeFileName(tt.in)
			if got != tt.want {
				t.Errorf("SanitizeFileName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeFileName_OutputIsValidUTF8(t *testing.T) {
	inputs := []string{
		"a\xffb.txt", // 非法 UTF-8 字节
		"\xff\xfe",
		"报告\xff.txt",
	}
	for _, in := range inputs {
		got := SanitizeFileName(in)
		if !utf8.ValidString(got) {
			t.Errorf("SanitizeFileName(%q) = %q, 不是合法 UTF-8", in, got)
		}
	}
}

func TestSanitizeFileName_MaxLength(t *testing.T) {
	tests := []struct {
		name string
		in   string
		// maxBytes 为上限（字节）；<=0 表示不限。
		maxBytes int
		want     string
		// fallback 为 true 表示结果是回退名——回退名逐字透传，不受上限约束。
		fallback bool
	}{
		{"under limit untouched", "report.txt", 100, "report.txt", false},
		{"exact limit untouched", "report.txt", 10, "report.txt", false},
		{"truncate keeps ext", "verylongfilename.txt", 10, "verylo.txt", false},
		{"truncate by bytes not runes", "中文中文中文.txt", 10, "中文.txt", false},
		{"ext dropped when it cannot fit", "a.txt", 4, "a", false},
		{"rune boundary kept", "中文.txt", 4, "中", false},
		{"no extension truncated", "abcdefghij", 5, "abcde", false},
		{"single rune name fits", "中", 3, "中", false},
		{"name shorter than one rune", "中文", 2, "file", true},
		{"zero means unlimited", "abcdefghij", 0, "abcdefghij", false},
		{"negative means unlimited", "abcdefghij", -1, "abcdefghij", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeFileName(tt.in, WithMaxFileNameLength(tt.maxBytes))
			if got != tt.want {
				t.Errorf("SanitizeFileName(%q, max=%d) = %q, want %q", tt.in, tt.maxBytes, got, tt.want)
			}
			if !tt.fallback && tt.maxBytes > 0 && len(got) > tt.maxBytes {
				t.Errorf("SanitizeFileName(%q, max=%d) = %q, 长度 %d 字节超出上限", tt.in, tt.maxBytes, got, len(got))
			}
			if !utf8.ValidString(got) {
				t.Errorf("SanitizeFileName(%q, max=%d) = %q, 不是合法 UTF-8", tt.in, tt.maxBytes, got)
			}
		})
	}
}

func TestSanitizeFileName_Fallback(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "unnamed"},
		{"pure symbol", "***", "unnamed"},
		{"truncated to nothing", "中文", "unnamed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeFileName(tt.in, WithFileNameFallback("unnamed"), WithMaxFileNameLength(2))
			if got != tt.want {
				t.Errorf("SanitizeFileName(%q, fallback=unnamed) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeFileName_FallbackVerbatim(t *testing.T) {
	// 回退名是调用方给的常量：原样透传，不参与消毒、不受长度上限约束。
	got := SanitizeFileName("", WithFileNameFallback("  a:b  "), WithMaxFileNameLength(1))
	if got != "  a:b  " {
		t.Errorf("回退名应原样透传, got %q", got)
	}
}

func TestSanitizeFileName_NoSideEffectOnInput(t *testing.T) {
	in := "a/b/c.txt"
	_ = SanitizeFileName(in)
	if in != "a/b/c.txt" {
		t.Errorf("输入被改写: %q", in)
	}
}

func TestSanitizeFileName_DefaultMaxLength(t *testing.T) {
	// 缺省上限 255 字节（ext4 单目录项上限口径）。
	long := strings.Repeat("a", 300)
	got := SanitizeFileName(long)
	if len(got) != 255 {
		t.Errorf("缺省上限应 255 字节, got %d", len(got))
	}
}
