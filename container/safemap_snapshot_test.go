package container

import (
	"reflect"
	"sync"
	"testing"
)

// TestSafeMap_Snapshot_Basic #272：Snapshot 返回全部键值的整体拷贝，
// 空 map 返回非 nil 的空 map。
func TestSafeMap_Snapshot_Basic(t *testing.T) {
	empty := NewSafeMap[string, int]()
	got := empty.Snapshot()
	if got == nil {
		t.Fatal("空容器 Snapshot 应返回非 nil 空 map")
	}
	if len(got) != 0 {
		t.Fatalf("空容器 Snapshot 应返回空 map, got %v", got)
	}

	sm := NewSafeMap[string, int]()
	sm.Set("a", 1)
	sm.Set("b", 2)
	sm.Set("c", 3)
	got = sm.Snapshot()
	want := map[string]int{"a": 1, "b": 2, "c": 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot 应与容器内容一致, got %v want %v", got, want)
	}
	if sm.Len() != 3 {
		t.Fatalf("Snapshot 不应影响容器, len=%d", sm.Len())
	}
}

// TestSafeMap_Snapshot_Independent 副本与容器此后互不影响：改副本不穿透
// 容器，改容器不影响已有副本。
func TestSafeMap_Snapshot_Independent(t *testing.T) {
	sm := NewSafeMap[string, int]()
	sm.Set("a", 1)
	sm.Set("b", 2)

	snap := sm.Snapshot()
	// 改副本：容器不受影响
	snap["a"] = 100
	delete(snap, "b")
	snap["zz"] = 9
	if v, ok := sm.Get("a"); !ok || v != 1 {
		t.Fatalf("改副本不应影响容器, a=%d(ok=%v)", v, ok)
	}
	if _, ok := sm.Get("zz"); ok {
		t.Fatal("向副本写新键不应影响容器")
	}
	// 改容器：已有副本不受影响
	sm.Set("a", 77)
	sm.Set("new", 8)
	if snap["a"] != 100 {
		t.Fatalf("容器更新不应影响已发副本, snap[a]=%d", snap["a"])
	}
	if _, ok := snap["new"]; ok {
		t.Fatal("容器新增键不应出现在已发副本")
	}
}

// TestSafeMap_Snapshot_ZeroValueCopy 值类型为切片/指针时拷贝的是**浅层**
// 容器条目：调用方改副本里的值形态会穿透（与 plain map 复制同一语义）。
// 本用例钉住这条边界，防止后续有人把它误解成深拷贝。
func TestSafeMap_Snapshot_ZeroValueCopy(t *testing.T) {
	sm := NewSafeMap[string, []int]()
	sm.Set("k", []int{1})
	snap := sm.Snapshot()
	snap["k"][0] = 99
	if v, _ := sm.Get("k"); v[0] != 99 {
		t.Fatalf("Snapshot 是浅拷贝：改副本内的引用值应穿透, got %v", v)
	}
}

// TestSafeMap_Snapshot_Concurrent 并发 Set 与 Snapshot 无竞态（-race 门禁）。
func TestSafeMap_Snapshot_Concurrent(t *testing.T) {
	sm := NewSafeMap[int, int]()
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				sm.Set(w*1000+i, i)
			}
		}(w)
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_ = sm.Snapshot()
			}
		}()
	}
	wg.Wait()
	if sm.Len() != 2000 {
		t.Fatalf("并发写入后 len 应为 2000, got %d", sm.Len())
	}
}

// TestSafeMap_Snapshot_AfterClear Clear 后 Snapshot 为空。
func TestSafeMap_Snapshot_AfterClear(t *testing.T) {
	sm := NewSafeMap[string, int]()
	sm.Set("a", 1)
	sm.Clear()
	got := sm.Snapshot()
	if got == nil || len(got) != 0 {
		t.Fatalf("Clear 后 Snapshot 应为空 map, got %v", got)
	}
}
