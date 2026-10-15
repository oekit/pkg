package bufiox

import "io"

// BytesReader 基于一段既有字节切片的 Reader（只读，不修改 buf）。
// 适合解析已整体收包的帧。
type BytesReader struct {
	buf     []byte
	ri      int
	readLen int
}

// NewBytesReader 返回读取 buf[:len(buf)] 的 BytesReader。
func NewBytesReader(buf []byte) *BytesReader {
	return &BytesReader{buf: buf}
}

// Next 实现 Reader。
func (r *BytesReader) Next(n int) ([]byte, error) {
	if n < 0 {
		return nil, ErrNegativeCount
	}
	if n > len(r.buf)-r.ri {
		return nil, ErrNoRemainingData
	}
	p := r.buf[r.ri : r.ri+n]
	r.ri += n
	r.readLen += n
	return p, nil
}

// Peek 实现 Reader。
func (r *BytesReader) Peek(n int) ([]byte, error) {
	if n < 0 {
		return nil, ErrNegativeCount
	}
	if n > len(r.buf)-r.ri {
		return nil, ErrNoRemainingData
	}
	return r.buf[r.ri : r.ri+n], nil
}

// Skip 实现 Reader。
func (r *BytesReader) Skip(n int) error {
	if _, err := r.Next(n); err != nil {
		return err
	}
	return nil
}

// ReadBinary 实现 Reader（拷贝读）。
func (r *BytesReader) ReadBinary(p []byte) (int, error) {
	n := copy(p, r.buf[r.ri:])
	r.ri += n
	r.readLen += n
	switch {
	case n == len(p):
		return n, nil
	case n == 0:
		return 0, io.EOF
	default:
		return n, io.ErrUnexpectedEOF
	}
}

// ReadLen 实现 Reader。
func (r *BytesReader) ReadLen() int { return r.readLen }

// Release 实现 Reader：重置读位置与计数，buf 引用清空。
func (r *BytesReader) Release(error) error {
	r.buf = nil
	r.ri = 0
	r.readLen = 0
	return nil
}

// BytesWriter 基于可增长字节切片的 Writer（无底层写出目标）。
// 适合在内存中组装报文后整体取走。
type BytesWriter struct {
	buf        []byte
	writtenLen int
}

// NewBytesWriter 返回空 BytesWriter。可传初始 buf 预留容量。
func NewBytesWriter(buf []byte) *BytesWriter {
	return &BytesWriter{buf: buf}
}

// Malloc 实现 Writer：在 buf 尾部申请 n 字节零值空间并返回视图。
func (w *BytesWriter) Malloc(n int) ([]byte, error) {
	if n < 0 {
		return nil, ErrNegativeCount
	}
	off := len(w.buf)
	w.buf = append(w.buf, make([]byte, n)...)
	w.writtenLen += n
	return w.buf[off : off+n], nil
}

// WriteBinary 实现 Writer：拷贝追加 bs（内存组装场景零拷贝无意义）。
func (w *BytesWriter) WriteBinary(bs []byte) (int, error) {
	n := len(bs)
	w.buf = append(w.buf, bs...)
	w.writtenLen += n
	return n, nil
}

// WrittenLen 实现 Writer：自上次 Flush 起累计写入的字节数。
func (w *BytesWriter) WrittenLen() int { return w.writtenLen }

// Flush 实现 Writer：无底层写出目标，仅重置计数；已写内容保留在 buf。
func (w *BytesWriter) Flush() error {
	w.writtenLen = 0
	return nil
}

// Bytes 返回已组装的全部内容（视图，非拷贝）。
func (w *BytesWriter) Bytes() []byte { return w.buf }
