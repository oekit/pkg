package pathx

import (
	"strings"
	"testing"
)

func TestSanitizeRel(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		// 正常路径
		{"plain relative", "a/b.go", "a/b.go", true},
		{"single segment", "main.go", "main.go", true},
		{"dot prefix", "./a/b.go", "a/b.go", true},
		{"dot segment in middle", "a/./b.go", "a/b.go", true},
		{"double separator", "a//b.go", "a/b.go", true},
		{"trailing separator", "a/b/", "a/b", true},
		{"dotdot cancels segment", "a/../b.go", "b.go", true},
		{"dotdot cancels nested", "a/b/../c.go", "a/c.go", true},
		{"dotdot only segment", "..", "", false},
		{"dotdot prefix", "../etc/passwd", "", false},
		{"dotdot after cancel escapes", "a/../../b.go", "", false},
		{"dotdot in middle stays inside", "a/../b/../c.go", "c.go", true},

		// 绝对路径
		{"absolute unix", "/etc/passwd", "", false},
		{"absolute unix root", "/", "", false},
		{"unc path", "//host/share/x", "", false},
		{"windows drive backslash", `C:\Windows\System32`, "", false},
		{"windows drive slash", "C:/Windows/System32", "", false},
		{"windows drive relative form", "C:foo", "", false},
		{"lowercase drive", "c:/foo", "", false},

		// 反斜杠分隔符（跨平台一致口径）
		{"backslash separator", `a\b.go`, "a/b.go", true},
		{"backslash traversal", `..\..\etc\passwd`, "", false},
		{"mixed separators", `a\b/../c.go`, "a/c.go", true},

		// 空 / 空白 / 指向根目录
		{"empty", "", "", false},
		{"whitespace only", "   ", "", false},
		{"tab only", "\t", "", false},
		{"dot", ".", "", false},
		{"cancels to root", "a/..", "", false},

		// 非 traversal 的 ".." 字面量
		{"double dot inside name", "a..b", "a..b", true},
		{"triple dot inside name", "..a", "..a", true},
		{"name with dots", "..foo/bar..baz", "..foo/bar..baz", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := SanitizeRel(tt.in)
			if got != tt.want || ok != tt.ok {
				t.Errorf("SanitizeRel(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestSanitizeRelStrict(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		// 严格档：显式 ".." 段一律拒绝
		{"dotdot cancels segment rejected", "a/../b.go", "", false},
		{"dotdot nested rejected", "a/b/../c.go", "", false},
		{"dotdot prefix rejected", "../etc/passwd", "", false},
		{"dotdot only segment rejected", "..", "", false},
		{"backslash dotdot rejected", `a\..\b.go`, "", false},

		// 其余判定与宽松档一致
		{"plain relative", "a/b.go", "a/b.go", true},
		{"dot prefix", "./a/b.go", "a/b.go", true},
		{"double separator", "a//b.go", "a/b.go", true},
		{"trailing separator", "a/b/", "a/b", true},
		{"backslash separator", `a\b.go`, "a/b.go", true},
		{"absolute rejected", "/etc/passwd", "", false},
		{"drive rejected", "C:/foo", "", false},
		{"empty rejected", "", "", false},
		{"dot rejected", ".", "", false},
		{"double dot inside name kept", "a..b", "a..b", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := SanitizeRelStrict(tt.in)
			if got != tt.want || ok != tt.ok {
				t.Errorf("SanitizeRelStrict(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// 两档的差异只在「显式 .. 段」：宽松档抵消它，严格档拒绝它。
// 其余输入两档必须同判——否则"严格档更安全"的说法就不成立（可能只是更严，
// 也可能更宽），消费方无法在宽松档之上选严格档。
func TestSanitizeRel_TwoTiersShareJudgement(t *testing.T) {
	inputs := []string{
		"a/b.go", "./a/b.go", "a//b.go", "a/b/", "a..b", "..a",
		"/etc/passwd", "//host/share", "C:/foo", `C:\foo`, "", "   ", ".",
		"a/..", "../x", "a/../../x", `..\..\x`, "a/../b.go",
	}
	for _, in := range inputs {
		lo, loOK := SanitizeRel(in)
		st, stOK := SanitizeRelStrict(in)
		if loOK && !stOK {
			continue // 唯一允许的差异：严格档更严
		}
		if loOK != stOK || lo != st {
			t.Errorf("两档判定不一致：in=%q 宽松=(%q,%v) 严格=(%q,%v)", in, lo, loOK, st, stOK)
		}
	}
}

// 清洗结果必须是「相对路径且不含 . / .. 段」——调用方会直接把它拼到
// 仓库根目录下取文件，任何残留段都是逃逸的口子。
func TestSanitizeRel_ResultIsInert(t *testing.T) {
	inputs := []string{
		"a/b.go", "./a/b.go", "a/../b.go", `a\b.go`, "a//b.go", "a/./b/c.go",
	}
	for _, in := range inputs {
		got, ok := SanitizeRel(in)
		if !ok {
			continue
		}
		if got == "" {
			t.Errorf("SanitizeRel(%q) 返回空串却 ok=true", in)
			continue
		}
		if got[0] == '/' {
			t.Errorf("SanitizeRel(%q) = %q：结果不应以 / 开头", in, got)
		}
		for _, seg := range strings.Split(got, "/") {
			if seg == "." || seg == ".." {
				t.Errorf("SanitizeRel(%q) = %q：结果仍含 %q 段", in, got, seg)
			}
		}
	}
}
