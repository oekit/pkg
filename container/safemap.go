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

// Snapshot 返回全部键值的整体拷贝（plain map，调用方持有独立副本，可安全
// 修改/交给非并发安全代码）。副本与容器此后互不影响（#272）。
//
// 与 Slice.Snapshot 同纪律：**锁内整体拷贝、锁外交付**——拷贝在读锁内完成，
// 返回后调用方怎么改都不再经过容器的锁。API 对偶闭合后，「容器内容出口」
// 两个形态（有序 → []T，键值 → map[K]V）都有单调用原语；调用方不再手写
// 「Range 构建副本」的样板循环——那段样板写错成边遍历边写活体会绕开容器
// 纪律（Range 返回的是快照，正确但没有任何契约提示），Snapshot 把正确姿势
// 收成编译期可调用的单点。
//
// 返回值永不为 nil（容器为空时返回空 map），可直接交给序列化/调试转储。
func (sm *SafeMap[K, V]) Snapshot() map[K]V {
	sm.mu.RLock()
	out := make(map[K]V, len(sm.m))
	for k, v := range sm.m {
		out[k] = v
	}
	sm.mu.RUnlock()
	return out
}

// Clear 清空全部键值。
func (sm *SafeMap[K, V]) Clear() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.m = make(map[K]V)
}
