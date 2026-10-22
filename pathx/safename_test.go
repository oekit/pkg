package pathx

import "testing"

func TestIsSafeFileName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		// 普通单段文件名
		{"plain", "report.txt", true},
		{"no extension", "report", true},
		{"with spaces inside", "my report.txt", true},
		{"non ascii", "报告.txt", true},
		{"leading dot file", ".gitignore", true},
		{"dots inside", "a..b.txt", true},
		{"triple dot inside", "..a..b", true},

		// 空 / 空白
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"tab only", "\t", false},

		// 点段（filepath.Base 放行的那两个）
		{"dot", ".", false},
		{"dotdot", "..", false},

		// 多段（不是单段）
		{"slash", "a/b.txt", false},
		{"backslash", `a\b.txt`, false},
		{"deep path", "a/b/c.txt", false},
		{"leading slash absolute", "/etc/passwd", false},
		{"windows absolute", `C:\Windows\System32`, false},
		{"unc", `\\host\share\x`, false},

		// Windows 盘符相对路径（fail-closed）
		{"drive relative", "C:foo", false},
		{"drive relative lowercase", "c:foo", false},
		{"drive relative with ext", "C:foo.bundle", false},
		{"colon after letter", "a:b.txt", false},

		// 目录成分穿越
		{"traversal", "../etc/passwd", false},
		{"traversal backslash", `..\..\etc\passwd`, false},
		{"dot segment inside", "a/./b", false},
		{"parent inside", "a/../b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSafeFileName(tt.in); got != tt.want {
				t.Errorf("IsSafeFileName(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// IsSafeFileName 必须与 SanitizeRelStrict 严格档同源判定：
// 同一个输入，两处结论必须一致（否则又是「同一仓里两份实现」的口径漂移）。
func TestIsSafeFileName_AgreesWithSanitizeRelStrict(t *testing.T) {
	inputs := []string{
		"", ".", "..", "a", "a.txt", ".gitignore", "a..b",
		"a/b", `a\b`, "/abs", `C:\x`, "C:x", "c:x", "a:b",
		"../x", `..\x`, "  ", "报告.txt",
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			_, strictOK := SanitizeRelStrict(in)
			// 严格档放行但含分隔符的输入，单段判定必须拒绝。
			if !strictOK && IsSafeFileName(in) {
				t.Errorf("IsSafeFileName(%q) = true，严格档已拒绝", in)
			}
			if strictOK && !IsSafeFileName(in) {
				// 严格档放行且无分隔符的输入，单段判定应放行。
				if !containsAnySep(in) {
					t.Errorf("IsSafeFileName(%q) = false，严格档已放行", in)
				}
			}
		})
	}
}

func containsAnySep(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' || s[i] == '\\' {
			return true
		}
	}
	return false
}
