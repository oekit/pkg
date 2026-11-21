package cache

// SingleFlightCache 能力转发与共享回源生命周期测试（#566）：
//   - GetEx/MGetEx/Sweep/Close 穿透装饰器（内层具备能力时不得退化）；
//   - MGet 退化路径保留 nil 真值命中的键（BatchGetter 契约）；
//   - GetOrLoad 共享回源跑在 WithoutCancel + 有界超时上：leader ctx 取消
//     不把同批 waiter 连坐失败。

import (
	"context"
	"sync"
	"testing"
	"time"
)

// hitOnlyInner 是**只**实现 HitGetter、不实现 BatchGetter/BatchHitGetter
// 的内层替身：钉住装饰器 MGet/MGetEx 的逐键退化路径仍能区分 nil 真值命中。
type hitOnlyInner struct {
	*noPeekInner
}

func (h *hitOnlyInner) GetEx(_ context.Context, key string) (*string, bool, error) {
	v, ok := h.values[key]
	return v, ok, nil
}

var _ HitGetter[string] = (*hitOnlyInner)(nil)

// closeRecorderInner 记录 Close 是否被转发到内层。
type closeRecorderInner struct {
	*noPeekInner
	closed bool
}

func (c *closeRecorderInner) Close(context.Context) error {
	c.closed = true
	return nil
}

// TestSingleFlightCache_GetExMGetExForwarding 钉住 #566：装饰器补齐
// HitGetter/BatchHitGetter——包装 LocalCache 后 GetEx/MGetEx 仍能区分
// 「命中 nil 真值」与「缺席」；MGet 不丢 nil 真值键。
func TestSingleFlightCache_GetExMGetExForwarding(t *testing.T) {
	ctx := context.Background()
	inner := NewLocalCache(LocalConfig[string]{TTL: time.Minute})
	t.Cleanup(func() { _ = inner.Close(ctx) })
	sf := NewSingleFlightCache(inner)

	if err := sf.Set(ctx, "nil-value", nil); err != nil {
		t.Fatalf("Set nil 真值: %v", err)
	}
	if err := sf.Set(ctx, "value", ptr("v")); err != nil {
		t.Fatalf("Set value: %v", err)
	}

	// GetEx：nil 真值命中 hit=true（Get 的 (nil, nil) 无法区分）。
	if v, hit, err := sf.GetEx(ctx, "nil-value"); err != nil || !hit || v != nil {
		t.Fatalf("GetEx(nil 真值) = (%v, %v, %v)，want (nil, true, nil)", v, hit, err)
	}
	if v, hit, err := sf.GetEx(ctx, "missing"); err != nil || hit || v != nil {
		t.Fatalf("GetEx(缺席) = (%v, %v, %v)，want (nil, false, nil)", v, hit, err)
	}
	if v, hit, err := sf.GetEx(ctx, "value"); err != nil || !hit || v == nil || *v != "v" {
		t.Fatalf("GetEx(真值) = (%v, %v, %v)，want (v, true, nil)", v, hit, err)
	}

	// MGetEx：对每个请求键各给一条，nil 真值命中 Hit=true 且 Value=nil。
	entries, err := sf.MGetEx(ctx, "value", "nil-value", "missing")
	if err != nil {
		t.Fatalf("MGetEx: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("MGetEx 应对每个请求键各给一条，got %d 条: %+v", len(entries), entries)
	}
	if e := entries["value"]; !e.Hit || e.Value == nil || *e.Value != "v" {
		t.Fatalf("MGetEx(value) = %+v，want Hit=true v", e)
	}
	if e := entries["nil-value"]; !e.Hit || e.Value != nil {
		t.Fatalf("MGetEx(nil 真值) = %+v，want Hit=true Value=nil", e)
	}
	if e := entries["missing"]; e.Hit || e.Value != nil {
		t.Fatalf("MGetEx(缺席) = %+v，want Hit=false", e)
	}

	// MGet：内层具备 BatchGetter，直接委托——nil 真值键在场（值为 nil）。
	m, err := sf.MGet(ctx, "value", "nil-value", "missing")
	if err != nil {
		t.Fatalf("MGet: %v", err)
	}
	if _, ok := m["nil-value"]; !ok {
		t.Fatalf("MGet 丢掉了 nil 真值命中的键（键必须在场、值为 nil）: %+v", m)
	}
	if v, ok := m["value"]; !ok || v == nil || *v != "v" {
		t.Fatalf("MGet(value) = %v, ok=%v", v, ok)
	}
	if _, ok := m["missing"]; ok {
		t.Fatalf("MGet 不应包含缺席键: %+v", m)
	}
}

// TestSingleFlightCache_MGetFallbackKeepsNilTruth 钉住 #566 的退化路径：
// 内层只有 HitGetter（无批量能力）时，MGet 逐键 GetEx 仍保留 nil 真值键，
// 不再被 `if v != nil` 过滤掉。
func TestSingleFlightCache_MGetFallbackKeepsNilTruth(t *testing.T) {
	ctx := context.Background()
	inner := &hitOnlyInner{noPeekInner: &noPeekInner{values: map[string]*string{
		"nil-value": nil,
		"value":     ptr("v"),
	}}}
	sf := NewSingleFlightCache[string](inner)

	m, err := sf.MGet(ctx, "nil-value", "value", "missing")
	if err != nil {
		t.Fatalf("MGet: %v", err)
	}
	if _, ok := m["nil-value"]; !ok {
		t.Fatalf("退化路径应保留 nil 真值命中的键: %+v", m)
	}
	if v, ok := m["value"]; !ok || v == nil || *v != "v" {
		t.Fatalf("MGet(value) = %v, ok=%v", v, ok)
	}
	if _, ok := m["missing"]; ok {
		t.Fatalf("不应包含缺席键: %+v", m)
	}

	// MGetEx 的逐键退化同样能区分 nil 真值。
	entries, err := sf.MGetEx(ctx, "nil-value", "missing")
	if err != nil {
		t.Fatalf("MGetEx: %v", err)
	}
	if e := entries["nil-value"]; !e.Hit {
		t.Fatalf("MGetEx 退化路径应保留 nil 真值命中: %+v", e)
	}
	if e := entries["missing"]; e.Hit {
		t.Fatalf("MGetEx 退化路径不应把缺席判成命中: %+v", e)
	}
}

// TestSingleFlightCache_SweepCloseForwarding 钉住 #566：Sweep/Close 穿透
// 装饰器；内层缺失能力时按既有缺失口径（Sweep 返回 0、Close 返回 nil）——
// 不能静默丢掉手动清扫，也不能让内层后台 goroutine 不可停止。
func TestSingleFlightCache_SweepCloseForwarding(t *testing.T) {
	ctx := context.Background()
	clock := &fakeNowClock{t: nowTestStart}
	inner := newNowTestCache(t, clock, func(cfg *LocalConfig[string]) {
		cfg.NoBackgroundSweep = true
	})
	sf := NewSingleFlightCache(inner)

	if err := sf.Set(ctx, "expired", ptr("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	clock.Advance(31 * time.Second)
	if n := sf.Sweep(ctx); n != 1 {
		t.Fatalf("Sweep 应穿透装饰器清掉 1 条过期条目，got %d", n)
	}
	if n := sf.Sweep(ctx); n != 0 {
		t.Fatalf("Sweep 幂等，二次应为 0，got %d", n)
	}

	// 内层无可清扫能力：返回 0（能力缺失是内层的事实）。
	if n := NewSingleFlightCache[string](&noPeekInner{values: map[string]*string{}}).Sweep(ctx); n != 0 {
		t.Fatalf("内层无 Sweeper 时应返回 0，got %d", n)
	}

	// Close 转发到内层。
	rec := &closeRecorderInner{noPeekInner: &noPeekInner{values: map[string]*string{}}}
	if err := NewSingleFlightCache[string](rec).Close(ctx); err != nil || !rec.closed {
		t.Fatalf("Close 应穿透装饰器转发到内层，closed=%v err=%v", rec.closed, err)
	}
	// 内层无 Close：返回 nil（装饰器自身无资源）。
	if err := NewSingleFlightCache[string](&noPeekInner{values: map[string]*string{}}).Close(ctx); err != nil {
		t.Fatalf("内层无 Close 时应返回 nil，got %v", err)
	}
}

// TestSingleFlightCache_GetOrLoadLeaderCancelDoesNotFailWaiter 钉住 #566：
// 共享回源跑在 WithoutCancel + 有界超时上。leader 的 ctx 在回源中被取消，
// 回源本身不受影响（取值保留、deadline 存在），同批 waiter 拿到正常结果
// ——旧实现把 leader ctx 直接交给 Source，取消后全员共享失败、缓存不回填。
func TestSingleFlightCache_GetOrLoadLeaderCancelDoesNotFailWaiter(t *testing.T) {
	type ctxKey struct{}
	type observed struct {
		err         error
		hasDeadline bool
		remaining   time.Duration
		value       any
	}

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	observedCh := make(chan observed, 4) // 足够容纳回源重入（防测试实现自身阻塞）

	inner := NewLocalCache(LocalConfig[string]{
		TTL: time.Minute,
		Source: func(ctx context.Context, key string) (*string, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			deadline, hasDeadline := ctx.Deadline()
			remaining := time.Duration(0)
			if hasDeadline {
				remaining = time.Until(deadline)
			}
			observedCh <- observed{
				err:         ctx.Err(),
				hasDeadline: hasDeadline,
				remaining:   remaining,
				value:       ctx.Value(ctxKey{}),
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return ptr("shared"), nil
		},
	})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache(inner)

	leaderCtx, cancelLeader := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "kept"))
	var wg sync.WaitGroup
	var leaderVal *string
	var leaderErr error
	wg.Go(func() {
		leaderVal, leaderErr = sf.GetOrLoad(leaderCtx, "k")
	})
	<-started // leader 已在 Source 里

	var waiterVal *string
	var waiterErr error
	wg.Go(func() {
		waiterVal, waiterErr = sf.GetOrLoad(context.Background(), "k")
	})
	// 给 waiter 时间进入 flight 等待（leader 仍卡在 release 前）。
	time.Sleep(20 * time.Millisecond)

	cancelLeader() // leader 断开/超时
	close(release) // 放行共享回源
	wg.Wait()

	obs := <-observedCh
	if obs.err != nil {
		t.Fatalf("共享回源被 leader 取消连坐（应跑在 WithoutCancel 上）: %v", obs.err)
	}
	if !obs.hasDeadline {
		t.Fatal("共享回源应有有界超时（deadline）")
	}
	if obs.remaining <= 0 || obs.remaining > sharedLoadTimeout {
		t.Fatalf("共享回源剩余预算 = %v，应在 (0, %v] 内", obs.remaining, sharedLoadTimeout)
	}
	if obs.value != "kept" {
		t.Fatalf("WithoutCancel 应保留 ctx 取值，got %v", obs.value)
	}

	if leaderErr != nil || leaderVal == nil || *leaderVal != "shared" {
		t.Fatalf("leader 应拿到共享回源结果，got (%v, %v)", leaderVal, leaderErr)
	}
	if waiterErr != nil || waiterVal == nil || *waiterVal != "shared" {
		t.Fatalf("waiter 不应被 leader 取消连坐，got (%v, %v)", waiterVal, waiterErr)
	}
}
