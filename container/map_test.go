package container

import (
	"sync"
	"testing"
)

func TestMapBasic(t *testing.T) {
	m := NewMap[string, int]()
	if _, ok := m.Load("a"); ok {
		t.Fatal("empty map should not contain a")
	}
	m.Store("a", 1)
	if v, ok := m.Load("a"); !ok || v != 1 {
		t.Fatalf("Load(a) = %v, %v; want 1, true", v, ok)
	}
	if _, loaded := m.LoadOrStore("a", 2); !loaded {
		t.Fatal("LoadOrStore existing key should report loaded")
	}
	if v, _ := m.Load("a"); v != 1 {
		t.Fatalf("LoadOrStore overwrote existing value: %v", v)
	}
	if actual, loaded := m.LoadOrStore("b", 3); loaded || actual != 3 {
		t.Fatalf("LoadOrStore new key = %v, %v; want 3, false", actual, loaded)
	}
	if m.Len() != 2 {
		t.Fatalf("Len = %d; want 2", m.Len())
	}
	m.Delete("a")
	if _, ok := m.Load("a"); ok {
		t.Fatal("Delete should remove key a")
	}
	m.Range(func(k string, v int) bool {
		if k != "b" || v != 3 {
			t.Fatalf("Range got %s=%d; want b=3", k, v)
		}
		return true
	})
	m.Clear()
	if m.Len() != 0 {
		t.Fatalf("Clear: Len = %d; want 0", m.Len())
	}
}

func TestMapRangeEarlyStop(t *testing.T) {
	m := NewMap[int, int]()
	for i := 0; i < 10; i++ {
		m.Store(i, i)
	}
	visited := 0
	m.Range(func(k, v int) bool {
		visited++
		return false
	})
	if visited != 1 {
		t.Fatalf("Range early stop visited %d; want 1", visited)
	}
}

func TestMapConcurrent(t *testing.T) {
	m := NewMap[int, int]()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				m.Store(i, i)
				m.Load(i)
				m.LoadOrStore(i+1000, i)
				m.Len()
			}
		}()
	}
	wg.Wait()
	if m.Len() != 400 { // 0..199 + LoadOrStore 去重后的 1000..1199
		t.Fatalf("Len = %d; want 400", m.Len())
	}
}
