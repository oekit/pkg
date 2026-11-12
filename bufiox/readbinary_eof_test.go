package bufiox

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// #406 回归：ReadBinary 的部分读必须返回 io.ErrUnexpectedEOF（接口契约），
// 无数据可读返回 io.EOF。此前「部分读撞上流终态」的错误取决于剩余字节此前
// 是否已在内部缓冲里——acquire 路径与直读粘滞路径把底层 io.EOF 原样上抛，
// 被截断的帧被下游当成正常流尾。

// TestReadBinary_PartialFromBufferEOF acquire 路径：先填缓冲再部分消费，
// 余量不足触发对已耗尽底层的补读——此前返回 (n, io.EOF)，契约要求
// (n, io.ErrUnexpectedEOF)。
func TestReadBinary_PartialFromBufferEOF(t *testing.T) {
	r := NewDefaultReader(bytes.NewReader([]byte("hello")))
	bs := make([]byte, 3)
	if n, err := r.ReadBinary(bs); n != 3 || err != nil {
		t.Fatalf("第一次 ReadBinary = (%d, %v), want (3, nil)", n, err)
	}
	// 缓冲剩 "lo"（2 字节），请求 4 ⇒ 缺口 2 落在 acquire 路径。
	bs2 := make([]byte, 4)
	n, err := r.ReadBinary(bs2)
	if n != 2 {
		t.Fatalf("部分读 n = %d, want 2", n)
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("部分读 err = %v, want io.ErrUnexpectedEOF（此前是 io.EOF）", err)
	}
	if string(bs2[:n]) != "lo" {
		t.Fatalf("部分读内容 = %q, want %q", bs2[:n], "lo")
	}
	// 粘滞终态后：无数据可读保持 (0, io.EOF)。
	if n, err := r.ReadBinary(make([]byte, 4)); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("耗尽后 ReadBinary = (%d, %v), want (0, io.EOF)", n, err)
	}
}

// TestReadBinary_PartialStickyDirectPath 直读粘滞路径：缓冲有存量（≥1 字节）
// 且 rdErr 已是 io.EOF、缺口 ≥ directlyReadThreshold——此前返回 (n, io.EOF)。
func TestReadBinary_PartialStickyDirectPath(t *testing.T) {
	r := NewDefaultReader(bytes.NewReader([]byte("hello")))
	// 先制造「缓冲存量 + rdErr=io.EOF」：Peek 超量触发对耗尽底层的补读。
	if _, err := r.Peek(10); !errors.Is(err, ErrNoRemainingData) {
		t.Fatalf("Peek 超量 err = %v, want ErrNoRemainingData", err)
	}
	if got := r.Buffered(); got != 5 {
		t.Fatalf("Peek 后缓冲 = %d, want 5（存量保留）", got)
	}
	// 直读路径：拷走 5 字节存量后缺口 4995 ≥ 4KiB，rdErr=io.EOF 粘滞返回。
	bs := make([]byte, 5000)
	n, err := r.ReadBinary(bs)
	if n != 5 {
		t.Fatalf("部分读 n = %d, want 5", n)
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("部分读 err = %v, want io.ErrUnexpectedEOF（此前是 io.EOF）", err)
	}
	if string(bs[:n]) != "hello" {
		t.Fatalf("部分读内容 = %q", bs[:n])
	}
}

// TestReadBinary_FreshExhaustedIsEOF 契约基线：无数据可读保持 (0, io.EOF)，
// 部分读归一不得吞掉「正常流尾」信号。
func TestReadBinary_FreshExhaustedIsEOF(t *testing.T) {
	r := NewDefaultReader(bytes.NewReader([]byte("")))
	if n, err := r.ReadBinary(make([]byte, 8)); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("空流 ReadBinary = (%d, %v), want (0, io.EOF)", n, err)
	}
}

// TestReadBinary_DirectPathPartial 直读路径本次自己读到字节再撞 EOF：
// readAtLeast 的升级形态，契约要求 (n, io.ErrUnexpectedEOF)。
func TestReadBinary_DirectPathPartial(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 5000)
	r := NewDefaultReader(bytes.NewReader(data))
	n, err := r.ReadBinary(make([]byte, 8192))
	if n != 5000 {
		t.Fatalf("部分读 n = %d, want 5000", n)
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("部分读 err = %v, want io.ErrUnexpectedEOF", err)
	}
	if got := r.ReadLen(); got != 5000 {
		t.Fatalf("ReadLen = %d, want 5000", got)
	}
}

// TestReadBinary_FullReadNoError 契约基线：读满返回 (len(p), nil)。
func TestReadBinary_FullReadNoError(t *testing.T) {
	r := NewDefaultReader(bytes.NewReader([]byte("hello")))
	bs := make([]byte, 5)
	if n, err := r.ReadBinary(bs); n != 5 || err != nil || string(bs) != "hello" {
		t.Fatalf("ReadBinary = (%d, %v, %q)", n, err, bs)
	}
}
