package container

import (
	"reflect"
	"sync"
	"testing"
)

func TestSliceBasic(t *testing.T) {
	s := NewSlice[int](1, 2, 3)
	if s.Len() != 3 {
		t.Fatalf("Len = %d; want 3", s.Len())
	}
	if v, ok := s.Get(1); !ok || v != 2 {
		t.Fatalf("Get(1) = %v, %v; want 2, true", v, ok)
	}
	if _, ok := s.Get(3); ok {
		t.Fatal("Get out of range should return false")
	}
	if !s.Set(0, 10) {
		t.Fatal("Set(0) should succeed")
	}
	if s.Set(3, 99) {
		t.Fatal("Set out of range should return false")
	}
	if !s.Delete(1) {
		t.Fatal("Delete(1) should succeed")
	}
	if s.Delete(5) {
		t.Fatal("Delete out of range should return false")
	}
	if got := s.Snapshot(); !reflect.DeepEqual(got, []int{10, 3}) {
		t.Fatalf("Snapshot = %v; want [10 3]", got)
	}
	s.Append(4, 5)
	if got := s.Snapshot(); !reflect.DeepEqual(got, []int{10, 3, 4, 5}) {
		t.Fatalf("Snapshot = %v; want [10 3 4 5]", got)
	}
	s.Clear()
	if s.Len() != 0 {
		t.Fatalf("Clear: Len = %d; want 0", s.Len())
	}
}

func TestSliceRangeEarlyStop(t *testing.T) {
	s := NewSlice[int](1, 2, 3)
	visited := 0
	s.Range(func(i, v int) bool {
		visited++
		return false
	})
	if visited != 1 {
		t.Fatalf("Range early stop visited %d; want 1", visited)
	}
}

func TestSliceSnapshotIsCopy(t *testing.T) {
	s := NewSlice[int](1, 2, 3)
	snap := s.Snapshot()
	snap[0] = 99
	if v, _ := s.Get(0); v != 1 {
		t.Fatalf("Snapshot leaked into source: Get(0) = %v; want 1", v)
	}
}

func TestSliceConcurrent(t *testing.T) {
	s := NewSlice[int]()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				s.Append(i)
				s.Len()
				s.Get(0)
				s.Snapshot()
				s.Range(func(i, v int) bool { return i < 10 })
			}
		}()
	}
	wg.Wait()
	if s.Len() != 800 {
		t.Fatalf("Len = %d; want 800", s.Len())
	}
}
