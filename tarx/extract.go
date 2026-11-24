package tarx

// Extract 防御式解包（#326）：绝对路径与 `..` 穿越条目跳过并计数（点名
// 前三个），绝不写 root 之外——Zip Slip 是解包不可信归档时的共同威胁；
// 非常规条目（symlink/hardlink/设备/fifo）同样跳过（链接目标逃逸 root
// 的问题面整类消失）。

import (
	"archive/tar"
	stderrors "errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/oekit/pkg/errors"
)

// ExtractOption Extract 的构造选项（仓规禁裸 Option）。
type ExtractOption func(*extractConfig)

type extractConfig struct {
	symlinks bool
}

// WithSymlinkExtract 落地符号链接条目（TypeSymlink）。
//
// **默认不落地**：符号链接默认跳过并计数（见 Extract 的防线清单）。开启后
// 除「链接目标逃逸 root」的判定外，其余防线（条目名穿越、既有 symlink 链、
// 非常规类型）一条不减。
//
// 逃逸判定（开启链接语义后新增的面）：链接目标按**相对链接解析到 root 内**
// 的口径校验——绝对路径目标与解析后落在 root 之外的相对目标一律拒收
// （Skipped 计数 + 点名）。理由：归档里的一个指向 /etc/passwd 的链接落地后
// 就是「一个随时可读系统文件的入口」，而收件人拿到的是链接不是数据。
//
// ⚠️ 开启后**配对开启写侧的 WithSymlinks**——只收不写没有意义，只写不收
// 会让链接在流里存在、落地时被跳过，两侧口径分裂。
func WithSymlinkExtract() ExtractOption {
	return func(cfg *extractConfig) { cfg.symlinks = true }
}

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
//   - 符号链接条目默认跳过并计数点名；**开启 WithSymlinks 后**按链接目标
//     的逃逸校验判定（见 WithSymlinks 的说明）；
//   - 其余类型（hardlink/char/block/fifo/未知）跳过并计数点名。
//
// 错误语义：tar 流损坏/截断（含 Write 出错未写结束块的流）原样上抛——
// 已落地的条目保留（调用方按 staging 策略处置），Stats 反映死前落地量；
// root 不存在时自动创建。
//
// 已知边界：symlink 链检查（Lstat）与实际写入之间存在检查窗口——解包期间
// **并发修改 root 内容**的场景超出防线范围（威胁模型是「归档不可信」，不是
// 「目标目录同时被本地进程改写」）；每轮新建 staging 目录是既有建议。
func Extract(r io.Reader, root string, opts ...ExtractOption) (Stats, error) {
	var cfg extractConfig
	for _, opt := range opts {
		if opt == nil {
			panic("tarx: ExtractOption 为 nil（fail-fast，仓规总则①）")
		}
		opt(&cfg)
	}
	var st Stats
	root = filepath.Clean(root)
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if stderrors.Is(err, io.EOF) {
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
		case tar.TypeSymlink:
			// 符号链接：默认拒收；开启 WithSymlinks 后按链接目标逃逸判定。
			if !cfg.symlinks {
				st.skip(name)
				continue
			}
			if !linkTargetWithinRoot(root, target, hdr.Linkname) {
				st.skip(name)
				continue
			}
			if err := extractSymlink(target, hdr.Linkname); err != nil {
				return st, err
			}
			st.Symlinks++
		default:
			// hardlink/char/block/fifo/未知类型：一律拒收
			st.skip(name)
		}
	}
}

// linkTargetWithinRoot 判定「落在 target 处的符号链接、指向 linkname」
// 是否安全——链接目标不得逃逸 root。
//
// 口径：链接目标按**相对链接语义**解析（相对链接相对于链接自身所在目录
// target 的父目录；绝对链接按绝对路径判）。
//
//   - 绝对路径目标（/etc/passwd）⇒ 一律逃逸 ⇒ 拒收。理由：落地后就是
//     一个随时可读系统文件的入口，而收件人拿到的是链接不是数据；
//   - 相对目标解析后落在 root 之外（../../etc/passwd）⇒ 逃逸 ⇒ 拒收；
//   - 相对目标解析后落在 root 之内 ⇒ 放行（含「指向尚不存在的路径」——
//     悬空链接是合法形态，不因目标缺失而拒收）。
//
// ⚠️ 只判**词法**逃逸，不解析目标路径里的中间 symlink（那需要遍历 root
// 之外的文件系统，超出「归档不可信」的威胁模型——不可信的是归档内容，
// 不是目标机上的文件系统）。
func linkTargetWithinRoot(root, target, linkname string) bool {
	if linkname == "" {
		return false
	}
	if filepath.IsAbs(linkname) {
		return false // 绝对目标一律逃逸
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(target), linkname))
	return withinDir(root, resolved)
}

// extractSymlink 落地单个符号链接（父目录按需补建；已存在的同名条目先移除
// ——覆盖式升级场景下目标处可能残留旧文件或旧链接，直接建会失败）。
//
// 用 os.Symlink（不走 Lstat 解析）——创建时若父目录里已有 symlink，内核会
// 沿它解析，可能写到 root 之外。父目录的 symlink 检查由 symlinkInChain 在
// 条目层做（同一道防线，位置在写入之前）。
func extractSymlink(target, linkname string) error {
	if err := osEnsureDir(filepath.Dir(target)); err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(linkname, target)
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
		return errors.Wrapf(copyErr, "tarx: %s: 写入中止", target)
	}
	if closeErr != nil {
		return errors.Wrapf(closeErr, "tarx: %s: 关闭失败", target)
	}
	st.Files++
	st.Bytes += n
	return nil
}
