package pathx

import "strings"

// IsSafeFileName 判断 name 是否可安全地作为「某目录下的单个文件名」使用。
//
// 与同族另两个原语是**三件不同的事**，别混用：
//
//   - SanitizeRel / SanitizeRelStrict 答的是「这条**路径**合法吗、越不越界」——
//     它答不了「单段文件名」这件事（它本身就允许多段相对路径）；
//   - SanitizeFileName 是**改写器**：回答「这名字能不能安全地显示和存储」，
//     目录成分一律丢弃、消毒后总有名字可给（没有 ok=false 这一说）；
//   - IsSafeFileName 是**判定器**：回答「这个名字能不能原样作为某个目录下的
//     单个文件名用」——不合规则直接说不合格，不改写。
//
// 为什么需要它：消费方把外部输入（上传名、归档名、bundle 名）拼进
// filepath.Join(dir, name) 时要的正是这个判定，而最自然的写法
// filepath.Base(name) != name 是**错的**——filepath.Base("..") == ".."、
// filepath.Base(".") == "."，两个都会被放行，而 name 随后被 Join 拼进路径，
// ".." 把路径指到 dir 的父目录。
//
// 判定口径（与 SanitizeRelStrict 严格档一致，外加「必须是单段」这一条）：
//
//   - 不含路径分隔符：**"/" 与 "\" 都算分隔符**（按运行平台解释会让同一段载荷
//     在 Linux 与 Windows 上得到不同结论，而 Linux 才是绝大多数服务端环境，
//     那正是双点反斜杠形绕过最容易漏掉的地方）；
//   - 不是 "." / ".." 段、不是绝对路径、不是 Windows 盘符（"C:\foo"、"C:foo"）；
//   - **fail-closed**：首两字符形如盘符的名字（"C:foo.bundle"）在 Windows 上是
//     盘符相对路径，同样拒绝——宁可拒，不可让同一段输入在不同平台上得到不同结论。
//
// 用法:
//
//	pathx.IsSafeFileName("report.txt")  // true
//	pathx.IsSafeFileName("a/b.txt")     // false（含分隔符，不是单段）
//	pathx.IsSafeFileName("..")          // false（filepath.Base 放行的那两个）
//	pathx.IsSafeFileName("C:foo")       // false（Windows 盘符相对路径）
func IsSafeFileName(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) {
		return false
	}
	// 其余判定（"." / ".." 段、绝对路径、盘符、UNC）与路径清洗的严格档同源，
	// 避免两处各写一套口径再次漂移。
	_, ok := SanitizeRelStrict(name)
	return ok
}
