package pathx

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// 文件名消毒的缺省口径。
const (
	// defaultMaxFileNameLength 是文件名长度上限的缺省值（**字节**）。
	//
	// 取 255 而不是字符数：ext4 / xfs / APFS / NTFS 的**单个目录项**上限
	// 都落在 255（ext4/xfs 是 255 **字节**，APFS/NTFS 是 255 个字符），
	// 按字节卡是最保守的公共下限——按字符数卡，在中文文件名下会超出 ext4 的
	// 255 字节上限（255 个中文字符 = 765 字节），落盘时 ENAMETOOLONG。
	defaultMaxFileNameLength = 255

	// defaultFileNameFallback 是空名 / 纯符号名的缺省回退名。
	defaultFileNameFallback = "file"
)

// fileNameConfig 是 SanitizeFileName 的配置态。
type fileNameConfig struct {
	maxLength int    // 长度上限（字节）；<=0 表示不限
	fallback  string // 空名 / 纯符号名的回退名
}

// FileNameOption 是 SanitizeFileName 的可选配置。
//
// 命名带 FileName 前缀而非裸 Option：本包与 pathx 其它导出符号同处一个
// import 组，裸 Option 会与 co-import 的其它包的 Option 撞名（矩阵硬规则）。
type FileNameOption func(*fileNameConfig)

// WithMaxFileNameLength 设置文件名长度上限，单位**字节**。
//
// n <= 0 表示**不限长**（与矩阵内并发闸门「cap<=0 视为不限」的既有口径一致）。
//
// 为什么按字节而不是字符数：文件系统按字节卡（ext4 单目录项 255 字节），
// 按字符数卡会在多字节文件名下超限落盘失败。截断本身按 rune 边界回落，
// 不会产出半个 UTF-8 字符。
func WithMaxFileNameLength(n int) FileNameOption {
	return func(c *fileNameConfig) { c.maxLength = n }
}

// WithFileNameFallback 设置空名 / 纯符号名的回退名。
//
// 回退名**原样返回**（不参与消毒、不受长度上限约束）——它是调用方给的
// 常量，不是外部输入，原样透传可预期。
func WithFileNameFallback(s string) FileNameOption {
	return func(c *fileNameConfig) { c.fallback = s }
}

// SanitizeFileName 消毒**展示 / 存储用**的文件名。
//
// 与 SanitizeRel 是同族但**语义相反**的两个原语，不要混用：
//
//   - SanitizeRel 管**路径**（仓库相对路径）：回答「这个路径合法吗、越不越界」，
//     越界与绝对路径一律**拒绝**（返回 ok=false）。
//   - SanitizeFileName 管**文件名**（展示名 / 存储名 / 上传原始名）：回答
//     「这个名字能不能安全地显示和存储」，目录成分一律**丢弃**（只取最后一段），
//     不存在 ok=false 这一说——消毒后总有名字可给（空则回退）。
//
// 同一段输入在两者下的结论必然不同，这是刻意的：
//
//	pathx.SanitizeRel("C:\\Users\\me\\报告.txt")     // "", false（Windows 盘符绝对路径 → 拒绝）
//	pathx.SanitizeFileName("C:\\Users\\me\\报告.txt") // "报告.txt"（文件名只要最后一段）
//
// 消毒规则（顺序即口径）：
//
//  1. **剥掉目录成分**：取最后一段，"/" 与 "\" 都算分隔符（跨平台同判）。
//  2. **丢弃控制字符**：r < 0x20 或 r == 0x7f。
//  3. **Windows 保留字符**（`: * ? " < > |`）替换为下划线 `_`。
//  4. **去首尾空白与结尾的点**：结尾的点与空白在 Windows 上是目录项陷阱
//     （"file." 与 "file " 会指向 "file"），首部空白无此含义故只去空白。
//  5. **空名 / 纯符号名回退**：清洗后为空，或通篇没有字母与数字（"***" → "___"）
//     ⇒ 回退为 fallback（缺省 "file"）。纯符号名落盘无碍但对展示无意义，
//     直接回退比留一串下划线干净。
//  6. **超长截断**：按**字节**上限截断，且**保留扩展名**；截断点回落到
//     rune 边界，不会产出半个 UTF-8 字符。
//
// 非 ASCII 字符（中日韩等）一律保留——展示名与存储名的核心诉求就是别把
// 中文名弄丢。非法 UTF-8 字节替换为 U+FFFD，保证输出是合法 UTF-8。
//
// 用法:
//
//	pathx.SanitizeFileName("a/b/c.txt")               // "c.txt"
//	pathx.SanitizeFileName(`C:\Users\me\报告.txt`)      // "报告.txt"
//	pathx.SanitizeFileName("a:b*c?.txt")               // "a_b_c_.txt"
//	pathx.SanitizeFileName("...")                      // "file"（纯符号名回退）
//	pathx.SanitizeFileName("报告.txt",
//		pathx.WithMaxFileNameLength(20))                // "报告.txt"（按字节卡）
func SanitizeFileName(name string, opts ...FileNameOption) string {
	cfg := fileNameConfig{maxLength: defaultMaxFileNameLength, fallback: defaultFileNameFallback}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	cleaned := trimFileName(sanitizeFileNameChars(lastPathSegment(name)))
	if !hasFileNameContent(cleaned) {
		return cfg.fallback
	}

	limited := trimFileName(limitFileNameLength(cleaned, cfg.maxLength))
	if !hasFileNameContent(limited) {
		return cfg.fallback
	}
	return limited
}

// lastPathSegment 取路径的最后一段（目录成分全部丢弃）。
//
// "/" 与 "\" 同为分隔符——上传文件名常来自 Windows 客户端或浏览器，
// 只认 "/" 会让 "C:\Users\me\报告.txt" 原样落进存储名。
func lastPathSegment(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		return name[i+1:]
	}
	return name
}

// sanitizeFileNameChars 逐字符消毒：丢控制字符、保留字符换下划线。
func sanitizeFileNameChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r < 0x20 || r == 0x7f:
			// 控制字符：直接丢弃（换行、回车、NUL 等进不了文件名）
		case isWindowsReservedFileNameChar(r):
			b.WriteRune('_')
		default:
			// 含非法 UTF-8 字节时 range 会给出 U+FFFD，原样写出即完成替换，
			// 顺带保证输出是合法 UTF-8（后续按 rune 边界截断才成立）。
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isWindowsReservedFileNameChar 判断是否 Windows 文件名保留字符。
//
// 这七个字符在 Windows 上不可出现在文件名里；替换为 "_" 而不是丢弃，
// 是为了保留可读性与扩展名结构（"a:b.txt" → "a_b.txt" 而非 "a.txt"）。
func isWindowsReservedFileNameChar(r rune) bool {
	switch r {
	case ':', '*', '?', '"', '<', '>', '|':
		return true
	}
	return false
}

// trimFileName 去首部空白、尾部空白与尾部的点。
//
// 尾部的点与空白在 Windows 上会被文件系统吃掉（"file." 落盘成 "file"），
// 是目录项陷阱；首部只去空白——前导点是合法名字的一部分（".gitignore"）。
func trimFileName(s string) string {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	s = strings.TrimRightFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || r == '.'
	})
	return s
}

// hasFileNameContent 判断名字里是否有实质内容（字母或数字）。
//
// 空名与纯符号名（"***"、"___"、"..."）对展示没有意义，一律回退；
// 有字母或数字（含中日韩等非 ASCII 字母）即视为有内容。
func hasFileNameContent(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// limitFileNameLength 按字节上限截断并保留扩展名。
//
// 截断优先砍**主名**、保留扩展名——"报告.pdf" 截成 "报.pdf" 仍可辨认，
// 截成 "报告.p" 就不是那个文件了。扩展名自己都装不下时**丢弃扩展名**、
// 只截主名：留半个扩展名（"a.tx"）比没有扩展名更糟——扩展名是骗人的，
// 下游按它判类型会判错。
func limitFileNameLength(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	ext := fileNameExt(s)
	stem := s[:len(s)-len(ext)]
	if stem == "" {
		// 整个名字就是扩展名（如 ".verylongname"）：主名无处可砍，整体截断。
		return truncateFileNameBytes(s, maxBytes)
	}
	budget := maxBytes - len(ext)
	if budget <= 0 {
		// 扩展名自己就超限：保不住就丢掉，整体按主名截断。
		return truncateFileNameBytes(stem, maxBytes)
	}
	if cut := truncateFileNameBytes(stem, budget); cut != "" {
		return cut + ext
	}
	// 预算连一个完整 rune 都放不下 ⇒ **丢弃扩展名**、只留按总预算截断的主名。
	// 此前是「整体截断 s」——那会砍进扩展名（"中.txt" + 上限 5 字节 → "中.t"），
	// 而半个扩展名比没有扩展名更糟：扩展名是骗人的，下游按它判类型会判错
	// （本函数自己的文档就写着这条）。
	return truncateFileNameBytes(stem, maxBytes)
}

// fileNameExt 取扩展名（含点号），无扩展名返回空串。
//
// 与 filepath.Ext 同语义，但**不依赖运行平台**：filepath.Ext 会遇平台
// 分隔符停下，而本函数只处理已剥离目录成分的名字，不含分隔符——
// 自己取最后一个点，避免 Windows / Linux 上同名字得到不同结论。
func fileNameExt(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		return s[i:]
	}
	return ""
}

// truncateFileNameBytes 按字节上限截断，且回落到完整 rune 边界。
//
// 不会产出半个 UTF-8 字符——多字节字符被截掉一半时整个字符丢弃，
// 而不是留下一个非法字节。
func truncateFileNameBytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	b := s[:maxBytes]
	for len(b) > 0 {
		r, size := utf8.DecodeLastRuneInString(b)
		if r == utf8.RuneError && size == 1 {
			// 尾部是不完整的 rune（或非法字节）：整块丢掉。
			b = b[:len(b)-1]
			continue
		}
		break
	}
	return b
}
