package container

import (
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
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
	for range 8 {
		wg.Go(func() {
			for i := range 100 {
				s.Append(i)
				s.Len()
				s.Get(0)
				s.Snapshot()
				s.Range(func(i, v int) bool { return i < 10 })
			}
		})
	}
	wg.Wait()
	if s.Len() != 800 {
		t.Fatalf("Len = %d; want 800", s.Len())
	}
}

// ---- AppendIfAbsent（#178）：有序注册表的原子「查重+追加」 ----

// TestSlice_AppendIfAbsent_ExactEqual 锁定原子性契约：已存在等值元素时返回
// false 且不改动切片；不存在时追加返回 true。修复前「Range 查重 + Append」
// 两步之间重复注册会插进两个同名条目。
func TestSlice_AppendIfAbsent_ExactEqual(t *testing.T) {
	s := NewSlice[string]()
	if !s.AppendIfAbsent("host-a", nil) {
		t.Fatal("首个元素应追加成功")
	}
	if s.AppendIfAbsent("host-a", nil) {
		t.Fatal("重复注册应返回 false")
	}
	if got := s.Len(); got != 1 {
		t.Fatalf("重复注册不应改动切片：Len=%d，want 1", got)
	}
	if !s.AppendIfAbsent("host-b", nil) {
		t.Fatal("新元素应追加成功")
	}
	// 有序性：追加到尾部，先注册先命中
	if v, _ := s.Get(0); v != "host-a" {
		t.Fatalf("有序性破坏：Get(0)=%q", v)
	}
	if v, _ := s.Get(1); v != "host-b" {
		t.Fatalf("Get(1)=%q，want host-b", v)
	}
}

// TestSlice_AppendIfAbsent_CustomEq 自定义等价关系（大小写归一）。
func TestSlice_AppendIfAbsent_CustomEq(t *testing.T) {
	s := NewSlice[string]("Host-A")
	ci := func(a, b string) bool { return strings.EqualFold(a, b) }
	if s.AppendIfAbsent("host-a", ci) {
		t.Fatal("大小写归一后应视为已存在")
	}
	if !s.AppendIfAbsent("host-b", ci) {
		t.Fatal("不同 host 应追加")
	}
}

// TestSlice_AppendIfAbsent_StructElement 结构体元素：== 口径（同值同类型）。
func TestSlice_AppendIfAbsent_StructElement(t *testing.T) {
	type reg struct {
		Name string
		Prio int
	}
	s := NewSlice[reg]()
	if !s.AppendIfAbsent(reg{"a", 1}, nil) {
		t.Fatal("首个元素应追加")
	}
	if s.AppendIfAbsent(reg{"a", 1}, nil) {
		t.Fatal("同值同类型应视为已存在")
	}
	if !s.AppendIfAbsent(reg{"a", 2}, nil) {
		t.Fatal("值不同应追加")
	}
	if got := s.Len(); got != 2 {
		t.Fatalf("Len=%d，want 2", got)
	}
}

// TestSlice_AppendIfAbsent_Concurrent 原子性：并发注册同 key 只能有恰好一个
// 成功（两步查重+追加在竞态下会插进多个同名条目，go-git-platform 查重
// fail-fast 的窗口）。
func TestSlice_AppendIfAbsent_Concurrent(t *testing.T) {
	const goroutines = 50
	s := NewSlice[string]()
	var success atomic.Int64
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			if s.AppendIfAbsent("same-key", nil) {
				success.Add(1)
			}
		})
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatalf("并发注册同 key 应恰好一个成功，got %d", success.Load())
	}
	if got := s.Len(); got != 1 {
		t.Fatalf("Len=%d，want 1", got)
	}
}
