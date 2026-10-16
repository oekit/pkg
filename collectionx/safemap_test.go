package collectionx

import (
	"sync"
	"testing"
)

func TestSafeMap_BasicOps(t *testing.T) {
	sm := NewSafeMap[string, int]()
	sm.Set("a", 1)
	sm.Set("b", 2)

	if v, ok := sm.Get("a"); !ok || v != 1 {
		t.Fatalf("expected a=1, got %d (ok=%v)", v, ok)
	}
	if _, ok := sm.Get("zz"); ok {
		t.Fatal("expected missing key")
	}
	if sm.Len() != 2 {
		t.Fatalf("expected len 2, got %d", sm.Len())
	}

	sm.Delete("a")
	if _, ok := sm.Get("a"); ok {
		t.Fatal("expected a deleted")
	}
	sm.Delete("zz") // 不存在为 no-op

	sm.Clear()
	if sm.Len() != 0 {
		t.Fatalf("expected empty after clear, got %d", sm.Len())
	}
}

func TestSafeMap_GetOrSet(t *testing.T) {
	sm := NewSafeMap[string, int]()
	v, loaded := sm.GetOrSet("k", 1)
	if loaded || v != 1 {
		t.Fatalf("first GetOrSet expected (1,false), got (%d,%v)", v, loaded)
	}
	v, loaded = sm.GetOrSet("k", 99)
	if !loaded || v != 1 {
		t.Fatalf("second GetOrSet expected (1,true), got (%d,%v)", v, loaded)
	}
}

func TestSafeMap_ConcurrentSetGet(t *testing.T) {
	sm := NewSafeMap[int, int]()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				sm.Set(i, g)
				sm.Get(i)
				if i%10 == 0 {
					sm.GetOrSet(i, g)
				}
			}
		}(g)
	}
	wg.Wait()
	if sm.Len() != 200 {
		t.Fatalf("expected 200 keys, got %d", sm.Len())
	}

	visited := 0
	sm.Range(func(k, v int) bool {
		visited++
		return visited < 50 // 提前终止路径
	})
	if visited != 50 {
		t.Fatalf("expected range stop at 50, got %d", visited)
	}
}
