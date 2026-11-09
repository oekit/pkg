package procx

import (
	"bytes"
	"testing"
)

// 仓规总则①：非正上限构造期 fail-fast（0 与负值都在选项构造点 panic）。
func TestWithOutputLimit_NonPositivePanics(t *testing.T) {
	for _, n := range []int{0, -1, -(64 << 10)} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("上限 %d 应构造期 panic（仓规总则①）", n)
				}
			}()
			WithOutputLimit(n)
		}()
	}
}

// 限容基本语义：正好等于上限不算截断；超限部分不再保留但 Write 仍报全量
// 消费成功——后者是排空契约：报少了 io.Copy 会以为管道堵了，子进程卡在
// write 上（httpjson.Truncated 同口径：==limit 不算截断）。
func TestCappedBuffer_LimitSemantics(t *testing.T) {
	b := &cappedBuffer{limit: 10}
	if _, err := b.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	if b.truncated || len(b.buf) != 10 {
		t.Fatalf("正好等于上限不算截断: buf=%q truncated=%v", b.buf, b.truncated)
	}
	n, err := b.Write([]byte("xyz"))
	if err != nil || n != 3 {
		t.Fatalf("超限写入必须报全量消费成功（排空契约）: n=%d err=%v", n, err)
	}
	if !b.truncated || string(b.buf) != "0123456789" {
		t.Fatalf("超限写入: buf=%q truncated=%v", b.buf, b.truncated)
	}
}

// 洪封顶场景（issue 判据的缩比）：106,496 字节灌进 64KB 上限——缓冲钉在
// limit、truncated 恒可判定、每次写都全量成功（管道持续排空，内存不放大）。
func TestCappedBuffer_FloodStaysAtLimit(t *testing.T) {
	b := &cappedBuffer{limit: 64 << 10}
	chunk := bytes.Repeat([]byte{0x78}, 8192)
	for i := 0; i < 13; i++ {
		n, err := b.Write(chunk)
		if err != nil || n != len(chunk) {
			t.Fatalf("写入必须全量成功（否则子进程卡 write）: n=%d err=%v", n, err)
		}
	}
	if len(b.buf) != 64<<10 || !b.truncated {
		t.Fatalf("缓冲应钉在上限: len=%d truncated=%v", len(b.buf), b.truncated)
	}
}

// 不限容（limit<=0）= 全量保留、不标记截断——Run 缺省语义（存量行为）的
// 底层口径。
func TestCappedBuffer_NoLimitKeepsAll(t *testing.T) {
	b := &cappedBuffer{}
	_, _ = b.Write(bytes.Repeat([]byte("a"), 100000))
	if len(b.buf) != 100000 || b.truncated {
		t.Fatalf("未设上限应全量保留: len=%d truncated=%v", len(b.buf), b.truncated)
	}
}
