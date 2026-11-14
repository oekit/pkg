package container

import "sync"

// Map 并发安全的泛型映射（sync.Map 背书，对标 go-kratos/kit container/maps）。
// 读多写少、key 集合趋于稳定时性能优于 SafeMap；写冲突频繁或需要 GetOrSet
// 语义时用 SafeMap。零值即可用（底层 sync.Map 的零值语义，#476——此前
// 「零值不可直接使用」与构造器返回零值自相矛盾），NewMap 仅是可读性糖。
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

// Snapshot 返回全部键值的整体拷贝（plain map，调用方持有独立副本，可安全
// 修改/交给非并发安全代码）。副本与容器此后互不影响。
//
// 与 SafeMap.Snapshot 对偶（#393）：「容器内容出口」的键值形态有单调用
// 原语，调用方不再手写「Range 构建副本」的样板循环——那段样板写错成边遍历
// 边写活体会绕开容器纪律。两个类型的 godoc 互链，出口原语对偶闭合。
//
// sync.Map 的 Range 本身就是锁外遍历语义，收集不持有任何锁；遍历期间的
// 并发写入可见性不保证（同 Range 的标准 sync.Map 语义）。
//
// 返回值永不为 nil（容器为空时返回空 map），可直接交给序列化/调试转储。
func (m *Map[K, V]) Snapshot() map[K]V {
	out := make(map[K]V)
	m.m.Range(func(k, v any) bool {
		out[k.(K)] = v.(V)
		return true
	})
	return out
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
