package container

import "sync"

// Map 并发安全的泛型映射（sync.Map 背书，对标 go-kratos/kit container/maps）。
// 读多写少、key 集合趋于稳定时性能优于 SafeMap；写冲突频繁或需要 GetOrSet
// 语义时用 SafeMap。零值不可直接使用，须经 NewMap 创建。
type Map[K comparable, V any] struct {
	m sync.Map
}

// NewMap 创建空映射。
func NewMap[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{}
}

// Load 读取键值；不存在返回零值与 false。
func (m *Map[K, V]) Load(k K) (V, bool) {
	v, ok := m.m.Load(k)
	if !ok {
		var zero V
		return zero, false
	}
	return v.(V), true
}

// Store 写入键值。
func (m *Map[K, V]) Store(k K, v V) {
	m.m.Store(k, v)
}

// LoadOrStore 原子地"不存在则写入"：返回当前值与是否本已存在。
func (m *Map[K, V]) LoadOrStore(k K, v V) (V, bool) {
	actual, loaded := m.m.LoadOrStore(k, v)
	return actual.(V), loaded
}

// Delete 删除键（不存在为 no-op）。
func (m *Map[K, V]) Delete(k K) {
	m.m.Delete(k)
}

// Range 遍历快照；fn 返回 false 提前终止。遍历期间其他 goroutine 的写入
// 不阻塞，但可见性不保证（标准 sync.Map Range 语义）。
func (m *Map[K, V]) Range(fn func(k K, v V) bool) {
	m.m.Range(func(k, v any) bool {
		return fn(k.(K), v.(V))
	})
}

// Len 返回键值对数量（通过一次全量 Range 统计，O(n)；高频调用请自行缓存）。
func (m *Map[K, V]) Len() int {
	n := 0
	m.m.Range(func(k, v any) bool {
		n++
		return true
	})
	return n
}

// Clear 清空全部键值。
func (m *Map[K, V]) Clear() {
	m.m.Range(func(k, _ any) bool {
		m.m.Delete(k.(K))
		return true
	})
}
