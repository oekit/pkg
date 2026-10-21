// Package pathx 提供文件路径匹配与清洗工具。
//
// MatchesPath 支持标准 glob 模式和 /** 目录前缀匹配，
// 适用于 .gitignore 风格的路径过滤场景（代码审查忽略路径、构建排除等）。
//
// SanitizeRel / SanitizeRelStrict 提供仓库相对路径的清洗与 traversal 拒绝：
// 路径来自外部输入（webhook 载荷）时，清洗逻辑每次都要重写且各处口径容易
// 漂移（实测同一仓内三档：宽松 / 严格 / 只看 Clean 后的结果）——本包给基线，
// 严格档额外拒绝显式 ".." 段。
//
// 用法:
//
//	pathx.MatchesPath([]string{"vendor/**", "*.test"}, "vendor/foo/bar.go") // true
//	pathx.MatchesPath([]string{"*.go"}, "main.go")                          // true
//
//	cleaned, ok := pathx.SanitizeRel("src/./main.go")   // "src/main.go", true
//	cleaned, ok := pathx.SanitizeRel("../../etc/passwd") // "", false
package pathx
