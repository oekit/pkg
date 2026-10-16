package collectionx

// Set 泛型集合（非并发安全：单 goroutine 使用，或由调用方加锁保护）。
type Set[T comparable] map[T]struct{}

// NewSet 以给定元素创建集合并去重。
func NewSet[T comparable](items ...T) Set[T] {
	s := make(Set[T], len(items))
	s.AddAll(items...)
	return s
}

// Add 加入元素。
func (s Set[T]) Add(v T) { s[v] = struct{}{} }

// AddAll 批量加入元素。
func (s Set[T]) AddAll(items ...T) {
	for _, v := range items {
		s[v] = struct{}{}
	}
}

// Remove 移除元素（不存在为 no-op）。
func (s Set[T]) Remove(v T) { delete(s, v) }

// Contains 判断元素是否在集合中。
func (s Set[T]) Contains(v T) bool {
	_, ok := s[v]
	return ok
}

// Len 返回元素个数。
func (s Set[T]) Len() int { return len(s) }

// Items 返回全部元素（顺序不保证）。
func (s Set[T]) Items() []T {
	out := make([]T, 0, len(s))
	for v := range s {
		out = append(out, v)
	}
	return out
}

// Range 遍历全部元素；fn 返回 false 提前终止。
func (s Set[T]) Range(fn func(v T) bool) {
	for v := range s {
		if !fn(v) {
			return
		}
	}
}

// Clone 深拷贝出一个新集合。
func (s Set[T]) Clone() Set[T] {
	out := make(Set[T], len(s))
	for v := range s {
		out[v] = struct{}{}
	}
	return out
}

// Union 并集：返回包含两者全部元素的新集合。
func (s Set[T]) Union(other Set[T]) Set[T] {
	out := s.Clone()
	out.AddAll(other.Items()...)
	return out
}

// Intersect 交集：返回同时存在于两者的新集合。
func (s Set[T]) Intersect(other Set[T]) Set[T] {
	out := NewSet[T]()
	for v := range s {
		if other.Contains(v) {
			out.Add(v)
		}
	}
	return out
}

// Difference 差集：返回存在于 s 但不存在于 other 的新集合。
func (s Set[T]) Difference(other Set[T]) Set[T] {
	out := NewSet[T]()
	for v := range s {
		if !other.Contains(v) {
			out.Add(v)
		}
	}
	return out
}
