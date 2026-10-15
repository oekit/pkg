package bufiox

import (
	"io"
	"net"
)

const (
	defaultBufSize           = 8 * 1024
	nocopyWriteThreshold     = 4 * 1024
	directlyReadThreshold    = 4 * 1024
	skipBufSize              = 64 * 1024
	maxConsecutiveEmptyReads = 100
)

// DefaultReader 基于任意 io.Reader 的缓冲 Reader。
// Next/Peek 返回内部缓冲的零拷贝视图；视图在 Release 之前始终有效
// （内部缓冲扩容时旧缓冲整体保活，不会原地搬移破坏已返回的视图）。
type DefaultReader struct {
	rd  io.Reader
	buf []byte // buf[ri:] 未消费数据
	ri  int
	// ref 标记已返回零拷贝视图：扩容丢弃旧缓冲前需保活进 toFree
	ref    bool
	toFree [][]byte

	rn      int // 自上次 Release 起累计消费字节数
	bufSize int // 最小缓冲尺寸

	rdErr error // 终态错误（读错误/io.EOF），粘滞到下次 Release
}

// NewDefaultReader 返回从 rd 读取的 DefaultReader（默认 8KiB 起步缓冲）。
func NewDefaultReader(rd io.Reader) *DefaultReader {
	return NewDefaultReaderSize(rd, defaultBufSize)
}

// NewDefaultReaderSize 返回从 rd 读取、缓冲至少 size 字节的 DefaultReader。
func NewDefaultReaderSize(rd io.Reader, size int) *DefaultReader {
	if size < defaultBufSize {
		size = defaultBufSize
	}
	return &DefaultReader{rd: rd, bufSize: size}
}

// Buffered 返回缓冲中尚未消费的字节数。
func (r *DefaultReader) Buffered() int { return len(r.buf) - r.ri }

// acquire 保证缓冲内未消费数据不少于 n 字节；空间不足时换新缓冲
// （剩余数据搬移，旧缓冲若仍被视图引用则保活到 Release）。
func (r *DefaultReader) acquire(n int) error {
	if n > cap(r.buf)-r.ri {
		size := r.bufSize
		for ; size < n; size *= 2 {
		}
		buf := make([]byte, 0, size)
		if remaining := r.Buffered(); remaining > 0 {
			copy(buf, r.buf[r.ri:])
			buf = buf[:remaining]
		}
		if cap(r.buf) > 0 {
			if r.ref {
				// 旧缓冲里有已返回的视图，保活到 Release 统一丢引用
				r.toFree = append(r.toFree, r.buf)
			}
		}
		r.buf = buf
		r.ri = 0
		r.ref = false
	}

	need := n - r.Buffered()
	if need <= 0 {
		return nil
	}
	nl, err := readAtLeast(r.rd, r.buf[len(r.buf):cap(r.buf)], need)
	r.buf = r.buf[:len(r.buf)+nl]
	r.rdErr = err
	return err
}

// readAtLeast 从 rd 读取至多填满 buf、至少 min 字节。
// 读满清除 EOF（下一次调用再感知）；部分读遇 EOF 转为 io.ErrUnexpectedEOF；
// 连续空读超过阈值判 io.ErrNoProgress（防挂死 reader 空转）。
func readAtLeast(rd io.Reader, buf []byte, min int) (n int, err error) {
	if len(buf) < min {
		return 0, io.ErrShortBuffer
	}
	emptyRead := 0
	for n < min && err == nil {
		var nn int
		nn, err = rd.Read(buf[n:])
		n += nn
		if nn > 0 {
			emptyRead = 0
			continue
		}
		emptyRead++
		if emptyRead > maxConsecutiveEmptyReads {
			return n, io.ErrNoProgress
		}
	}
	switch {
	case n >= min:
		err = nil
	case n > 0 && err == io.EOF:
		err = io.ErrUnexpectedEOF
	}
	return
}

// Next 实现 Reader。
func (r *DefaultReader) Next(n int) ([]byte, error) {
	if n < 0 {
		return nil, ErrNegativeCount
	}
	if n > r.Buffered() {
		if err := r.acquire(n); err != nil {
			return nil, wrapInsufficient(err)
		}
	}
	p := r.buf[r.ri : r.ri+n : r.ri+n]
	r.ri += n
	r.rn += n
	if n > 0 {
		r.ref = true
	}
	return p, nil
}

// Peek 实现 Reader；数据不足时尽量返回缓冲中已有的部分视图 + 错误。
func (r *DefaultReader) Peek(n int) ([]byte, error) {
	if n < 0 {
		return nil, ErrNegativeCount
	}
	if n > r.Buffered() {
		if err := r.acquire(n); err != nil {
			end := len(r.buf)
			buf := r.buf[r.ri:end:end]
			if len(buf) > 0 {
				r.ref = true
			}
			return buf, wrapInsufficient(err)
		}
	}
	buf := r.buf[r.ri : r.ri+n : r.ri+n]
	if n > 0 {
		r.ref = true
	}
	return buf, nil
}

// wrapInsufficient 流耗尽类错误统一为 ErrNoRemainingData，真实读错误原样透出。
func wrapInsufficient(err error) error {
	if err == nil {
		return nil
	}
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return ErrNoRemainingData
	}
	return err
}

// Skip 实现 Reader：先消费缓冲，不足部分用小缓冲直接从底层丢弃。
func (r *DefaultReader) Skip(n int) error {
	if n < 0 {
		return ErrNegativeCount
	}
	if bufn := r.Buffered(); n > bufn {
		r.ri += bufn
		r.rn += bufn
		n -= bufn
		if !r.ref {
			r.buf = nil
			r.ri = 0
		}
		nn, err := skipReader(r.rd, n)
		r.rn += nn
		r.rdErr = err
		return wrapInsufficient(err)
	}
	r.ri += n
	r.rn += n
	return nil
}

// skipReader 用临时小缓冲从 rd 精确读取并丢弃 n 字节。
func skipReader(rd io.Reader, n int) (skipped int, err error) {
	buf := make([]byte, skipBufSize)
	emptyRead := 0
	for skipped < n {
		sz := n - skipped
		if sz > skipBufSize {
			sz = skipBufSize
		}
		var nn int
		nn, err = rd.Read(buf[:sz])
		skipped += nn
		if nn > 0 {
			emptyRead = 0
		} else {
			emptyRead++
			if emptyRead > maxConsecutiveEmptyReads {
				return skipped, io.ErrNoProgress
			}
		}
		if err != nil {
			break
		}
	}
	switch {
	case skipped >= n:
		err = nil
	case skipped > 0 && err == io.EOF:
		err = io.ErrUnexpectedEOF
	}
	return
}

// ReadLen 实现 Reader。
func (r *DefaultReader) ReadLen() int { return r.rn }

// ReadBinary 实现 Reader：先拷贝缓冲存量，缺口大（≥ 4KiB）时直接读入
// 调用方切片省一次中转拷贝，小缺口走 acquire 缓冲后拷贝。
func (r *DefaultReader) ReadBinary(bs []byte) (int, error) {
	if len(bs) == 0 {
		return 0, nil
	}
	n := copy(bs, r.buf[r.ri:])
	r.ri += n
	if need := len(bs) - n; need > 0 {
		if need >= directlyReadThreshold {
			nn, err := readAtLeast(r.rd, bs[n:], need)
			r.rdErr = err
			n += nn
			return n, err
		}
		if err := r.acquire(need); err != nil {
			m := copy(bs[n:], r.buf[r.ri:])
			r.ri += m
			n += m
			r.rn += n
			return n, err
		}
		m := copy(bs[n:], r.buf[r.ri:])
		r.ri += m
		n += m
	}
	r.rn += n
	return n, nil
}

// Read 实现 io.Reader：缓冲有存量时只返回存量（不等满），语义区别于 ReadBinary。
func (r *DefaultReader) Read(bs []byte) (n int, err error) {
	if len(bs) == 0 {
		return 0, nil
	}
	n = copy(bs, r.buf[r.ri:])
	if n > 0 {
		r.ri += n
		r.rn += n
		return n, nil
	}
	if len(bs) >= directlyReadThreshold {
		n, r.rdErr = r.rd.Read(bs)
		return n, r.rdErr
	}
	if err := r.acquire(1); err != nil {
		return 0, err
	}
	n = copy(bs, r.buf[r.ri:])
	r.ri += n
	r.rn += n
	return n, nil
}

// Release 实现 Reader：丢掉保活引用与缓冲，重置读计数与 ref；
// 终态读错误不重置（下一次操作仍返回，粘滞语义与上游一致）。
func (r *DefaultReader) Release(error) error {
	for i := range r.toFree {
		r.toFree[i] = nil
	}
	r.toFree = r.toFree[:0]
	if r.Buffered() == 0 {
		r.buf = nil
		r.ri = 0
	}
	r.ref = false
	r.rn = 0
	return nil
}

var _ Writer = &DefaultWriter{}

// DefaultWriter 基于任意 io.Writer 的缓冲 Writer：Malloc 段与零拷贝引用段
// 按调用顺序攒批，Flush 经 net.Buffers 写出（对 net.Conn 自动走 writev）。
type DefaultWriter struct {
	chunk  []byte      // 当前 Malloc 目标缓冲
	chunks net.Buffers // 攒批段（malloc 段 + 零拷贝引用段），按序写出

	wl  int // 自上次 Flush 起累计写入字节数
	wd  io.Writer
	err error // 粘滞错误：一旦写出失败，后续操作原样返回
}

// NewDefaultWriter 返回写入 wd 的 DefaultWriter。
func NewDefaultWriter(wd io.Writer) *DefaultWriter {
	return &DefaultWriter{wd: wd}
}

func (w *DefaultWriter) acquire(n int) {
	if len(w.chunk)+n <= cap(w.chunk) {
		return
	}
	if len(w.chunk) > 0 {
		w.chunks = append(w.chunks, w.chunk)
		w.chunk = nil
	}
	ncap := defaultBufSize
	for ; ncap < n; ncap *= 2 {
	}
	w.chunk = make([]byte, 0, ncap)
}

// writeDirect 把大块 bs 作为零拷贝段直接入队。
func (w *DefaultWriter) writeDirect(bs []byte) {
	if len(w.chunk) > 0 {
		w.chunks = append(w.chunks, w.chunk)
		w.chunk = nil
	}
	w.chunks = append(w.chunks, bs)
}

// Malloc 实现 Writer。
func (w *DefaultWriter) Malloc(n int) ([]byte, error) {
	if w.err != nil {
		return nil, w.err
	}
	if n < 0 {
		return nil, ErrNegativeCount
	}
	w.acquire(n)
	off := len(w.chunk)
	w.chunk = w.chunk[:off+n]
	w.wl += n
	return w.chunk[off : off+n], nil
}

// WriteBinary 实现 Writer：≥ 4KiB 零拷贝引用，小切片拷入当前 chunk 攒批。
func (w *DefaultWriter) WriteBinary(bs []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if len(bs) >= nocopyWriteThreshold {
		w.writeDirect(bs)
		w.wl += len(bs)
		return len(bs), nil
	}
	w.acquire(len(bs))
	n := copy(w.chunk[len(w.chunk):cap(w.chunk)], bs)
	w.chunk = w.chunk[:len(w.chunk)+n]
	w.wl += len(bs)
	return n, nil
}

// WrittenLen 实现 Writer。
func (w *DefaultWriter) WrittenLen() int { return w.wl }

// Flush 实现 Writer：net.Buffers.WriteTo 对 net.Conn 走 writev，普通
// io.Writer 逐段写。写出失败错误粘滞，后续 Malloc/WriteBinary/Flush 直接返回。
func (w *DefaultWriter) Flush() error {
	if w.err != nil {
		return w.err
	}
	if len(w.chunk) > 0 {
		w.chunks = append(w.chunks, w.chunk)
		w.chunk = nil
	}
	if len(w.chunks) == 0 {
		return nil
	}
	_, err := w.chunks.WriteTo(w.wd)
	if err != nil {
		w.err = err
	}
	w.chunk = nil
	for i := range w.chunks {
		w.chunks[i] = nil
	}
	w.chunks = w.chunks[:0]
	w.wl = 0
	return err
}
