package pathx

import "strings"

// SanitizeRel 清洗仓库相对路径：统一分隔符为 "/"、消除 "." 与 ".." 段，
// 绝对路径或越出根目录（".."）时 ok=false。
//
// 定位：仓库相对路径常来自外部输入（webhook 载荷里的文件路径），
// 清洗与 traversal 拒绝是同一件事的两面——消费方各写各的清洗逻辑时，
// 口径会漂移（实测同一仓内三档：宽松 / 严格 / 只看 Clean 后的结果）。
// 本原语给基线，调用方在基线上选严格档。
//
// 分隔符口径：**"/" 与 "\" 都当作分隔符**。仓库路径来自外部载荷时
// Windows 风格分隔符同样是路径的一部分；若按运行平台解释（Linux 上
// "\" 只是普通字符），同一段载荷在 Linux 与 Windows 上会得到不同结论——
// 而 Linux 才是绝大多数服务端环境，那正是 "..\..\" 形绕过最容易漏掉的地方。
//
// 判定口径：
//   - 绝对路径（"/foo"、UNC "//host/share"、Windows 盘符 "C:\foo"）→ ok=false；
//   - 越出根目录（"../x"、"/../x"、"a/../../x"）→ ok=false；
//   - 清洗后为空（"."、"a/.." —— 指向仓库根目录本身，非具体条目）→ ok=false；
//   - 空串 / 纯空白 → ok=false。
//
// ".." 段的处理：宽松档下 ".." 抵消前一段（"a/../b" → "b"），但抵消后
// 仍要留在根目录内（"a/../../b" 越界 → ok=false）。
//
// 用法:
//
//	cleaned, ok := pathx.SanitizeRel("src/./main.go")   // "src/main.go", true
//	cleaned, ok := pathx.SanitizeRel("../../etc/passwd") // "", false
func SanitizeRel(p string) (cleaned string, ok bool) {
	return sanitizeRel(p, false)
}

// SanitizeRelStrict 是 SanitizeRel 的严格档：除 SanitizeRel 的全部判定外，
// **输入中显式出现的 ".." 段一律拒绝**，即使它能被 Clean 抵消掉
// （"a/../b" 在宽松档得到 "b"，在严格档直接 ok=false）。
//
// 为什么需要这一档：宽松档的 ".." 抵消是纯文本语义，看不见符号链接。
// 路径先过一层符号链接再按相对路径解析时，文本上合法的 "a/../b" 可能
// 指向预期之外的位置——安全档宁可拒绝，也不做这层推断。
//
// 用法:
//
//	cleaned, ok := pathx.SanitizeRelStrict("a/../b") // "", false（宽松档是 "b"）
func SanitizeRelStrict(p string) (cleaned string, ok bool) {
	return sanitizeRel(p, true)
}

// sanitizeRel 是两档口径的共同实现；strict 决定 ".." 段是抵消还是直接拒绝。
func sanitizeRel(p string, strict bool) (string, bool) {
	if strings.TrimSpace(p) == "" {
		return "", false
	}
	norm := strings.ReplaceAll(p, "\\", "/")
	if strings.HasPrefix(norm, "/") {
		return "", false // 绝对路径 / UNC（//host/share）
	}
	segs := strings.Split(norm, "/")
	out := make([]string, 0, len(segs))
	for i, seg := range segs {
		switch seg {
		case "":
			continue // 重复分隔符（"a//b"）
		case ".":
			continue // 当前目录段（"a/./b"）
		case "..":
			if strict {
				return "", false // 严格档：显式 ".." 段一律拒绝
			}
			if len(out) == 0 {
				return "", false // 越出根目录
			}
			out = out[:len(out)-1] // 抵消前一段
			continue
		}
		if i == 0 && isWindowsDrive(seg) {
			return "", false // Windows 盘符绝对路径（"C:\foo"、"C:foo"）
		}
		out = append(out, seg)
	}
	if len(out) == 0 {
		return "", false // 清洗后为空（"." / "a/.." → 指向仓库根目录本身）
	}
	return strings.Join(out, "/"), true
}

// isWindowsDrive 判断路径首段是否带 Windows 盘符前缀（"C:" / "c:"）。
func isWindowsDrive(seg string) bool {
	if len(seg) < 2 || seg[1] != ':' {
		return false
	}
	c := seg[0]
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}
