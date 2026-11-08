// Package pathx 提供文件路径匹配工具。
package pathx

import (
	"fmt"
	"path/filepath"
	"strings"
)

// MatchesPath 检查路径是否匹配任意一个 glob 模式。
// 支持标准 filepath.Match 模式以及 /** 目录前缀匹配。
//
// ⚠️ 语法非法的 pattern（如未闭合字符类 "[abc"）被静默按「不匹配」处理
// （#252）——这是刻意的宽松口径：本函数无错误返回值，语义必须自洽。
// 典型的拒绝名单用法（`if MatchesPath(deny, p) { return errDenied }`）下，
// 配置写错一个 `[` 整条规则静默失效＝安全面 fail-open 且无日志线索。
// 配置可校验/失败要可见的场景请用 MatchesPathStrict（错误可传播）或
// ValidatePattern（配置加载期 fail-fast）。
//
// 模式示例:
//   - "*.go"          → 匹配根目录下的 .go 文件
//   - "vendor/*"      → 匹配 vendor 目录下的直接子项
//   - "node_modules/**" → 匹配 node_modules 下所有层级的文件
//   - "test/*.txt"    → 匹配 test 目录下的 .txt 文件
//
// 用法:
//
//	pathx.MatchesPath([]string{"vendor/**", "*.test"}, "vendor/foo/bar.go") // true
//	pathx.MatchesPath([]string{"*.go"}, "main.go")                          // true
func MatchesPath(patterns []string, path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return false
	}

	for _, pattern := range patterns {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		// 标准 glob 匹配（ErrBadPattern 吞掉——见函数注释的宽松口径声明）
		if ok, _ := filepath.Match(pattern, path); ok {
			return true
		}
		// /** 目录前缀匹配: "node_modules/**" 匹配 "node_modules/foo/bar"
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "**")
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}
	}
	return false
}

// MatchesPathStrict 是 MatchesPath 的严格档：任一 pattern 语法非法时返回
// 可 errors.Is(err, filepath.ErrBadPattern) 判定的错误（错误文本带出问题
// 的 pattern），不做「静默按不匹配」的宽松处理（#252）。拒绝名单等安全
// 判据建议用它——匹配失败与判据失效必须可区分，否则配置错误就是 fail-open。
//
// 空串 pattern 与 MatchesPath 同口径：跳过（不算错误）。
func MatchesPathStrict(patterns []string, path string) (bool, error) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return false, nil
	}

	for _, pattern := range patterns {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		ok, err := filepath.Match(pattern, path)
		if err != nil {
			return false, fmt.Errorf("pathx: 非法 glob pattern %q: %w", pattern, err)
		}
		if ok {
			return true, nil
		}
		// /** 目录前缀匹配（与 MatchesPath 同构）
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "**")
			if strings.HasPrefix(path, prefix) {
				return true, nil
			}
		}
	}
	return false, nil
}

// ValidatePattern 校验单个 glob pattern 的语法（#252）：非法时返回可
// errors.Is(err, filepath.ErrBadPattern) 判定的错误，供配置加载期
// fail-fast——拒绝名单在装载时就拒绝坏 pattern，而不是请求路径上静默放行。
// 空串合法（MatchesPath/MatchesPathStrict 对空串都是跳过）。
func ValidatePattern(pattern string) error {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" {
		return nil
	}
	// name 取任意非空占位即可：语法错误与 name 无关，filepath.Match 对
	// 非法 pattern 无论 name 是什么都返回 ErrBadPattern。
	if _, err := filepath.Match(pattern, "x"); err != nil {
		return fmt.Errorf("pathx: 非法 glob pattern %q: %w", pattern, err)
	}
	return nil
}
