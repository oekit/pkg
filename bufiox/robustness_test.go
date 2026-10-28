package bufiox

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
)

// TestReadBinary_ReadLenCountsDirectPath 回归测试：ReadBinary 走「直读」路径
// （缺口 ≥ 4KiB）时，读到的字节必须计入 ReadLen。
//
// 缺陷成因：直读路径在 `return n, err` 之前**漏了 `r.rn += n`** ⇒ ReadLen
// 少算本次读到的全部字节。接口约定（bufiox.go）：「ReadLen 返回自上次 Release
// 以来累计消费的字节数（Next/ReadBinary/Skip 累加，Release 归零）」。
func TestReadBinary_ReadLenCountsDirectPath(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 8192)
	r := NewDefaultReader(bytes.NewReader(data))
	bs := make([]byte, 8192)
	n, err := r.ReadBinary(bs)
	if err != nil {
		t.Fatalf("ReadBinary: %v", err)
	}
	if n != 8192 {
		t.Fatalf("ReadBinary 读到 %d 字节，期望 8192", n)
	}
	if got := r.ReadLen(); got != 8192 {
		t.Fatalf("ReadLen = %d，期望 8192（直读路径漏记）", got)
	}
}

// TestReadBinary_ReadLenCountsPartialDirectPath 直读路径**部分读**（遇 EOF）
// 时，读到的字节同样要计入 ReadLen。
func TestReadBinary_ReadLenCountsPartialDirectPath(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 5000)
	r := NewDefaultReader(bytes.NewReader(data))
	bs := make([]byte, 8192)
	n, err := r.ReadBinary(bs)
	if n != 5000 {
		t.Fatalf("ReadBinary 读到 %d 字节，期望 5000", n)
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		t.Fatalf("期望 EOF 类错误，实得 %v", err)
	}
	if got := r.ReadLen(); got != 5000 {
		t.Fatalf("ReadLen = %d，期望 5000", got)
	}
}

// countingReader 统计底层 Read 被调用了多少次。
type countingReader struct {
	data  []byte
	pos   int
	calls int
}

func (c *countingReader) Read(p []byte) (int, error) {
	c.calls++
	if c.pos >= len(c.data) {
		return 0, io.EOF
	}
	n := copy(p, c.data[c.pos:])
	c.pos += n
	return n, nil
}

// TestDefaultReader_StickyTerminalError 回归测试：终态读错误一旦发生，后续
// 读操作直接返回它，**不再碰底层 reader**。
//
// 缺陷成因：rdErr 只写不读 ⇒ 粘滞语义形同虚设，每次操作都重新读底层。
// Release 的文档承诺（「终态读错误…下一次操作仍返回」）也没兑现。
func TestDefaultReader_StickyTerminalError(t *testing.T) {
	src := &countingReader{data: []byte("hello")}
	r := NewDefaultReader(src)

	// 读光数据并触发 EOF。
	buf := make([]byte, 5)
	if _, err := r.ReadBinary(buf); err != nil {
		t.Fatalf("ReadBinary: %v", err)
	}
	// 再读一次：触发 EOF（终态）。
	_, _ = r.Next(1)

	callsAfterTerminal := src.calls

	// 后续操作不得再碰底层。
	for i := 0; i < 3; i++ {
		if _, err := r.Next(1); err == nil {
			t.Fatal("终态后 Next 应返回错误")
		}
	}
	if src.calls != callsAfterTerminal {
		t.Fatalf("终态后仍读了底层 %d 次（粘滞语义未生效）", src.calls-callsAfterTerminal)
	}
}

// TestDefaultReader_ReleaseClearsTerminalError Release 后终态错误清掉——
// 下一个读取周期重新感知底层状态（与 bufio 的 Reset 同口径）。
func TestDefaultReader_ReleaseClearsTerminalError(t *testing.T) {
	src := &countingReader{data: []byte("hello")}
	r := NewDefaultReader(src)

	buf := make([]byte, 5)
	if _, err := r.ReadBinary(buf); err != nil {
		t.Fatalf("ReadBinary: %v", err)
	}
	if _, err := r.Next(1); err == nil {
		t.Fatal("应触发 EOF")
	}
	if err := r.Release(nil); err != nil {
		t.Fatalf("Release: %v", err)
	}
	// Release 后缓冲被清空、终态错误被清掉 ⇒ 再次读会重新碰底层（并再次得到 EOF）。
	callsBefore := src.calls
	if _, err := r.Next(1); err == nil {
		t.Fatal("Release 后仍应读到 EOF")
	}
	if src.calls == callsBefore {
		t.Fatal("Release 后应重新感知底层状态（再次读底层）")
	}
}

// TestDefaultReader_HugeRequestDoesNotHang 回归测试：极大的长度请求不得让
// 倍增循环溢出成死循环。
//
// 缺陷成因：acquire 按 `size *= 2` 倍增找容量，而 size 是 int：请求量超过
// 2^62 时倍增溢出成负数，`size < n` 永远成立 ⇒ **无限循环**（实测
// Next(1<<62+1) 永久挂起）。
func TestDefaultReader_HugeRequestDoesNotHang(t *testing.T) {
	r := NewDefaultReader(bytes.NewReader([]byte("x")))
	defer func() { _ = r.Release(nil) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = r.Next((1 << 62) + 1)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Next(1<<62+1) 无限挂起：倍增溢出成负数，循环永不退出")
	}
}

// TestDefaultReader_HugeRequestReturnsError 超出缓冲上限的请求应返回明确错误。
func TestDefaultReader_HugeRequestReturnsError(t *testing.T) {
	r := NewDefaultReader(bytes.NewReader([]byte("x")))
	defer func() { _ = r.Release(nil) }()

	if _, err := r.Next((1 << 62) + 1); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("期望 ErrTooLarge，实得 %v", err)
	}
}

// TestDefaultWriter_HugeRequestDoesNotHang 写入侧的同型问题：Malloc 的倍增
// 同样会溢出成死循环。
func TestDefaultWriter_HugeRequestDoesNotHang(t *testing.T) {
	w := NewDefaultWriter(io.Discard)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = w.Malloc((1 << 62) + 1)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Malloc(1<<62+1) 无限挂起：倍增溢出成负数")
	}
}
