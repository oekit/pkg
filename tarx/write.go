package tarx

// Write 流式写出（#326）：把 root 目录树写成一个标准 tar 流（不压缩，
// gzip 由调用方包一层）。可选在流尾夹带逐文件 sha256 manifest（WithManifest
// / VerifyManifest 自证闭环，格式与远端 sha256sum -c 兼容）。

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"io/fs"
	"os"
	"time"
)

// WriteOption Write 的构造选项（仓规禁裸 Option）。
type WriteOption func(*writeConfig)

type writeConfig struct {
	manifest bool
}

// WithManifest 流尾夹带逐文件 sha256 清单（ManifestName 条目，sha256sum
// 文本格式：`<hex>  <name>`）。清单覆盖源树全部常规文件；manifest 条目本
// 身是流协议开销，不计入 Stats。远端人工核对可直接 `sha256sum -c`，程序
// 化核对用 VerifyManifest。
func WithManifest() WriteOption {
	return func(cfg *writeConfig) { cfg.manifest = true }
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

	err := walkRel(root, func(rel, path string, d fs.DirEntry) error {
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
			manifest.WriteString(hashHex + "  " + rel + "\n")
		}
		return nil
	})
	if err != nil {
		return st, err
	}

	if cfg.manifest {
		if err := writeManifestEntry(tw, manifest.Bytes()); err != nil {
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

// writeManifestEntry 流尾写 manifest 条目（常规文件，sha256sum 文本格式）。
func writeManifestEntry(tw *tar.Writer, data []byte) error {
	hdr := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     ManifestName,
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
