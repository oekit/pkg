package pii

// 订阅快照的可读性与单调性（issue #296）。
//
// # 这组用例补的是哪条缝
//
// 包头与 Subscribe 都承诺「收到即所见」：回调参数就是那一刻的完整策略快照，
// 写侧后续换策略不影响它。但 Policy 的字段此前全部非导出、也没有任何方法，
// 订阅者在回调里**读不出任何字段**——只能转身去调包级访问器拿「此刻」的
// 全局值，而那可能已经是下一代。包头的承诺于是不可兑现，且没有任何用例
// 能钉住它（读都读不到，断言无从写起）。
//
// 修法：给 Policy 加一组只读访问方法（无 setter，CurrentPolicy 仍是值返回），
// 让回调参数真正可读；同时把并发通知按代次收成单调：订阅者不会先看到
// 新一代、再看到旧一代。

import (
	"sync"
	"testing"
	"time"
)

// TestPolicySnapshotExposesReadOnlyState 回调参数必须能读出整份策略。
func TestPolicySnapshotExposesReadOnlyState(t *testing.T) {
	restore(t)
	if err := setDigestKeyForTest(t, "0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	surfaces, err := ParseSurfaces("input", "output")
	if err != nil {
		t.Fatal(err)
	}
	setSurfacesForTest(t, surfaces)
	if err := setStrategyForTest(t, StrategyHash); err != nil {
		t.Fatal(err)
	}
	setEnabledForTest(t, false)

	got := CurrentPolicy()
	if got.Gen() != Gen() {
		t.Fatalf("快照代次 %d != 包级 Gen %d", got.Gen(), Gen())
	}
	if got.Enabled() != Enabled() {
		t.Fatalf("快照 Enabled %v != 包级 %v", got.Enabled(), Enabled())
	}
	if got.Surfaces() != Surfaces() {
		t.Fatalf("快照 Surfaces %v != 包级 %v", got.Surfaces(), Surfaces())
	}
	if !got.SurfaceEnabled(SurfaceInput) || got.SurfaceEnabled(SurfaceToolResults) {
		t.Fatalf("快照面开关不可读或读错: %v", got.Surfaces())
	}
	if got.Strategy() != StrategyHash {
		t.Fatalf("快照策略 = %q, want hash", got.Strategy())
	}
	if !got.DigestKeyed() {
		t.Fatal("快照应报告已注入摘要密钥")
	}
}

// TestSubscribeCallbackSnapshotIsReadable 订阅回调里的参数必须是**那一代**，
// 而不是一个读不出东西的哑值；写入方后续换策略不得改写它手里的快照。
func TestSubscribeCallbackSnapshotIsReadable(t *testing.T) {
	restore(t)
	if err := setStrategyForTest(t, StrategyMask); err != nil {
		t.Fatal(err)
	}

	type seen struct {
		gen      uint64
		strategy StrategyKind
		surfaces SurfaceSet
	}
	ch := make(chan seen, 8)
	h, err := Subscribe(func(p Policy) {
		ch <- seen{gen: p.Gen(), strategy: p.Strategy(), surfaces: p.Surfaces()}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer Unsubscribe(h)

	first := <-ch // 注册即回调
	if first.strategy != StrategyMask {
		t.Fatalf("立即回调应读到当时策略 mask，实得 %q", first.strategy)
	}
	if first.gen == 0 {
		t.Fatal("立即回调的快照必须有代次")
	}

	// 换策略：回调参数必须跟着换，且代次前进。
	if err := setStrategyForTest(t, StrategyRedact); err != nil {
		t.Fatal(err)
	}
	second := <-ch
	if second.strategy != StrategyRedact {
		t.Fatalf("变更通知应读到 redact，实得 %q", second.strategy)
	}
	if second.gen <= first.gen {
		t.Fatalf("代次必须前进：%d → %d", first.gen, second.gen)
	}

	// 同一份快照在写侧继续换策略后仍逐字不变（值语义，不可被就地改）。
	snapshot := CurrentPolicy()
	snapshotGen, snapshotStrategy := snapshot.Gen(), snapshot.Strategy()
	if err := setStrategyForTest(t, StrategyBlock); err != nil {
		t.Fatal(err)
	}
	if snapshot.Gen() != snapshotGen || snapshot.Strategy() != snapshotStrategy {
		t.Fatalf("写侧换策略改写了已取出的快照：gen %d→%d strategy %q→%q",
			snapshotGen, snapshot.Gen(), snapshotStrategy, snapshot.Strategy())
	}
}

// TestSubscriberObservesMonotonicGenerations 并发换策略时，订阅者观察到的
// 代次必须单调不回退（issue #296 的「待坐实」项）。
//
// 通知按写侧 CAS 落地顺序发出，但并发写侧可能在 CAS 之后被抢占：旧一代的
// notify 晚于新一代的 notify 到达。修前没有任何机制阻止这种倒序，订阅者会
// 先看到 gen N+1 再看到 gen N——「收到即所见」当场变成「收到的是旧的」。
func TestSubscriberObservesMonotonicGenerations(t *testing.T) {
	restore(t)

	var (
		mu       sync.Mutex
		last     uint64
		updates  int
		monotone = true
	)
	h, err := Subscribe(func(p Policy) {
		mu.Lock()
		defer mu.Unlock()
		if p.Gen() < last {
			monotone = false
			t.Errorf("代次回退：看到 gen %d 在 gen %d 之后", p.Gen(), last)
			return
		}
		last = p.Gen()
		updates++
	})
	if err != nil {
		t.Fatal(err)
	}
	defer Unsubscribe(h)

	const writers, perWriter = 8, 200
	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				if i%2 == 0 {
					_ = setStrategyForTest(t, StrategyMask)
				} else {
					_ = setStrategyForTest(t, StrategyRedact)
				}
				setSurfacesForTest(t, SurfaceSet(j%3)+1)
			}
		}(i)
	}
	wg.Wait()

	// 收尾的通知在写侧同步调用链上完成；这里再等一小段，吸收「最后一个
	// notify 的 pending 由前一个投递者收尾」的窗口。
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		done := monotone && updates > 1 && last >= Gen()
		mu.Unlock()
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if !monotone {
		t.Fatal("订阅者观察到代次回退")
	}
	if updates < 2 {
		t.Fatalf("并发换策略应产生通知，实得 %d 次", updates)
	}
}

// TestSubscribeInsideCallbackDoesNotSelfLock 回调里再 Subscribe 不得自锁
// （订阅者对「策略子集变化」再挂一层是自然写法）。
func TestSubscribeInsideCallbackDoesNotSelfLock(t *testing.T) {
	restore(t)
	done := make(chan struct{})
	var once sync.Once
	outer, err := Subscribe(func(p Policy) {
		once.Do(func() {
			h, err := Subscribe(func(inner Policy) {
				_ = inner.Strategy()
				_ = p.Strategy()
			})
			if err != nil {
				t.Errorf("回调内 Subscribe 失败: %v", err)
				close(done)
				return
			}
			Unsubscribe(h)
			close(done)
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer Unsubscribe(outer)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("回调内 Subscribe 自锁")
	}
	if err := setStrategyForTest(t, StrategyRedact); err != nil {
		t.Fatal(err)
	}
}
