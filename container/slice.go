package container

import "sync"

// Slice 并发安全的泛型切片（RWMutex 保护，对标 go-kratos/kit container/slices）。
// 零值不可直接使用，须经 NewSlice 创建。
type Slice[T any] struct {
	mu sync.RWMutex
	s  []T
}

// NewSlice 以给定元素创建切片。
func NewSlice[T any](items ...T) *Slice[T] {
	s := &Slice[T]{}
	if len(items) > 0 {
		s.s = append(s.s, items...)
	}
	return s
}

// Append 追加元素。
func (s *Slice[T]) Append(items ...T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s = append(s.s, items...)
}

// Len 返回元素个数。
func (s *Slice[T]) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.s)
}

// Get 读取下标 i 的元素；越界返回零值与 false。
func (s *Slice[T]) Get(i int) (T, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var zero T
	if i < 0 || i >= len(s.s) {
		return zero, false
	}
	return s.s[i], true
}

// Set 覆写下标 i 的元素；越界返回 false。
func (s *Slice[T]) Set(i int, v T) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= len(s.s) {
		return false
	}
	s.s[i] = v
	return true
}

// Delete 删除下标 i 的元素（后续元素前移）；越界为 no-op 并返回 false。
func (s *Slice[T]) Delete(i int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= len(s.s) {
		return false
	}
	s.s = append(s.s[:i], s.s[i+1:]...)
	return true
}

// Snapshot 返回全部元素的拷贝（调用方持有独立副本，可安全修改）。
func (s *Slice[T]) Snapshot() []T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]T, len(s.s))
	copy(out, s.s)
	return out
}

// Range 遍历快照副本；fn 返回 false 提前终止。遍历期间的并发写入
// 不影响本次遍历（每次 Range 都在独立快照上进行）。
func (s *Slice[T]) Range(fn func(i int, v T) bool) {
	for i, v := range s.Snapshot() {
		if !fn(i, v) {
			return
		}
	}
}

// Clear 清空全部元素（保留底层数组容量）。
func (s *Slice[T]) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s = s.s[:0]
}
