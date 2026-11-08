// Package pathx 提供文件路径匹配、清洗与 URL path 低基数模板化工具。
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
// ClassifyPath / PathClassifier 把 URL path 折叠成低基数模板（ID 段折叠）：
// 按 path 切 metrics 标签时，path 里的数字主键 / commit SHA 段会让标签基数
// 爆炸——每个对象一条时间序列。缺省启发式折叠纯数字段与 hex≥7 段，repo 名
// 等有界词段保留（正是运维要切的维度）；UUID 形态、保留清单、占位符风格
// （"{id}" / ":id"）、补充判定经 NewPathClassifier 可配。与上面三个原语
// 语义不同——它们问「匹配吗 / 合法吗」（面向文件系统），Classify 问
// 「哪些段是高基数 ID」（折叠改写，面向标签与模板）。
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
//
//	pathx.ClassifyPath("/repos/octocat/hello/pulls/42/files")
//	// /repos/octocat/hello/pulls/{id}/files
package pathx
