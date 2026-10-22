// Package pathx 提供文件路径匹配与清洗工具。
//
// MatchesPath 支持标准 glob 模式和 /** 目录前缀匹配，
// 适用于 .gitignore 风格的路径过滤场景（代码审查忽略路径、构建排除等）。
//
// MatchGlob 是**名字**（而非路径段）的通配匹配：'*' 可跨 '/'，'?' 匹配一个
// rune。与 MatchesPath 是两个语义各归其位的原语——分支名、ref 名、仓库全名
// 这类对象本身含 '/'，用路径段语义答不了（"*" 匹配不到 "owner/repo"，
// 「排除所有」这类配置会静默失效）。
//
// SanitizeRel / SanitizeRelStrict 提供仓库相对路径的清洗与 traversal 拒绝：
// 路径来自外部输入（webhook 载荷）时，清洗逻辑每次都要重写且各处口径容易
// 漂移（实测同一仓内三档：宽松 / 严格 / 只看 Clean 后的结果）——本包给基线，
// 严格档额外拒绝显式 ".." 段。
//
// SanitizeFileName 提供**文件名**（展示名 / 存储名 / 上传原始名）的消毒：
// 与 SanitizeRel 是同族但语义相反的两个原语——SanitizeRel 问「这路径合法吗」
// （越界即拒绝），SanitizeFileName 问「这名字能不能安全显示和存储」
// （目录成分丢弃、控制字符丢弃、Windows 保留字符替换、超长按字节截断并保扩展名）。
//
// IsSafeFileName 是**判定器**（不合规则说不合格，不改写）：这个名字能不能原样
// 作为某目录下的单个文件名用。它与 SanitizeFileName 语义相反——后者是改写器，
// 拿它做回环校验会把「本来就不合法」与「被消毒规则改了」混为一谈。
//
// 用法:
//
//	pathx.MatchesPath([]string{"vendor/**", "*.test"}, "vendor/foo/bar.go") // true
//	pathx.MatchesPath([]string{"*.go"}, "main.go")                          // true
//
//	pathx.MatchGlob("feature/*", "feature/x/y") // true（'*' 跨 '/'）
//	pathx.MatchGlob("*", "owner/repo")          // true
//
//	cleaned, ok := pathx.SanitizeRel("src/./main.go")   // "src/main.go", true
//	cleaned, ok := pathx.SanitizeRel("../../etc/passwd") // "", false
//
//	pathx.SanitizeFileName("a/b/c.txt")          // "c.txt"
//	pathx.SanitizeFileName(`C:\Users\me\报告.txt`) // "报告.txt"
//
//	pathx.IsSafeFileName("report.txt") // true
//	pathx.IsSafeFileName("..")         // false
package pathx
