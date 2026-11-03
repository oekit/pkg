package cachetest

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/oekit/pkg/cache"
)

// 本文件是 pkg#25 的可选能力契约套件：手动清扫（Sweep）、命中语义
// （GetEx）、读命中返回副本（CloneOnRead）、读命中提升顺序（LRU）、
// 无后台 goroutine（NoBackgroundSweep）。
//
// 与 Run/RunWithClock 的区别：那些是所有后端都必须满足的通用契约；
// 本文件里的场景需要实现**显式提供**对应能力（通过可选能力接口或
// 构造参数），因此套件入口一律**硬失败**而不是静默跳过——显式调用
// 即视为声明该能力，能力缺失或行为不符都必须红，避免"假绿"。
//
// 场景断言都是真实的：每个场景都构造出「不满足契约就一定会失败」的
// 状态（例如 LRU 场景在 FIFO 实现下热点 key 必被淘汰）。

// RunSweepWithClock 跑「手动清扫」契约（pkg#25）。
//
// 实现须提供 cache.Sweeper（Sweep(ctx) int）——未提供即判失败。
//
// 场景：两条 TTL 条目 + 一条 TTL=0（永不过期）条目 → 推进时钟越过 TTL
// → Sweep 恰好清除 2 条、未过期条目保留 → 再扫一次返回 0（幂等）。
//
// 前提：场景窗口（毫秒级）内实现不得自行回收过期条目——LocalCache 的
// 后台清扫节奏为 TTL/2（真实时间 15s），远大于场景耗时。
func RunSweepWithClock[T any](t *testing.T, c Contract[T], factory ClockFactory[T]) {
	t.Helper()

	clock := NewFakeClock()
	cc := factory(t, unusedSource[T](t), clock)
	sweeper, ok := cc.(cache.Sweeper)
	if !ok {
		t.Fatalf("实现须提供 cache.Sweeper 能力（Sweep(ctx) int）")
	}
	ctx := context.Background()

	if err := cc.SetWithTTL(ctx, "sweep-expired-1", c.NewValue(0), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	if err := cc.SetWithTTL(ctx, "sweep-expired-2", c.NewValue(1), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	if err := cc.SetWithTTL(ctx, "sweep-alive", c.NewValue(2), 0); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}

	clock.Advance(31 * time.Second)

	removed := sweeper.Sweep(ctx)
	if removed != 2 {
		t.Fatalf("Sweep 应清除 2 条过期条目，got %d", removed)
	}
	for _, key := range []string{"sweep-expired-1", "sweep-expired-2"} {
		if got, err := cc.Get(ctx, key); err != nil || got != nil {
			t.Fatalf("清扫后 %s 应 miss，got (%v, %v)", key, got, err)
		}
	}
	if got, err := cc.Get(ctx, "sweep-alive"); err != nil || !c.Equal(got, c.NewValue(2)) {
		t.Fatalf("清扫不得动未过期条目，got (%v, %v)", got, err)
	}
	if again := sweeper.Sweep(ctx); again != 0 {
		t.Fatalf("重复清扫应返回 0（幂等），got %d", again)
	}
}

// RunHitWithClock 跑「命中语义」契约（pkg#25）。
//
// 实现须提供 cache.HitGetter（GetEx(ctx) (*T, bool, error)）——未提供
// 即判失败。
//
// 覆盖五种情形：
//
//  1. key 不存在       → (nil, false, nil)
//  2. 命中 null marker → (nil, false, nil)（null marker 表示数据不存在）
//  3. 命中显式 nil 值 → (nil, true, nil)（有值但值为 nil——Get 无法区分）
//  4. 命中真值         → (value, true, nil)
//  5. 条目已过期       → (nil, false, nil)
//
// 前提：实现须能存储显式 nil 值（本地实现天然支持；序列化型实现须定义
// nil 值的编码——本场景对未声明该能力的实现不适用）。
func RunHitWithClock[T any](t *testing.T, c Contract[T], factory ClockFactory[T]) {
	t.Helper()

	clock := NewFakeClock()
	cc := factory(t, func(ctx context.Context, key string) (*T, error) {
		if key == "hit-null" {
			return nil, nil // null marker
		}
		return c.NewValue(0), nil
	}, clock)
	hitter, ok := cc.(cache.HitGetter[T])
	if !ok {
		t.Fatalf("实现须提供 cache.HitGetter 能力（GetEx(ctx) (*T, bool, error)）")
	}
	ctx := context.Background()

	// 1. 不存在
	if v, hit, err := hitter.GetEx(ctx, "hit-miss"); err != nil || hit || v != nil {
		t.Fatalf("不存在应返回 (nil, false, nil)，got (%v, %v, %v)", v, hit, err)
	}

	// 2. null marker（数据不存在）
	if _, err := cc.GetOrLoad(ctx, "hit-null"); err != nil {
		t.Fatalf("GetOrLoad(null): %v", err)
	}
	if v, hit, err := hitter.GetEx(ctx, "hit-null"); err != nil || hit || v != nil {
		t.Fatalf("null marker 应按未命中处理 (nil, false, nil)，got (%v, %v, %v)", v, hit, err)
	}

	// 3. 显式 nil 值（真值条目，值为 nil）
	if err := cc.Set(ctx, "hit-nil", nil); err != nil {
		t.Fatalf("Set(nil): %v", err)
	}
	if v, hit, err := hitter.GetEx(ctx, "hit-nil"); err != nil || !hit || v != nil {
		t.Fatalf("显式 nil 值应命中 (nil, true, nil)，got (%v, %v, %v)", v, hit, err)
	}

	// 4. 真值
	want := c.NewValue(1)
	if err := cc.Set(ctx, "hit-value", want); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, hit, err := hitter.GetEx(ctx, "hit-value"); err != nil || !hit || !c.Equal(v, want) {
		t.Fatalf("真值应命中且相等，got (%v, %v, %v)", v, hit, err)
	}

	// 5. 过期
	if err := cc.SetWithTTL(ctx, "hit-ttl", c.NewValue(2), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(31 * time.Second)
	if v, hit, err := hitter.GetEx(ctx, "hit-ttl"); err != nil || hit || v != nil {
		t.Fatalf("过期应按未命中处理 (nil, false, nil)，got (%v, %v, %v)", v, hit, err)
	}
}

// RunReadIsolation 跑「读命中返回副本」契约（pkg#25）。
//
// factory 须构造「读命中返回副本」模式的缓存（LocalCache：
// LocalConfig.CloneOnRead 开启；序列化型后端天然满足）。
//
// 断言：调用方改写 Get 返回值后，再次 Get 必须拿到原始值——缓存内部
// 条目未被污染。期望值用**独立值副本**保存（不与缓存内条目共享指针），
// 避免「改写返回值时把期望值一起改掉」造成的假绿。
func RunReadIsolation[T any](t *testing.T, c Contract[T], factory func(t *testing.T, source cache.SourceFunc[T]) cache.CacheAside[T]) {
	t.Helper()

	cc := factory(t, unusedSource[T](t))
	ctx := context.Background()

	original := *c.NewValue(7)
	if err := cc.Set(ctx, "iso", &original); err != nil {
		t.Fatalf("Set: %v", err)
	}
	expected := original // 独立值副本（值拷贝，不共享指针）

	got, err := cc.Get(ctx, "iso")
	if err != nil || got == nil {
		t.Fatalf("Get: (%v, %v)", got, err)
	}
	// 调用方改写返回值（共享引用实现会污染缓存内部）
	*got = *c.NewValue(8)

	got2, err := cc.Get(ctx, "iso")
	if err != nil || got2 == nil {
		t.Fatalf("Get again: (%v, %v)", got2, err)
	}
	if !c.Equal(got2, &expected) {
		t.Fatalf("读命中应返回副本：调用方改写返回值不得影响缓存内部，got %v", got2)
	}
}

// RunReadPromotion 跑「读命中提升顺序」契约（pkg#25）。
//
// factory 须构造「读命中提升顺序（LRU）」且容量为 capacity 的缓存
// （LocalCache：LocalConfig.EvictPolicy=EvictLRU + MaxSize=capacity）。
//
// 断言：写满容量 → 反复读最旧的 key（提升顺序）→ 再写一条触发淘汰
// → 热点 key 仍存活；且确有条目被淘汰（证明场景真的触发了淘汰，
// 不是因为压根没写满而"通过"）。
//
// 同一场景在 FIFO 实现下热点 key 必被淘汰——即该场景能区分两种策略。
func RunReadPromotion[T any](t *testing.T, c Contract[T], capacity int, factory func(t *testing.T, source cache.SourceFunc[T]) cache.CacheAside[T]) {
	t.Helper()
	if capacity < 2 {
		t.Fatalf("capacity 至少为 2，got %d", capacity)
	}

	cc := factory(t, unusedSource[T](t))
	ctx := context.Background()

	key := func(i int) string { return fmt.Sprintf("promote-%d", i) }
	for i := 0; i < capacity; i++ {
		if err := cc.Set(ctx, key(i), c.NewValue(i)); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	// 读命中最旧的 key（提升顺序）
	for i := 0; i < 3; i++ {
		if _, err := cc.Get(ctx, key(0)); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}
	// 写入新条目触发淘汰
	if err := cc.Set(ctx, "promote-new", c.NewValue(100)); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if v, err := cc.Get(ctx, key(0)); err != nil || v == nil {
		t.Fatalf("读命中的热点 key 不应被淘汰，got (%v, %v)", v, err)
	}
	alive := 0
	for i := 0; i < capacity; i++ {
		if ok, err := cc.Exists(ctx, key(i)); err == nil && ok {
			alive++
		}
	}
	if alive >= capacity {
		t.Fatalf("容量压力下应有条目被淘汰（alive=%d, capacity=%d）", alive, capacity)
	}
}

// RunNoBackgroundGoroutine 跑「不起后台 goroutine」契约（pkg#25）。
//
// 构造缓存不得在进程内新增 goroutine——短命命令行/宿主内嵌场景不该
// 为缓存常驻后台协程（LocalCache：LocalConfig.NoBackgroundSweep）。
//
// 断言：构造前后 runtime.NumGoroutine() 不得增加。
func RunNoBackgroundGoroutine[T any](t *testing.T, factory func(t *testing.T, source cache.SourceFunc[T]) cache.CacheAside[T]) {
	t.Helper()

	before := runtime.NumGoroutine()
	factory(t, unusedSource[T](t))
	runtime.Gosched()
	after := runtime.NumGoroutine()
	if after > before {
		t.Fatalf("构造缓存不应新增后台 goroutine：before=%d after=%d", before, after)
	}
}

// RunSetNXWithClock 跑「不存在才写入」契约（NXSetter）。
//
// 实现须提供 cache.NXSetter（SetNX(ctx, key, value, ttl) (bool, error)）
// ——未提供即判失败。
//
// 每个情形独立实例 + 独立假时钟（时间推进互不串扰）。每种情形都构造出
// 「不满足契约就一定失败」的状态：
//
//  1. 先到先得：空键 SetNX 成功；同键再来失败且原值不变；
//  2. nil 真值占位：SetNX(key, nil) 成功后同键 SetNX 失败——键占位是 NX
//     的核心语义（Redis 对空值键的 SET NX 同样未命中），值为 nil 也占位；
//  3. 过期条目按不存在：TTL 条目过期后同键 SetNX 成功并以新值覆盖；
//  4. null marker 按不存在：GetOrLoad 缓存的「已知无值」标记可被 SetNX
//     真值覆盖，随后 Get 拿到的是真值；
//  5. 失败零副作用：失败的 SetNX 不覆盖值、**不刷新 TTL**（写入 30s、
//     10s 后失败一次，31s 时必须已过期——失败写若重置/刷新了过期时点，
//     此处仍命中即红）；
//  6. ttl<=0 不过期：写入后大幅推进时钟仍在。
func RunSetNXWithClock[T any](t *testing.T, c Contract[T], factory ClockFactory[T]) {
	t.Helper()
	ctx := context.Background()

	nxOf := func(cc cache.CacheAside[T]) cache.NXSetter[T] {
		nx, ok := cc.(cache.NXSetter[T])
		if !ok {
			t.Fatalf("实现须提供 cache.NXSetter 能力（SetNX(ctx, key, value, ttl) (bool, error)）")
		}
		return nx
	}

	// 1. 先到先得 + 失败不覆盖
	{
		clock := NewFakeClock()
		cc := factory(t, unusedSource[T](t), clock)
		nx := nxOf(cc)
		if ok, err := nx.SetNX(ctx, "nx-first", c.NewValue(1), 0); err != nil || !ok {
			t.Fatalf("空键 SetNX 应成功，got (%v, %v)", ok, err)
		}
		if ok, err := nx.SetNX(ctx, "nx-first", c.NewValue(2), 0); err != nil || ok {
			t.Fatalf("同键 SetNX 应失败，got (%v, %v)", ok, err)
		}
		if v, err := cc.Get(ctx, "nx-first"); err != nil || !c.Equal(v, c.NewValue(1)) {
			t.Fatalf("失败的 SetNX 不得覆盖原值，got (%v, %v)", v, err)
		}
	}

	// 2. nil 真值占位
	{
		clock := NewFakeClock()
		cc := factory(t, unusedSource[T](t), clock)
		nx := nxOf(cc)
		if ok, err := nx.SetNX(ctx, "nx-nil", nil, 0); err != nil || !ok {
			t.Fatalf("空键 SetNX(nil) 应成功，got (%v, %v)", ok, err)
		}
		if ok, err := nx.SetNX(ctx, "nx-nil", c.NewValue(2), 0); err != nil || ok {
			t.Fatalf("nil 真值条目应占位（同键 SetNX 失败），got (%v, %v)", ok, err)
		}
	}

	// 3. 过期条目按不存在
	{
		clock := NewFakeClock()
		cc := factory(t, unusedSource[T](t), clock)
		nx := nxOf(cc)
		if ok, err := nx.SetNX(ctx, "nx-exp", c.NewValue(3), 30*time.Second); err != nil || !ok {
			t.Fatalf("空键 SetNX 应成功，got (%v, %v)", ok, err)
		}
		clock.Advance(31 * time.Second)
		if ok, err := nx.SetNX(ctx, "nx-exp", c.NewValue(4), 0); err != nil || !ok {
			t.Fatalf("过期条目按不存在，SetNX 应成功，got (%v, %v)", ok, err)
		}
		if v, err := cc.Get(ctx, "nx-exp"); err != nil || !c.Equal(v, c.NewValue(4)) {
			t.Fatalf("应以新值覆盖过期条目，got (%v, %v)", v, err)
		}
	}

	// 4. null marker 按不存在（真值覆盖「已知无值」标记）
	{
		clock := NewFakeClock()
		cc := factory(t, func(_ context.Context, key string) (*T, error) {
			if key == "nx-marker" {
				return nil, nil // 回源声明「无值」⇒ 缓存 null marker
			}
			return c.NewValue(0), nil
		}, clock)
		nx := nxOf(cc)
		if v, err := cc.GetOrLoad(ctx, "nx-marker"); err != nil || v != nil {
			t.Fatalf("GetOrLoad(nx-marker) 应得 (nil, nil)，got (%v, %v)", v, err)
		}
		if ok, err := nx.SetNX(ctx, "nx-marker", c.NewValue(5), 0); err != nil || !ok {
			t.Fatalf("null marker 按不存在，SetNX 应成功，got (%v, %v)", ok, err)
		}
		if v, err := cc.Get(ctx, "nx-marker"); err != nil || !c.Equal(v, c.NewValue(5)) {
			t.Fatalf("真值应覆盖 null marker，got (%v, %v)", v, err)
		}
	}

	// 5. 失败零副作用（不覆盖、不刷新 TTL）
	{
		clock := NewFakeClock()
		cc := factory(t, unusedSource[T](t), clock)
		nx := nxOf(cc)
		if ok, err := nx.SetNX(ctx, "nx-side", c.NewValue(5), 30*time.Second); err != nil || !ok {
			t.Fatalf("空键 SetNX 应成功，got (%v, %v)", ok, err)
		}
		clock.Advance(10 * time.Second)
		if ok, err := nx.SetNX(ctx, "nx-side", c.NewValue(6), 30*time.Second); err != nil || ok {
			t.Fatalf("同键 SetNX 应失败，got (%v, %v)", ok, err)
		}
		if v, err := cc.Get(ctx, "nx-side"); err != nil || !c.Equal(v, c.NewValue(5)) {
			t.Fatalf("失败的 SetNX 不得覆盖原值，got (%v, %v)", v, err)
		}
		clock.Advance(21 * time.Second) // 距写入共 31s > 30s TTL
		if v, err := cc.Get(ctx, "nx-side"); err != nil || v != nil {
			t.Fatalf("失败的 SetNX 不得刷新 TTL（31s 后应过期），got (%v, %v)", v, err)
		}
	}

	// 6. ttl<=0 不过期
	{
		clock := NewFakeClock()
		cc := factory(t, unusedSource[T](t), clock)
		nx := nxOf(cc)
		if ok, err := nx.SetNX(ctx, "nx-nottl", c.NewValue(7), 0); err != nil || !ok {
			t.Fatalf("空键 SetNX 应成功，got (%v, %v)", ok, err)
		}
		clock.Advance(24 * time.Hour)
		if v, err := cc.Get(ctx, "nx-nottl"); err != nil || !c.Equal(v, c.NewValue(7)) {
			t.Fatalf("ttl<=0 应不过期，got (%v, %v)", v, err)
		}
	}
}
