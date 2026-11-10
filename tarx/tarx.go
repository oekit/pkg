package tarx

// 三件套共用的口径底座（#326 坑 2「口径漂移」的解法）：Scan 与 Write 走
// 同一个 walkRel、同一套分类分支——预扫数字与流式写入数字逐字段相等由
// 实现结构保证，奇偶测试（parity_test.go）再钉一层。

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// skippedNameCap SkippedNames 点名样本上限（前 3 个；全量名单是调用方日志的事）。
const skippedNameCap = 3

// Stats 三件套共用的口径计数。Scan 与 Write 对同一棵树的 Stats 逐字段相
// 等（奇偶测试随包交付）；Extract 的同型字段描述流内条目的落地情况。
type Stats struct {
	Files        int      // 常规文件数
	Dirs         int      // 目录数（不含 root 本身）
	Bytes        int64    // 常规文件内容字节合计
	Skipped      int      // 流会丢下/解包会拒收的条目数（非常规类型、穿越条目）
	SkippedNames []string // 前 3 个被跳过条目的点名样本（穿越条目/非常规类型混排，按遇序）
}

// skip 计一条跳过并留点名样本（封顶 skippedNameCap）。
func (s *Stats) skip(name string) {
	s.Skipped++
	if len(s.SkippedNames) < skippedNameCap {
		s.SkippedNames = append(s.SkippedNames, name)
	}
}

// classify 流式写入器的分类口径（单源）：常规文件 / 目录 / 其余跳过。
// symlink、设备、socket、fifo 一律归「流会丢下的」——符号链接不传输，
// 链接目标逃逸 root 的整类问题面随之消失（需要链接语义的调用方自行处理）。
func classify(d fs.DirEntry) (regular, dir bool) {
	return d.Type().IsRegular(), d.Type().IsDir()
}

// walkRel 以 root 为基准行走并回调（相对名 rel 恒为 `/` 分隔的斜杠路径，
// tar 名即此形态）。filepath.WalkDir 语义：字典序、不跟随目录符号链接、
// 行走错误原样上抛（含路径上下文）。Scan 与 Write 共用——同序同分类是
// 奇偶相等的前提。root 本身不回调。
func walkRel(root string, fn func(rel, path string, d fs.DirEntry) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return fn(filepath.ToSlash(rel), path, d)
	})
}

// countEntry 预扫/写出共用的计数分支：按分类口径累计 Stats，返回条目
// 形态（常规文件/目录；其余为跳过）。regular 需要 size 时由调用方取
// d.Info()（Scan 累 Bytes、Write 写 header）。
func countEntry(s *Stats, rel string, d fs.DirEntry) (regular, isDir bool, err error) {
	isRegular, isDirectory := classify(d)
	switch {
	case isRegular:
		s.Files++
		return true, false, nil
	case isDirectory:
		s.Dirs++
		return false, true, nil
	default:
		s.skip(rel)
		return false, false, nil
	}
}

// scanFileSize 预扫口径下的常规文件字节数（与 Write 写出的 header.Size/
// 内容长度同源——都取 FileInfo.Size）。
func scanFileSize(d fs.DirEntry) (int64, error) {
	fi, err := d.Info()
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// safeJoin 防御式解包的落点判定：归档条目名必须是 root 内的相对路径。
// 绝对路径、`..` 穿越（首段或中段逃出）、空名、以及清洗后落点越出 root
// 的名字一律拒绝（调用方跳过并计数）——**检测并拒绝**，不做静默改写：
// `../../etc/passwd` 被改写成 `etc/passwd` 落地是「消毒式」语义，恶意条
// 目必须可见地死掉，否则审计面失真。
func safeJoin(root, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	clean := filepath.Clean(name)
	if filepath.IsAbs(name) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	target := filepath.Join(root, clean)
	if !withinDir(root, target) {
		return "", false
	}
	return target, true
}

// withinDir target 是否落在 dir 内（含 dir 本身）。Clean 后按分隔符前缀
// 判定（双保险，覆盖 join 后仍越界的构造）。
func withinDir(dir, target string) bool {
	dir = filepath.Clean(dir)
	if target == dir {
		return true
	}
	return strings.HasPrefix(target, dir+string(filepath.Separator))
}

// osEnsureDir 递归建父目录（Extract 的条目可能先于其父目录出现）。
func osEnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
