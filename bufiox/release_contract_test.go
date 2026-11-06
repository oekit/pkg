package bufiox

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// 本文件钉住 Release(e error) 的参数语义与状态归约（[#216]）。
//
// 修复前 Release 完全忽略 e：连接层错误场景下调用方 Release(err) 期望整段
// 丢弃，残留数据却照样保留——契约文档写明的那条用途根本没实现。同时
// 「重置状态」在 Buffered()>0 时不成立（ri 保留，下个周期从上一轮的消费点
// 继续交付），而大缓冲会随残留帧滞留下来。

// TestDefaultReader_ReleaseErrorDiscardsResidual 钉住 e != nil ⇒ 整段丢弃。
func TestDefaultReader_ReleaseErrorDiscardsResidual(t *testing.T) {
	r := NewDefaultReader(bytes.NewReader([]byte("abcdefgh")))
	if _, err := r.Next(2); err != nil {
		t.Fatalf("Next: %v", err)
	}
	if r.Buffered() == 0 {
		t.Fatalf("前提不成立：需要残留缓冲才能验证丢弃（Buffered=0）")
	}

	if err := r.Release(errors.New("connection reset by peer")); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if got := r.Buffered(); got != 0 {
		t.Fatalf("Release(err) 后 Buffered = %d, want 0（残留必须整段丢弃）", got)
	}
	if got := r.ReadLen(); got != 0 {
		t.Fatalf("Release(err) 后 ReadLen = %d, want 0", got)
	}

	// 残留被丢弃 ⇒ 后续读取只能碰底层（此处已到 EOF，不再是 "cdefgh"）
	if _, err := r.Next(3); err == nil {
		t.Fatal("Release(err) 后仍能读到被丢弃的残留数据")
	}
}

// TestDefaultReader_ReleaseNilKeepsResidualAndResetsCursor 钉住 e == nil ⇒
// 保留残留但把游标归零。
//
// 保留的理由不是「顺手」：socket 上已读出的字节无法重读，丢弃即丢数据；
// 且残留可能是同一 TCP 段里下一条报文的开头。游标归零是「重置状态」那一半
// 承诺的兑现——buf[ri:] 与 buf 等价，ri 恒为 0。
func TestDefaultReader_ReleaseNilKeepsResidualAndResetsCursor(t *testing.T) {
	r := NewDefaultReader(bytes.NewReader([]byte("abcdefgh")))
	if _, err := r.Next(2); err != nil {
		t.Fatalf("Next: %v", err)
	}
	want := r.Buffered()

	if err := r.Release(nil); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if got := r.Buffered(); got != want {
		t.Fatalf("Release(nil) 后 Buffered = %d, want %d（残留必须保留）", got, want)
	}
	if r.ri != 0 {
		t.Fatalf("Release(nil) 后 ri = %d, want 0（游标必须归零）", r.ri)
	}
	if got := r.ReadLen(); got != 0 {
		t.Fatalf("Release(nil) 后 ReadLen = %d, want 0", got)
	}

	// 残留可从新周期的起点继续读，且内容不是被截断的
	p, err := r.Next(3)
	if err != nil || string(p) != "cde" {
		t.Fatalf("Release 后 Next(3) = (%q, %v), want (\"cde\", nil)", p, err)
	}
}

// TestDefaultReader_ReleaseInvalidatesPriorViews 钉住「旧视图失效」。
//
// Go 里「失效」不是运行时保证（切片不会变野指针、读它不 fault），所以这里
// 断言的是**实现确实让旧视图指向的区域变了**：残留原地左移覆盖了旧视图所在
// 位置。若哪天改成换数组，本用例的形态要跟着调整，但断言意图不变——不能出现
// 「Release 之后旧视图仍读到与之前一致的字节」。
func TestDefaultReader_ReleaseInvalidatesPriorViews(t *testing.T) {
	r := NewDefaultReader(bytes.NewReader([]byte("abcdefgh")))
	p, err := r.Next(2)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if string(p) != "ab" {
		t.Fatalf("前提不成立：Next(2) = %q, want \"ab\"", p)
	}

	if err := r.Release(nil); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if got := string(p); got == "ab" {
		t.Fatalf("Release 后旧视图内容仍是 %q——视图未失效（残留未归位）", got)
	}
}

// TestDefaultReader_ReleaseShrinksOversizedBuffer 钉住「大缓冲不随残留帧滞留」。
//
// 场景：读了一个 4KB 的帧、只留下几十字节残留，然后 Release。修复前遗留的
// 是整个大缓冲（最坏可到 1GiB 上限），残留却是杯水车薪。现在残留被换成右
// 尺寸数组，大缓冲可回收。
func TestDefaultReader_ReleaseShrinksOversizedBuffer(t *testing.T) {
	const total = 5000
	r := NewDefaultReader(bytes.NewReader(bytes.Repeat([]byte("x"), total)))

	// 一次读走 4000 字节：缓冲按 bufSize 分配（8KiB），已消费游标落在 4000，
	// 残留是「缓冲里剩下的 1000 字节」——容量是残留的 8 倍，正是要收缩的形态。
	if _, err := r.Next(4000); err != nil {
		t.Fatalf("Next(4000): %v", err)
	}
	want := r.Buffered()
	if want == 0 {
		t.Fatalf("前提不成立：需要残留才能验证收缩")
	}
	before := cap(r.buf)
	if before <= compactSlackFactor*want {
		t.Fatalf("前提不成立：cap=%d 未超过残留 %d 的 %d 倍", before, want, compactSlackFactor)
	}

	if err := r.Release(nil); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if got := r.Buffered(); got != want {
		t.Fatalf("Release 后 Buffered = %d, want %d（收缩不得丢数据）", got, want)
	}
	if after := cap(r.buf); after > before {
		t.Fatalf("Release 后 cap 反而变大: %d → %d", before, after)
	}
	if after := cap(r.buf); after > compactSlackFactor*want {
		t.Fatalf("Release 后 cap = %d 仍远超残留 %d（应换成右尺寸数组）", after, want)
	}
}

// TestBytesReader_ReleaseErrorArgument 钉住 BytesReader 侧的同一契约（[#216]）：e != nil
// 整段丢弃；e == nil 回到起点（固定帧重新解析是它的常见用法）。
func TestBytesReader_ReleaseErrorArgument(t *testing.T) {
	t.Run("e_nil_回到起点", func(t *testing.T) {
		r := NewBytesReader([]byte("abcdef"))
		if _, err := r.Next(2); err != nil {
			t.Fatalf("Next: %v", err)
		}
		if err := r.Release(nil); err != nil {
			t.Fatalf("Release: %v", err)
		}
		if got := r.ReadLen(); got != 0 {
			t.Fatalf("ReadLen = %d, want 0", got)
		}
		p, err := r.Next(1)
		if err != nil || string(p) != "a" {
			t.Fatalf("Release(nil) 后 Next(1) = (%q, %v), want (\"a\", nil)", p, err)
		}
	})

	t.Run("e_非nil_整段丢弃", func(t *testing.T) {
		r := NewBytesReader([]byte("abcdef"))
		if _, err := r.Next(2); err != nil {
			t.Fatalf("Next: %v", err)
		}
		if err := r.Release(io.ErrUnexpectedEOF); err != nil {
			t.Fatalf("Release: %v", err)
		}
		if got := r.ReadLen(); got != 0 {
			t.Fatalf("ReadLen = %d, want 0", got)
		}
		if _, err := r.Next(1); !errors.Is(err, ErrNoRemainingData) {
			t.Fatalf("Release(err) 后 Next(1) err = %v, want ErrNoRemainingData", err)
		}
	})
}
