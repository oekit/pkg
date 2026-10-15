package bufiox

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

var (
	_ Reader    = &BytesReader{}
	_ Reader    = &DefaultReader{}
	_ Writer    = &BytesWriter{}
	_ Writer    = &DefaultWriter{}
	_ io.Reader = &DefaultReader{}
)

func TestBytesReader_Next(t *testing.T) {
	r := NewBytesReader([]byte("hello world"))
	p, err := r.Next(5)
	if err != nil || string(p) != "hello" {
		t.Fatalf("Next(5) = (%q, %v)", p, err)
	}
	// 视图指向底层，未 Release 前稳定
	if string(r.buf[r.ri-5:r.ri]) != "hello" {
		t.Fatal("Next 推进异常")
	}
	if _, err := r.Next(6); err != nil {
		t.Fatalf("Next(6): %v", err)
	}
	if _, err := r.Next(1); err != ErrNoRemainingData {
		t.Fatalf("耗尽后 Next err = %v, want ErrNoRemainingData", err)
	}
	if _, err := r.Next(-1); err != ErrNegativeCount {
		t.Fatalf("Next(-1) err = %v", err)
	}
	if r.ReadLen() != 11 {
		t.Fatalf("ReadLen = %d, want 11", r.ReadLen())
	}
}

func TestBytesReader_PeekSkipReadBinary(t *testing.T) {
	r := NewBytesReader([]byte("abcdef"))
	p, err := r.Peek(3)
	if err != nil || string(p) != "abc" {
		t.Fatalf("Peek(3) = (%q, %v)", p, err)
	}
	p2, _ := r.Peek(3)
	if &p[0] != &p2[0] {
		t.Fatal("两次 Peek 应返回同一视图")
	}
	// Peek 不推进，Next 应从头消费
	if _, err := r.Next(2); err != nil || string([]byte("ab")) != "ab" {
		t.Fatalf("Next after Peek: %v", err)
	}
	if err := r.Skip(1); err != nil {
		t.Fatalf("Skip(1): %v", err)
	}
	bs := make([]byte, 3)
	if n, err := r.ReadBinary(bs); n != 3 || err != nil || string(bs) != "def" {
		t.Fatalf("ReadBinary = (%d, %v, %q)", n, err, bs)
	}
	if n, err := r.ReadBinary(bs); n != 0 || err != io.EOF {
		t.Fatalf("耗尽后 ReadBinary = (%d, %v), want (0, io.EOF)", n, err)
	}
}

func TestBytesReader_ReadBinaryPartial(t *testing.T) {
	r := NewBytesReader([]byte("ab"))
	bs := make([]byte, 5)
	n, err := r.ReadBinary(bs)
	if n != 2 || err != io.ErrUnexpectedEOF {
		t.Fatalf("部分读 = (%d, %v), want (2, io.ErrUnexpectedEOF)", n, err)
	}
}

func TestBytesReader_Release(t *testing.T) {
	r := NewBytesReader([]byte("abc"))
	_, _ = r.Next(2)
	if err := r.Release(nil); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if r.ReadLen() != 0 {
		t.Fatalf("Release 后 ReadLen = %d, want 0", r.ReadLen())
	}
}

func TestBytesWriter_Basic(t *testing.T) {
	w := NewBytesWriter(nil)
	m, err := w.Malloc(4)
	if err != nil {
		t.Fatalf("Malloc: %v", err)
	}
	copy(m, "abcd")
	if _, err := w.WriteBinary([]byte("efg")); err != nil {
		t.Fatalf("WriteBinary: %v", err)
	}
	if w.WrittenLen() != 7 {
		t.Fatalf("WrittenLen = %d, want 7", w.WrittenLen())
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if w.WrittenLen() != 0 {
		t.Fatalf("Flush 后 WrittenLen = %d, want 0", w.WrittenLen())
	}
	if string(w.Bytes()) != "abcdefg" {
		t.Fatalf("Bytes = %q", w.Bytes())
	}
	if _, err := w.Malloc(-1); err != ErrNegativeCount {
		t.Fatalf("Malloc(-1) err = %v", err)
	}
}

// TestDefaultReader_ZeroCopyViewStability 关键回归：Next 返回的视图在
// Release 前必须稳定——中途 acquire 换缓冲不得破坏旧视图（旧缓冲保活）。
func TestDefaultReader_ZeroCopyViewStability(t *testing.T) {
	payload := strings.Repeat("x", 100) + "MARKER" + strings.Repeat("y", 200)
	r := NewDefaultReaderSize(bytes.NewReader([]byte(payload)), 8)
	defer r.Release(nil)

	p1, err := r.Next(100) // 耗尽首个小缓冲，触发扩容换缓冲
	if err != nil || string(p1) != strings.Repeat("x", 100) {
		t.Fatalf("Next(100) = (len %d, %v)", len(p1), err)
	}
	// 再触发多次扩容
	p2, err := r.Next(6)
	if err != nil || string(p2) != "MARKER" {
		t.Fatalf("Next(6) = (%q, %v)", p2, err)
	}
	// 旧视图不被后续 acquire 破坏
	if string(p1) != strings.Repeat("x", 100) {
		t.Fatal("扩容后旧视图被破坏")
	}
	if string(p2) != "MARKER" {
		t.Fatal("第二次视图被破坏")
	}
}

func TestDefaultReader_NextPeekSkip(t *testing.T) {
	r := NewDefaultReader(bytes.NewReader([]byte("0123456789")))
	defer r.Release(nil)

	p, err := r.Peek(4)
	if err != nil || string(p) != "0123" {
		t.Fatalf("Peek(4) = (%q, %v)", p, err)
	}
	if err := r.Skip(2); err != nil {
		t.Fatalf("Skip(2): %v", err)
	}
	p, err = r.Next(3)
	if err != nil || string(p) != "234" {
		t.Fatalf("Next(3) = (%q, %v)", p, err)
	}
	if _, err := r.Next(100); err != ErrNoRemainingData {
		t.Fatalf("超量 Next err = %v, want ErrNoRemainingData", err)
	}
	if r.ReadLen() != 5 {
		t.Fatalf("ReadLen = %d, want 5", r.ReadLen())
	}
}

// TestDefaultReader_PeekPartial 跨缓冲 Peek：请求量超过底层单次供给时能攒齐。
func TestDefaultReader_PeekPartial(t *testing.T) {
	data := []byte("abcdefghij")
	r := NewDefaultReaderSize(bytes.NewReader(data), 4)
	defer r.Release(nil)
	p, err := r.Peek(10)
	if err != nil || string(p) != string(data) {
		t.Fatalf("Peek(10) = (len %d, %v)", len(p), err)
	}
}

// TestDefaultReader_PeekEOFWithData 数据不足时 Peek 返回存量视图 + 错误。
func TestDefaultReader_PeekEOFWithData(t *testing.T) {
	r := NewDefaultReader(bytes.NewReader([]byte("ab")))
	defer r.Release(nil)
	p, err := r.Peek(5)
	if err != ErrNoRemainingData {
		t.Fatalf("Peek(5) err = %v, want ErrNoRemainingData", err)
	}
	if string(p) != "ab" {
		t.Fatalf("Peek(5) 应带出存量视图, got %q", p)
	}
}

func TestDefaultReader_ReadBinary(t *testing.T) {
	r := NewDefaultReaderSize(bytes.NewReader([]byte("0123456789abcdef")), 4)
	defer r.Release(nil)
	bs := make([]byte, 10)
	if n, err := r.ReadBinary(bs); n != 10 || err != nil {
		t.Fatalf("ReadBinary = (%d, %v)", n, err)
	}
	if string(bs) != "0123456789" {
		t.Fatalf("ReadBinary 内容 = %q", bs)
	}
	// Release 后拷贝读的内容仍有效（视图语义不适用）
	_ = r.Release(nil)
	if string(bs) != "0123456789" {
		t.Fatal("ReadBinary 结果在 Release 后失效")
	}
}

// TestDefaultReader_ReadBinaryLarge 大块直接读路径（≥ directlyReadThreshold）。
func TestDefaultReader_ReadBinaryLarge(t *testing.T) {
	data := bytes.Repeat([]byte{0xAB}, 10*1024)
	r := NewDefaultReaderSize(bytes.NewReader(data), 64)
	defer r.Release(nil)
	bs := make([]byte, len(data))
	if n, err := r.ReadBinary(bs); n != len(bs) || err != nil {
		t.Fatalf("大块 ReadBinary = (%d, %v)", n, err)
	}
	if !bytes.Equal(bs, data) {
		t.Fatal("大块 ReadBinary 内容不一致")
	}
}

func TestDefaultReader_SkipLarge(t *testing.T) {
	data := bytes.Repeat([]byte{0x01}, 200*1024)
	r := NewDefaultReader(bytes.NewReader(data))
	defer r.Release(nil)
	if err := r.Skip(150 * 1024); err != nil {
		t.Fatalf("Skip 大块: %v", err)
	}
	bs := make([]byte, 4)
	if n, err := r.ReadBinary(bs); n != 4 || err != nil {
		t.Fatalf("Skip 后 ReadBinary = (%d, %v)", n, err)
	}
}

func TestDefaultReader_ReadSemantics(t *testing.T) {
	r := NewDefaultReaderSize(bytes.NewReader([]byte("abcdef")), 4)
	defer r.Release(nil)
	bs := make([]byte, 3)
	if n, err := r.Read(bs); n != 3 || err != nil {
		t.Fatalf("Read = (%d, %v)", n, err)
	}
	if string(bs) != "abc" {
		t.Fatalf("Read 内容 = %q", bs)
	}
}

func TestDefaultReader_ReleaseReset(t *testing.T) {
	r := NewDefaultReader(bytes.NewReader([]byte("abc")))
	_, _ = r.Next(2)
	_ = r.Release(nil)
	// 上游语义：未消费完的缓冲保留（可继续读），只有耗尽才清空
	if r.ReadLen() != 0 || r.Buffered() != 1 {
		t.Fatalf("Release 后状态: ReadLen=%d Buffered=%d, want (0,1)", r.ReadLen(), r.Buffered())
	}
	// 复用：先读掉剩余存量
	p, err := r.Next(1)
	if err != nil || string(p) != "c" {
		t.Fatalf("复用 Next(1) = (%q, %v)", p, err)
	}
	_ = r.Release(nil)
	if r.Buffered() != 0 {
		t.Fatalf("耗尽后 Release 未清空: Buffered=%d", r.Buffered())
	}
}

// TestDefaultReader_StickyError 读错误粘滞：一旦发生，后续操作持续返回。
func TestDefaultReader_StickyError(t *testing.T) {
	boom := errors.New("boom")
	r := NewDefaultReader(io.MultiReader(bytes.NewReader([]byte("ab")), errReader{boom}))
	_ = r.Release(nil)
	bs := make([]byte, 10)
	if _, err := r.ReadBinary(bs); err != boom {
		t.Fatalf("ReadBinary err = %v, want boom", err)
	}
	if _, err := r.Next(1); err != boom {
		t.Fatalf("粘滞错误丢失, got %v", err)
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// TestDefaultReader_SlowReader 逐字节供给的 reader（验证 readAtLeast 攒齐逻辑）。
func TestDefaultReader_SlowReader(t *testing.T) {
	r := NewDefaultReader(iotestOneByte([]byte("hello")))
	defer r.Release(nil)
	p, err := r.Next(5)
	if err != nil || string(p) != "hello" {
		t.Fatalf("Next(5) = (%q, %v)", p, err)
	}
}

func iotestOneByte(data []byte) io.Reader {
	return &oneByteReader{data: data}
}

type oneByteReader struct{ data []byte }

func (o *oneByteReader) Read(p []byte) (int, error) {
	if len(o.data) == 0 {
		return 0, io.EOF
	}
	p[0] = o.data[0]
	o.data = o.data[1:]
	return 1, nil
}

func TestDefaultWriter_Basic(t *testing.T) {
	var out bytes.Buffer
	w := NewDefaultWriter(&out)

	m, err := w.Malloc(5)
	if err != nil {
		t.Fatalf("Malloc: %v", err)
	}
	copy(m, "hello")
	n, err := w.WriteBinary([]byte(" world"))
	if err != nil || n != 6 {
		t.Fatalf("WriteBinary = (%d, %v)", n, err)
	}
	if w.WrittenLen() != 11 {
		t.Fatalf("WrittenLen = %d, want 11", w.WrittenLen())
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if out.String() != "hello world" {
		t.Fatalf("写出内容 = %q", out.String())
	}
	if w.WrittenLen() != 0 {
		t.Fatal("Flush 后 WrittenLen 未归零")
	}
}

// TestDefaultWriter_ZeroCopyLarge 大切片零拷贝引用 + 顺序保持。
func TestDefaultWriter_ZeroCopyLarge(t *testing.T) {
	var out bytes.Buffer
	w := NewDefaultWriter(&out)

	big := bytes.Repeat([]byte("B"), 8*1024) // ≥ nocopyWriteThreshold
	if _, err := w.WriteBinary(big); err != nil {
		t.Fatalf("WriteBinary big: %v", err)
	}
	if _, err := w.WriteBinary([]byte("tail")); err != nil {
		t.Fatalf("WriteBinary small: %v", err)
	}
	m, _ := w.Malloc(3)
	copy(m, "END")
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	want := string(big) + "tailEND"
	if out.String() != want {
		t.Fatalf("写出长度 = %d, want %d（或内容不一致）", out.Len(), len(want))
	}
}

// TestDefaultWriter_OrderAcrossMalloc 混用 Malloc/WriteBinary 时顺序严格保持。
func TestDefaultWriter_OrderAcrossMalloc(t *testing.T) {
	var out bytes.Buffer
	w := NewDefaultWriter(&out)

	m1, _ := w.Malloc(2)
	copy(m1, "AA")
	_, _ = w.WriteBinary([]byte("bb"))
	m2, _ := w.Malloc(2)
	copy(m2, "CC")
	_, _ = w.WriteBinary([]byte("dd"))
	// 先改 m1 再 Flush：Malloc 返回的视图在 Flush 前填入有效
	copy(m1, "XX")
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if out.String() != "XXbbCCdd" {
		t.Fatalf("顺序错误: %q", out.String())
	}
}

func TestDefaultWriter_MultipleFlush(t *testing.T) {
	var out bytes.Buffer
	w := NewDefaultWriter(&out)
	for i := 0; i < 3; i++ {
		if _, err := w.WriteBinary([]byte("x")); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
		if err := w.Flush(); err != nil {
			t.Fatalf("round %d flush: %v", i, err)
		}
	}
	if out.String() != "xxx" {
		t.Fatalf("多次 Flush 内容 = %q", out.String())
	}
}

func TestDefaultWriter_StickyError(t *testing.T) {
	boom := errors.New("write boom")
	w := NewDefaultWriter(errWriter{boom})
	_, _ = w.WriteBinary([]byte("data"))
	if err := w.Flush(); err != boom {
		t.Fatalf("Flush err = %v, want boom", err)
	}
	if _, err := w.Malloc(1); err != boom {
		t.Fatalf("粘滞错误丢失, got %v", err)
	}
	if _, err := w.WriteBinary([]byte("x")); err != boom {
		t.Fatalf("粘滞错误丢失, got %v", err)
	}
	if err := w.Flush(); err != boom {
		t.Fatalf("粘滞 Flush err = %v", err)
	}
}

type errWriter struct{ err error }

func (e errWriter) Write(p []byte) (int, error) { return 0, e.err }

// TestDefaultWriter_FlushEmpty 空.Flush 不出错且不动底层。
func TestDefaultWriter_FlushEmpty(t *testing.T) {
	var out bytes.Buffer
	w := NewDefaultWriter(&out)
	if err := w.Flush(); err != nil || out.Len() != 0 {
		t.Fatalf("空 Flush = (%v, len %d)", err, out.Len())
	}
}
