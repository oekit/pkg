package tarx

// Write 流式写出（#326）：把 root 目录树写成一个标准 tar 流（不压缩，
// gzip 由调用方包一层）。可选在流尾夹带逐文件 sha256 manifest（WithManifest
// / VerifyManifest 自证闭环，格式与远端 sha256sum -c 兼容）。

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"
)

// WriteOption Write 的构造选项（仓规禁裸 Option）。
type WriteOption func(*writeConfig)

type writeConfig struct {
	manifest bool
	symlinks bool
	// manifestName 清单条目名（空 = 用 ManifestName 常量）。
	manifestName string
}

// WithManifest 流尾夹带逐文件 sha256 清单（ManifestName 条目，sha256sum
// 文本格式：`<hex>  <name>`）。清单覆盖源树全部常规文件；manifest 条目本
// 身是流协议开销，不计入 Stats。远端人工核对可直接 `sha256sum -c`，程序
// 化核对用 VerifyManifest。
func WithManifest() WriteOption {
	return func(cfg *writeConfig) { cfg.manifest = true }
}

// WithSymlinks 传输符号链接（写侧写 TypeSymlink 条目，承载 Linkname）。
//
// **默认不传输**：链接语义带来两个面——链接目标可能逃逸 root（归档里一个
// 指向 /etc/passwd 的链接，落地后就是一个随时可读的入口），以及链接本身
// 指向何处对接收方是隐式的（收件人拿到的是「一个链接」而不是「一份数据」）。
// 默认关闭 ⇒ 这两个面不出现。
//
// 开启后**必须配对开启解包侧的 WithSymlinks**（Extract 的同名选项）——
// 只写不收会让链接在流里存在、在落地时被跳过，两侧口径分裂。
//
// 开启后的安全承诺：链接目标的逃逸校验在**解包侧**做（Extract）——写侧
// 只负责如实记录「源树里这个链接指向哪」，不替接收方做判断。
func WithSymlinks() WriteOption {
	return func(cfg *writeConfig) { cfg.symlinks = true }
}

// WithManifestName 自定义清单条目名（与 WithManifest 配对使用）。
//
// **为什么需要**：默认名 ManifestName 是固定串，落在接收端的目录里可能与
// 用户自己的文件撞名（撞了 ⇒ 清单覆盖用户文件，或用户文件覆盖清单——
// 两种都让自证失去意义）。需要「绝不撞名」的调用方（如远端用
// `ls -1t <dir>/<前缀>*` 找清单的那种协议）可传一个带唯一后缀的名字。
//
// 名字必须是**单层**相对名（不含 `/`、不含 `..`）——它会被原样写进 tar
// 条目名，非法值在构造期就报错（写入期才发现会留下一半流）。
//
// 不传时用 ManifestName 常量（既有行为不变）。
func WithManifestName(name string) WriteOption {
	return func(cfg *writeConfig) { cfg.manifestName = name }
}

// Write 流式写出 root 目录树为 tar 流，返回与 Scan 同口径的 Stats。
//
// 行走为字典序（与 Scan 同序同分类，奇偶测试钉住）；常规文件写 TypeReg
// 条目（mode/mtime 取自文件系统，大小 = FileInfo.Size）、目录写 TypeDir
// 条目、其余类型（symlink/socket/设备/fifo）跳过并计入 Skipped。
//
// 错误语义：行走/读文件错误原样上抛（含路径上下文），此时**不写 tar 结
// 束块**——流不完整，接收端的 Extract 会以 tar 读错误告终，绝不静默交付
// 半棵树；root 不存在/不可读同理。空目录树（root 内无条目）是合法输入，
// 产出仅含结束块的空 tar。
func Write(w io.Writer, root string, opts ...WriteOption) (Stats, error) {
	var cfg writeConfig
	for _, opt := range opts {
		if opt == nil {
			panic("tarx: WriteOption 为 nil（fail-fast，仓规总则①）")
		}
		opt(&cfg)
	}

	var st Stats
	var manifest bytes.Buffer // WithManifest 时的逐文件清单（hex  <name>\n）
	tw := tar.NewWriter(w)

	// 清单条目名：自定义名优先，否则用常量。非法名在**写入任何内容前**
	// 判掉——半截流比没有流更坏（接收端会以为传输完成）。
	manifestName := cfg.manifestName
	if manifestName == "" {
		manifestName = ManifestName
	} else if !validManifestName(manifestName) {
		return st, fmt.Errorf("tarx: 清单条目名非法 %q（须是单层相对名：不含 / 与 ..）", manifestName)
	}

	err := walkRel(root, func(rel, path string, d fs.DirEntry) error {
		// symlink 优先于常规分类：开启链接语义时它是独立的一类，
		// 不走 countEntry 的「其余跳过」分支（那会把链接记成 Skipped）。
		if cfg.symlinks && isSymlink(d) {
			target, lerr := os.Readlink(path)
			if lerr != nil {
				return lerr
			}
			if werr := writeSymlinkEntry(tw, rel, target, d); werr != nil {
				return werr
			}
			st.Symlinks++
			return nil
		}
		regular, isDir := countEntry(&st, rel, d)
		if isDir {
			return writeDirEntry(tw, rel, d)
		}
		if !regular {
			return nil // 非常规条目：跳过（已由 countEntry 计数）
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		hashHex, err := writeFileEntry(tw, rel, path, fi, cfg.manifest)
		if err != nil {
			return err
		}
		st.Bytes += fi.Size()
		if cfg.manifest {
			line := hashHex + "  " + rel
			manifest.WriteString(line + "\n")
			st.ManifestLines = append(st.ManifestLines, line)
		}
		return nil
	})
	if err != nil {
		return st, err
	}

	if cfg.manifest {
		if err := writeManifestEntry(tw, manifest.Bytes(), manifestName); err != nil {
			return st, err
		}
	}
	if err := tw.Close(); err != nil {
		return st, err
	}
	return st, nil
}

// writeDirEntry 写单个目录条目（header，tar 约定名尾带 `/`）——目录条目
// 进流是 Extract 落地口径与 Scan/Write 逐字段相等的前提（空目录也能传输）。
func writeDirEntry(tw *tar.Writer, rel string, d fs.DirEntry) error {
	fi, err := d.Info()
	if err != nil {
		return err
	}
	return tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeDir,
		Name:     rel + "/",
		Mode:     int64(fi.Mode().Perm()),
		ModTime:  fi.ModTime(),
	})
}

// writeFileEntry 写单个常规文件条目（header + 内容）。内容写出与（可选的）
// sha256 累计同一遍 CopyN 完成；CopyN 封顶 header.Size——文件比遍历时变
// 大则截断（接收端有 manifest 核对兜底），变小则短读报错。内容写成功才
// 返回摘要，失败返回空串（半截文件绝不入清单）。
func writeFileEntry(tw *tar.Writer, rel, path string, fi fs.FileInfo, wantHash bool) (hashHex string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hdr := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     rel,
		Size:     fi.Size(),
		Mode:     int64(fi.Mode().Perm()),
		ModTime:  fi.ModTime(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return "", err
	}
	var sink io.Writer = tw
	var sum hash.Hash
	if wantHash {
		sum = sha256.New()
		sink = io.MultiWriter(tw, sum)
	}
	if _, err := io.CopyN(sink, f, fi.Size()); err != nil {
		return "", err
	}
	if sum == nil {
		return "", nil
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// writeSymlinkEntry 写单个符号链接条目（TypeSymlink，Linkname 承载目标）。
//
// 目标按源树里的**原样**记录（相对链接记相对、绝对链接记绝对）——写侧不
// 替接收方判断「这个链接能不能落在这儿」，那是解包侧的事（Extract 的
// 逃逸校验）。写侧只保证「如实记录」。
func writeSymlinkEntry(tw *tar.Writer, rel, target string, d fs.DirEntry) error {
	fi, err := d.Info()
	if err != nil {
		return err
	}
	return tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeSymlink,
		Name:     rel,
		Linkname: target,
		Mode:     int64(fi.Mode().Perm()),
		ModTime:  fi.ModTime(),
	})
}

// writeManifestEntry 流尾写 manifest 条目（常规文件，sha256sum 文本格式）。
func writeManifestEntry(tw *tar.Writer, data []byte, name string) error {
	hdr := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Size:     int64(len(data)),
		Mode:     0o644,
		ModTime:  time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// validManifestName 清单条目名是否合法：单层相对名（不含路径分隔符、不含
// .. 前缀、非空）。它会被原样写进 tar 条目名 ⇒ 含分隔符会变成多层级条目
// （落到子目录，远端按单层名找不到），含 .. 会是穿越条目。
func validManifestName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, "/\\\\") {
		return false
	}
	if strings.HasPrefix(name, "..") {
		return false
	}
	return true
}
