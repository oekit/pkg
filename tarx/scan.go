package tarx

// Scan 同口径预扫（#326）：按流式写入器（Write）的分类口径本地行走 root
// ——Files 只数常规文件、Bytes 只算内容、Skipped 是流会丢下的（symlink/
// socket/设备/fifo），给演练答复与流截止时间放大当底数。

import "io/fs"

// Scan 预扫 root 目录树，返回与 Write 同口径的 Stats。
//
// root 不存在/不可读时返回行走错误（filepath.WalkDir 原样上抛，含路径上
// 下文）。Scan 不打开文件内容——Bytes 取 FileInfo.Size（与 Write 的
// header.Size 同源同值）；两次 Scan 之间树内容变化（写入/删除）是调用方
// 的并发前置条件，本包不做快照。
func Scan(root string) (Stats, error) {
	var st Stats
	err := walkRel(root, func(rel, path string, d fs.DirEntry) error {
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
