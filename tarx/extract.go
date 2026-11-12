package tarx

// Extract 防御式解包（#326）：绝对路径与 `..` 穿越条目跳过并计数（点名
// 前三个），绝不写 root 之外——Zip Slip 是解包不可信归档时的共同威胁；
// 非常规条目（symlink/hardlink/设备/fifo）同样跳过（链接目标逃逸 root
// 的问题面整类消失）。

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Extract 从 tar 流解包到 root 目录，返回落地口径的 Stats。
//
// 防线（按遇序逐条目判定）：
//
//   - 条目名经 safeJoin 判定——绝对路径（/etc/passwd、Windows 盘符）、
//     `..` 穿越（../x、a/../../x）一律拒绝：Skipped 计数 + SkippedNames
//     点名（前 3），**检测并拒绝而非静默改写**（恶意条目必须可见地死掉，
//     审计面才不失真）；
//   - 目标目录**既有 symlink** 同样拒绝（#417）：root 之下的路径链任何一段
//     是 symlink（预置文件链接、预置目录链接）⇒ 该条目跳过计数——否则
//     `OpenFile`/`MkdirAll` 落地时内核沿链接解析，归档内容写穿 root 之外
//     （覆盖式升级/共享工作区把 symlink 种进目标目录是最常见形态），而
//     Stats 还报「全成功」——审计面同时失真。root 自身及更上层不参与判定
//     （macOS /tmp→/private/tmp 这类系统级链接是合法部署形态）；
//   - tar.Reader 自动弃置拒收条目的内容（流位置正确推进，恶意条目的
//     载荷不会落地也不会卡死后续条目）；
//   - 常规文件 create-or-truncate 写入（mode 取 header，umask 照常生效）；
//     父目录缺失时按需补建（合法流可能子先于父出现）；
//   - 目录条目 MkdirAll（`.` 条目视作 root 本身，不计数不报错）；
//   - 其余类型（symlink/hardlink/char/block/fifo/未知）跳过并计数点名。
//
// 错误语义：tar 流损坏/截断（含 Write 出错未写结束块的流）原样上抛——
// 已落地的条目保留（调用方按 staging 策略处置），Stats 反映死前落地量；
// root 不存在时自动创建。
//
// 已知边界：symlink 链检查（Lstat）与实际写入之间存在检查窗口——解包期间
// **并发修改 root 内容**的场景超出防线范围（威胁模型是「归档不可信」，不是
// 「目标目录同时被本地进程改写」）；每轮新建 staging 目录是既有建议。
func Extract(r io.Reader, root string) (Stats, error) {
	var st Stats
	root = filepath.Clean(root)
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return st, nil
		}
		if err != nil {
			return st, err
		}
		name := hdr.Name
		target, ok := safeJoin(root, name)
		if !ok {
			st.skip(name)
			continue
		}
		// 目标目录既有 symlink 链检查（#417）：root 之下任何一段是 symlink
		// 即拒收（文件与目录条目同判——MkdirAll 同样沿链接解析写穿）。
		if _, blocked := symlinkInChain(root, target); blocked {
			st.skip(name)
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if target == root {
				continue // root 本身的 `.` 条目：无需建、不计入
			}
			if err := os.MkdirAll(target, fs.FileMode(hdr.Mode)&os.ModePerm); err != nil {
				return st, err
			}
			st.Dirs++
		case tar.TypeReg:
			if target == root {
				st.skip(name) // 名字清洗后指向 root 本身的文件条目：怪异，拒收
				continue
			}
			if err := extractFile(tr, target, fs.FileMode(hdr.Mode)&os.ModePerm, &st); err != nil {
				return st, err
			}
		default:
			// symlink/hardlink/char/block/fifo/未知类型：一律拒收
			st.skip(name)
		}
	}
}

// symlinkInChain 报告 target 相对 root 的路径链上是否混入 symlink（#417），
// 返回命中的首段绝对路径。root 自身及更上层不参与（系统级链接是合法部署
// 形态）；root 之下任何一段是 symlink 即命中——归档内容会沿它写穿 root
// 之外。不存在的段安全（本条目将创建它），Lstat 错误按不存在处理。
func symlinkInChain(root, target string) (string, bool) {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." {
		return "", false
	}
	cur := root
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." || part == ".." {
			// ".." 不会出现：safeJoin 已保证 target 落在 root 内。
			continue
		}
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			continue // 不存在：安全
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return cur, true
		}
	}
	return "", false
}

// extractFile 落地单个常规文件：补父目录、create-or-truncate、按流长度
// 原样写完（tar.Reader 保证读满 header.Size 即 EOF，多出来的字节属于
// padding 不会混入）。
func extractFile(tr *tar.Reader, target string, perm fs.FileMode, st *Stats) error {
	if err := osEnsureDir(filepath.Dir(target)); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(f, tr)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("tarx: %s: 写入中止: %w", target, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("tarx: %s: 关闭失败: %w", target, closeErr)
	}
	st.Files++
	st.Bytes += n
	return nil
}
