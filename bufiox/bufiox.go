package bufiox

import "errors"

var (
	// ErrNegativeCount 负数长度参数。
	ErrNegativeCount = errors.New("bufiox: 负数长度")
	// ErrNoRemainingData Next/Peek/Skip 请求的数据量超过剩余可用数据。
	ErrNoRemainingData = errors.New("bufiox: 剩余数据不足")
	// ErrTooLarge 请求的长度超出单个缓冲的容量上限。
	ErrTooLarge = errors.New("bufiox: 请求长度超出缓冲上限")
)

// Reader 是缓冲读接口：用户态零拷贝读，降低内存分配与拷贝开销。
type Reader interface {
	// Next 顺序读出接下来 n 字节，返回长度为 n 的视图 p；
	// 数据不足时返回错误。p 可能是底层缓冲的浅拷贝视图，
	// 调用方在 Release 之前不得修改、之后不得继续使用 p。
	Next(n int) (p []byte, err error)

	// ReadBinary 读取至多 len(p) 字节填满 p，返回读到的字节数。
	// 拷贝读：p 在 Release 之后仍然有效。
	// 语义：读满返回 (len(p), nil)；部分读返回 (n, io.ErrUnexpectedEOF)；
	// 无数据可读返回 (0, io.EOF)。
	ReadBinary(p []byte) (n int, err error)

	// Peek 行为同 Next，但不推进读位置（随后可 ReadBinary/Skip 消费）。
	// 返回视图的生命周期约束同 Next。
	Peek(n int) (buf []byte, err error)

	// Skip 跳过接下来 n 字节；数据不足时返回错误。
	Skip(n int) (err error)

	// ReadLen 返回自上次 Release 以来累计消费的字节数
	// （Next/ReadBinary/Skip 累加，Release 归零）。
	ReadLen() (n int)

	// Release 释放缓冲并重置状态；此后 Next/Peek 返回的视图全部失效。
	// 参数 e 供"缓冲释放依赖错误结果"的场景使用（如连接错误触发整段丢弃）。
	// Release 不关闭底层连接，只归还用户态缓冲。
	Release(e error) (err error)
}

// Writer 是缓冲写接口：用户态攒批 + 一次性 Flush，支持零拷贝写。
type Writer interface {
	// Malloc 申请 n 字节写缓冲并返回视图，调用方填入数据；
	// Flush 之前对返回 buf 的写入都是暂存的，Flush 后不得再写入。
	Malloc(n int) (buf []byte, err error)

	// WriteBinary 追加 bs；可能是零拷贝写（Flush 之前不得修改 bs）。
	// 写入不足 len(bs) 时返回错误，n 为已写入字节数。
	WriteBinary(bs []byte) (n int, err error)

	// WrittenLen 返回自上次 Flush 以来累计写入的字节数
	// （Malloc/WriteBinary 累加，Flush 归零）。
	WrittenLen() (length int)

	// Flush 把攒批的数据写到底层目标，并重置 WrittenLen。
	Flush() (err error)
}
