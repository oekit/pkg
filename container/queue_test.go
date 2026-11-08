package container

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueue_Fifo(t *testing.T) {
	q := NewQueue[int]()
	q.Put(1)
	q.Put(2)
	if q.Len() != 2 {
		t.Fatalf("expected len 2, got %d", q.Len())
	}
	if v, ok := q.TryTake(); !ok || v != 1 {
		t.Fatalf("expected 1, got %d (ok=%v)", v, ok)
	}
	if v, ok := q.Take(); !ok || v != 2 {
		t.Fatalf("expected 2, got %d (ok=%v)", v, ok)
	}
	if _, ok := q.TryTake(); ok {
		t.Fatal("expected empty")
	}
}

func TestQueue_TakeBlocksUntilPut(t *testing.T) {
	q := NewQueue[string]()
	done := make(chan string, 1)
	go func() {
		v, _ := q.Take()
		done <- v
	}()
	// 稍等让消费者先阻塞
	time.Sleep(10 * time.Millisecond)
	q.Put("x")
	if v := <-done; v != "x" {
		t.Fatalf("expected x, got %s", v)
	}
}

func TestQueue_TakeReturnsOnClose(t *testing.T) {
	q := NewQueue[int]()
	go func() {
		time.Sleep(10 * time.Millisecond)
		q.Close()
	}()
	if _, ok := q.Take(); ok {
		t.Fatal("expected false on closed empty queue")
	}
	q.Close() // 幂等
	if v, ok := q.TryTake(); ok || v != 0 {
		t.Fatalf("expected zero,false, got %d,%v", v, ok)
	}
}

func TestQueue_CloseThenPutPanics(t *testing.T) {
	q := NewQueue[int]()
	q.Close()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on put after close")
		}
	}()
	q.Put(1)
}

func TestQueue_ConcurrentNoLoss(t *testing.T) {
	const producers, perProducer, consumers = 4, 500, 3
	q := NewQueue[int]()

	var produced atomic.Int64
	var wg sync.WaitGroup
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perProducer; i++ {
				q.Put(1)
				produced.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := q.Len(); got != producers*perProducer {
		t.Fatalf("expected all %d queued, got %d", producers*perProducer, got)
	}

	var consumed atomic.Int64
	var cwg sync.WaitGroup
	for c := 0; c < consumers; c++ {
		cwg.Add(1)
		go func() {
			defer cwg.Done()
			for {
				_, ok := q.Take()
				if !ok {
					return
				}
				consumed.Add(1)
			}
		}()
	}
	q.Close()
	cwg.Wait()
	if consumed.Load() != produced.Load() {
		t.Fatalf("loss: produced %d, consumed %d", produced.Load(), consumed.Load())
	}
}

// TestQueue_TryTakeAfterCloseNoDequeue 回归（[#258]）：TryTake 的文档写
// 「已关闭返回零值与 false」，实现此前却无条件 popLocked——Put(1) 后
// Close，TryTake 照样把最后一条元素出队。调用方按「关闭即不再有数据」
// 写收尾逻辑时，会在关闭后多拿到一条（重复处理最后一条，或 defer Close
// 之后仍把这条数据当有效值投递出去）。
func TestQueue_TryTakeAfterCloseNoDequeue(t *testing.T) {
	q := NewQueue[int]()
	q.Put(1)
	q.Close()

	v, ok := q.TryTake()
	if ok || v != 0 {
		t.Fatalf("TryTake on closed queue = (%d, %v), want (0, false)（关闭即不再有数据）", v, ok)
	}
	// 元素没有被出队：仍在余量里，交给 Take 的排空语义。
	if got := q.Len(); got != 1 {
		t.Fatalf("Len after closed TryTake = %d, want 1（元素不得被出队）", got)
	}
	// #258 复现序列的第二段：Take 的排空语义保持不变。
	if v2, ok2 := q.Take(); !ok2 || v2 != 1 {
		t.Fatalf("Take after close should drain remaining, got (%d, %v)", v2, ok2)
	}
	if _, ok := q.Take(); ok {
		t.Fatal("Take on closed drained queue should return false")
	}
}

// TestQueue_TryTakeVsTakeAfterClose 钉住两入口的分工口径（[#258] 同源）：
// 多条余量下，Close 之后 TryTake 一个都拿不到、Take 逐条排空。
func TestQueue_TryTakeVsTakeAfterClose(t *testing.T) {
	q := NewQueue[string]()
	q.Put("a")
	q.Put("b")
	q.Put("c")
	q.Close()

	for i := 0; i < 3; i++ {
		if v, ok := q.TryTake(); ok {
			t.Fatalf("TryTake #%d on closed queue = (%q, true), want false", i+1, v)
		}
	}
	for _, want := range []string{"a", "b", "c"} {
		if v, ok := q.Take(); !ok || v != want {
			t.Fatalf("Take after close = (%q, %v), want (%q, true)", v, ok, want)
		}
	}
	if v, ok := q.Take(); ok {
		t.Fatalf("Take on drained closed queue = (%q, true), want false", v)
	}
}
