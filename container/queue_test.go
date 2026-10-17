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
