package container

import (
	"sync"
	"testing"
	"time"
)

// TestSafeMap_Range_CallbackMayWrite 回归测试：Range 的回调里写同一个 map
// 不得死锁。
//
// 缺陷成因：Range 遍历期间**一直持着读锁**（RWMutex 不可重入）⇒ 回调里的
// Set 要写锁、写锁等读锁释放、读锁等回调返回 ⇒ 互等永久挂起。
//
// 附带修正：文档声称「遍历期间其他 goroutine 的写入不阻塞」，在旧实现下
// 并不成立（Set 会被读锁挡住直到遍历结束）。
func TestSafeMap_Range_CallbackMayWrite(t *testing.T) {
	sm := NewSafeMap[string, int]()
	sm.Set("a", 1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		sm.Range(func(k string, v int) bool {
			sm.Set("b", v+1) // 回调里写同一个 map
			return true
		})
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Range 回调里写同一个 map 死锁：遍历期间一直持着读锁，RWMutex 不可重入")
	}

	if _, ok := sm.Get("b"); !ok {
		t.Fatal("回调里的写入应当生效")
	}
}

// TestSafeMap_Range_ConcurrentWriteNotBlocked 遍历期间的并发写入不应被
// 读锁挡住（文档承诺）。
func TestSafeMap_Range_ConcurrentWriteNotBlocked(t *testing.T) {
	sm := NewSafeMap[string, int]()
	for i := 0; i < 100; i++ {
		sm.Set("k", i)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	go func() {
		close(entered)
		sm.Range(func(k string, v int) bool {
			<-release
			return true
		})
	}()
	<-entered

	wrote := make(chan struct{})
	go func() {
		sm.Set("other", 1) // 应当不被读锁挡住
		close(wrote)
	}()

	select {
	case <-wrote:
	case <-time.After(3 * time.Second):
		t.Fatal("遍历期间的并发写入被读锁挡住")
	}
	close(release)
}

// TestSafeMap_Range_ConcurrentWriteSafe 并发写 + 遍历不得触发 data race
// （-race 下跑）。
func TestSafeMap_Range_ConcurrentWriteSafe(t *testing.T) {
	sm := NewSafeMap[string, int]()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			sm.Set("k", i)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			sm.Range(func(k string, v int) bool { return true })
		}
	}()
	wg.Wait()
}
