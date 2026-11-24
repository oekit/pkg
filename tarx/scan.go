package tarx

// Scan 同口径预扫（#326）：按流式写入器（Write）的分类口径本地行走 root
// ——Files 只数常规文件、Bytes 只算内容、Skipped 是流会丢下的（symlink/
// socket/设备/fifo），给演练答复与流截止时间放大当底数。

import "io/fs"

// ScanOption Scan 的构造选项（仓规禁裸 Option）。
type ScanOption func(*scanConfig)

type scanConfig struct {
	symlinks bool
}

// WithScanSymlinks 预扫时把符号链接计入「会传输」（与 Write 的 WithSymlinks
// 配对）。
//
// **为什么需要**：预扫的承诺是「这次传输会带多少东西」——若写侧开了链接
// 传输而预扫仍把链接算成「流会丢下的」，两个数字就对不上了（演练说 3 个
// 文件，真跑下来多出 2 个链接，调用方按预扫做的分片决策就错了）。
//
// 不开时链接计入 Skipped（既有行为不变）。
func WithScanSymlinks() ScanOption {
	return func(cfg *scanConfig) { cfg.symlinks = true }
}

// Scan 预扫 root 目录树，返回与 Write 同口径的 Stats。
//
// root 不存在/不可读时返回行走错误（filepath.WalkDir 原样上抛，含路径上
// 下文）。Scan 不打开文件内容——Bytes 取 FileInfo.Size（与 Write 的
// header.Size 同源同值）；两次 Scan 之间树内容变化（写入/删除）是调用方
// 的并发前置条件，本包不做快照。
//
// 选项与 Write 对称：开启链接语义时（WithScanSymlinks + Write 的
// WithSymlinks）两端口径一致——预扫说链接会传，写侧就真传。
func Scan(root string, opts ...ScanOption) (Stats, error) {
	var cfg scanConfig
	for _, opt := range opts {
		if opt == nil {
			panic("tarx: ScanOption 为 nil（fail-fast，仓规总则①）")
		}
		opt(&cfg)
	}
	var st Stats
	err := walkRel(root, func(rel, path string, d fs.DirEntry) error {
		if cfg.symlinks && isSymlink(d) {
			st.Symlinks++
			return nil
		}
		regular, _ := countEntry(&st, rel, d)
		if regular {
			size, err := scanFileSize(d)
			if err != nil {
				return err
			}
			st.Bytes += size
		}
		return nil
	})
	return st, err
}
