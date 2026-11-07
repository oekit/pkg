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

// AppendIfAbsent 原子地「查重 + 追加」（#178）：已存在等值元素时返回 false 且
// 不改动切片；不存在时**在同一把写锁内**追加并返回 true。有序注册表场景
// （先注册先命中、查重 fail-fast）对应 SafeMap.GetOrSet 的「一次性原子」承诺——
// 先 Range/Snapshot 查重再 Append 是两步，重复注册的 fail-fast 窗口里会插进
// 两个同名条目。
//
// eq 为 nil 时按 == 比较（interface 相等：动态类型与值都相等；元素动态类型
// 不可比较时 panic，与 map 访问不可比较键同一运行时契约）。需要深比较
// （指针指向内容、切片逐元素）或自定义等价关系（host 别名大小写归一等）
// 时传入 eq。遍历按注册顺序进行，命中第一个等值元素即停。
func (s *Slice[T]) AppendIfAbsent(v T, eq func(a, b T) bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.s {
		if eq != nil {
			if eq(existing, v) {
				return false
			}
			continue
		}
		if any(existing) == any(v) {
			return false
		}
	}
	s.s = append(s.s, v)
	return true
}

// RemoveIf 原子删除所有命中谓词的元素，返回删除个数（#199——
// AppendIfAbsent 的删除侧对偶：有序注册表的「按名注销/按前缀删」主路径）。
//
// 「先 Snapshot/Range 找下标、再 Delete(i)」的两步组合在并发写入下不安全：
// Range 在快照副本上遍历（它正确的并发语义），副本下标与活体切片在并发
// Append/Delete 下会错位 ⇒ Delete(i) 删的是**另一个元素**。本方法把遍历
// 与删除收进**同一把写锁**（与 AppendIfAbsent 同纪律）。
//
// 谓词在锁内调用——必须轻量且**不得回调本切片的任何方法**（死锁边界；
// 同族先例：sync.Map 的 Range 也在锁语义内回调）。pred 为 nil 时 no-op
// 返回 0。返回删除**个数**而非被删元素副本（要副本先 Snapshot 自查——
// 返回副本列表会助长「谓词内副作用」的用法）。
func (s *Slice[T]) RemoveIf(pred func(v T) bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pred == nil || len(s.s) == 0 {
		return 0
	}
	kept := s.s[:0]
	removed := 0
	for _, v := range s.s {
		if pred(v) {
			removed++
			continue
		}
		kept = append(kept, v)
	}
	if removed > 0 {
		// 清尾（大对象滞留纪律，同 Delete/Clear）：过滤后尾部残留的旧引用
		// 会钉住已删元素，GC 收不走。
		var zero T
		for i := len(kept); i < len(s.s); i++ {
			s.s[i] = zero
		}
		s.s = kept
	}
	return removed
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
	// 把被删元素从底层数组里清掉：只缩 len 不清内容 ⇒ 底层数组仍持有被删
	// 元素 ⇒ GC 收不走（大对象滞留）。同包 Queue 的 Close 已是这个做法。
	old := s.s
	copy(old[i:], old[i+1:])
	var zero T
	old[len(old)-1] = zero
	s.s = old[:len(old)-1]
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
	// 逐个清零：只缩 len 不清内容 ⇒ 底层数组仍持有全部元素（大对象滞留）。
	for i := range s.s {
		var zero T
		s.s[i] = zero
	}
	s.s = s.s[:0]
}
