package pathx

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatchesPath(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		path     string
		want     bool
	}{
		// 标准 glob 匹配
		{"single pattern match", []string{"*.go"}, "main.go", true},
		{"single pattern no match", []string{"*.go"}, "main.js", false},
		{"extension pattern", []string{"*.test"}, "foo.test", true},

		// /** 目录前缀匹配
		{"globstar nested", []string{"vendor/**"}, "vendor/foo/bar.go", true},
		{"globstar direct child", []string{"node_modules/**"}, "node_modules/x", true},
		{"globstar no match", []string{"vendor/**"}, "src/main.go", false},

		// 多模式
		{"multiple patterns first matches", []string{"vendor/**", "*.test"}, "vendor/foo/bar.go", true},
		{"multiple patterns second matches", []string{"vendor/**", "*.test"}, "foo.test", true},
		{"multiple patterns none match", []string{"vendor/**", "*.test"}, "src/main.go", false},

		// 目录直接子项
		{"directory single child", []string{"test/*.txt"}, "test/a.txt", true},

		// 边界条件
		{"empty path", []string{"*.go"}, "", false},
		{"empty patterns", []string{}, "main.go", false},
		{"whitespace path", []string{"*.go"}, "  ", false},
		{"empty pattern skipped", []string{"", "*.go"}, "main.go", true},
		{"whitespace pattern skipped", []string{"   ", "*.go"}, "main.go", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesPath(tt.patterns, tt.path); got != tt.want {
				t.Errorf("MatchesPath(%v, %q) = %v, want %v", tt.patterns, tt.path, got, tt.want)
			}
		})
	}
}

// TestMatchesPath_InvalidPatternLenientFailOpen 钉住宽松档的口径声明
// （#252）：非法 pattern（未闭合字符类）静默按「不匹配」处理——返回值
// 与「不匹配」不可区分。这是无错误返回值签名下的刻意宽松，不是正确性
// 保障；拒绝名单场景必须走 MatchesPathStrict / ValidatePattern。
func TestMatchesPath_InvalidPatternLenientFailOpen(t *testing.T) {
	// issue 复现样本：非法 pattern "[abc" 对 "x"、"abc" 都返回 false，
	// 与合法 pattern 的不匹配形态返回值完全一样
	if MatchesPath([]string{"[abc"}, "x") {
		t.Error(`MatchesPath(["[abc"], "x") 应为 false（宽松档）`)
	}
	if MatchesPath([]string{"[abc"}, "abc") {
		t.Error(`MatchesPath(["[abc"], "abc") 应为 false（宽松档 fail-open 形态——本应匹配的路径也放行）`)
	}
	if !MatchesPath([]string{"*.go"}, "a.go") {
		t.Error(`MatchesPath(["*.go"], "a.go") 应为 true（对照组）`)
	}
}

// TestMatchesPathStrict 钉住严格档（#252）：非法 pattern 返回可判定错误
// （errors.Is ErrBadPattern，错误文本带出问题 pattern），合法 pattern 语义
// 与宽松档一致。
func TestMatchesPathStrict(t *testing.T) {
	for _, tt := range []struct {
		name     string
		patterns []string
		path     string
		want     bool
		wantErr  bool
	}{
		{"valid match", []string{"*.go"}, "a.go", true, false},
		{"valid no match", []string{"*.go"}, "a.js", false, false},
		{"globstar match", []string{"vendor/**"}, "vendor/foo/bar.go", true, false},
		{"empty pattern skipped", []string{"", "*.go"}, "main.go", true, false},
		{"empty path", []string{"*.go"}, "", false, false},
		{"unclosed class", []string{"[abc"}, "x", false, true},
		{"unclosed class would-match path", []string{"[abc"}, "abc", false, true},
		{"valid after invalid", []string{"[abc", "*.go"}, "a.go", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MatchesPathStrict(tt.patterns, tt.path)
			if got != tt.want {
				t.Errorf("MatchesPathStrict(%v, %q) = %v, want %v", tt.patterns, tt.path, got, tt.want)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if !errors.Is(err, filepath.ErrBadPattern) {
					t.Fatalf("错误应可 errors.Is(err, filepath.ErrBadPattern) 判定, got %v", err)
				}
				if !strings.Contains(err.Error(), tt.patterns[0]) {
					t.Fatalf("错误文本应带出问题 pattern %q, got %q", tt.patterns[0], err.Error())
				}
			}
		})
	}
}

// TestValidatePattern 钉住配置加载期预检原语（#252）：合法 nil / 非法可
// errors.Is 判定 / 空串合法（与匹配函数的跳过口径一致）。
func TestValidatePattern(t *testing.T) {
	for _, tt := range []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{"plain", "*.go", false},
		{"globstar", "vendor/**", false},
		{"class", "[a-z].go", false},
		{"empty", "", false},
		{"whitespace only", "  ", false},
		{"unclosed class", "[abc", true},
		{"unclosed class range", "[a-", true},
		{"trailing backslash", `a\`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePattern(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidatePattern(%q) err = %v, wantErr = %v", tt.pattern, err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, filepath.ErrBadPattern) {
				t.Fatalf("错误应可 errors.Is(err, filepath.ErrBadPattern) 判定, got %v", err)
			}
		})
	}
}
