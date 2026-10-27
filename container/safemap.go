package container

import (
	"sync"
)

// SafeMap 并发安全的泛型映射（RWMutex 保护），对标 go-zero collection/SafeMap
// 的泛型化替代。零值不可直接使用，须经 NewSafeMap 创建。
type SafeMap[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

// NewSafeMap 创建空映射。
func NewSafeMap[K comparable, V any]() *SafeMap[K, V] {
	return &SafeMap[K, V]{m: make(map[K]V)}
}

// Set 写入键值。
func (sm *SafeMap[K, V]) Set(k K, v V) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.m[k] = v
}

// Get 读取键值；不存在返回零值与 false。
func (sm *SafeMap[K, V]) Get(k K) (V, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	v, ok := sm.m[k]
	return v, ok
}

// GetOrSet 原子地"不存在则写入"：返回当前值与是否本已存在。
func (sm *SafeMap[K, V]) GetOrSet(k K, v V) (V, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if old, ok := sm.m[k]; ok {
		return old, true
	}
	sm.m[k] = v
	return v, false
}

// Delete 删除键（不存在为 no-op）。
func (sm *SafeMap[K, V]) Delete(k K) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.m, k)
}

// Len 返回键值对数量。
func (sm *SafeMap[K, V]) Len() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.m)
}

// Range 遍历快照；fn 返回 false 提前终止。
//
// **先在读锁内取快照，再在锁外遍历**（同 Slice.Range 的做法）：
//   - 此前遍历期间**一直持着读锁** ⇒ 文档声称的「遍历期间其他 goroutine 的
//     写入不阻塞」并不成立（Set 要写锁，会被读锁挡住直到遍历结束）；
//   - 更糟的是回调里写同一个 map 会**死锁**：RWMutex 不可重入，回调的
//     Set 要写锁、写锁要等读锁释放、读锁要等回调返回——互等。
//     （实测：回调里 sm.Set 同��个 map ⇒ 永久挂起。）
//
// 快照语义：遍历期间的并发写入不影响本次遍历（同 Slice.Range）。
func (sm *SafeMap[K, V]) Range(fn func(k K, v V) bool) {
	sm.mu.RLock()
	keys := make([]K, 0, len(sm.m))
	vals := make([]V, 0, len(sm.m))
	for k, v := range sm.m {
		keys = append(keys, k)
		vals = append(vals, v)
	}
	sm.mu.RUnlock()

	for i := range keys {
		if !fn(keys[i], vals[i]) {
			return
		}
	}
}

// Clear 清空全部键值。
func (sm *SafeMap[K, V]) Clear() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.m = make(map[K]V)
}
