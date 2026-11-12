package container

import "testing"

// #393 回归：Map（sync.Map 背书）与 SafeMap 的内容出口原语对偶闭合——
// Map.Snapshot 与 SafeMap.Snapshot 同名同义（键值 → map[K]V 整体拷贝），
// 调用方不再手写「Range 构建副本」的样板循环。

func TestMap_Snapshot(t *testing.T) {
	m := NewMap[string, int]()
	m.Store("a", 1)
	m.Store("b", 2)

	snap := m.Snapshot()
	if len(snap) != 2 || snap["a"] != 1 || snap["b"] != 2 {
		t.Fatalf("Snapshot = %v", snap)
	}

	// 副本独立：改快照不影响容器。
	snap["a"] = 99
	delete(snap, "b")
	if v, _ := m.Load("a"); v != 1 {
		t.Fatalf("改快照污染了容器: a=%v", v)
	}
	if _, ok := m.Load("b"); !ok {
		t.Fatal("删快照键影响了容器")
	}

	// 容器后续写入不影响已有快照。
	m.Store("c", 3)
	if _, exists := snap["c"]; exists {
		t.Fatal("快照不是独立副本")
	}
}

func TestMap_Snapshot_EmptyNotNil(t *testing.T) {
	m := NewMap[int, string]()
	snap := m.Snapshot()
	if snap == nil {
		t.Fatal("空容器 Snapshot 必须返回空 map 而非 nil（可直接交给序列化）")
	}
	if len(snap) != 0 {
		t.Fatalf("空容器 Snapshot = %v", snap)
	}
}

func TestMap_Snapshot_Concurrent(t *testing.T) {
	// -race 烟测：Snapshot 与 Store/Delete 并发无数据竞争。
	m := NewMap[int, int]()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 500 {
			m.Store(i, i)
			m.Delete(i - 100)
		}
	}()
	for range 100 {
		_ = m.Snapshot()
	}
	<-done
}
