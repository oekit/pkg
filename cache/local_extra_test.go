package cache

// LocalCache 可选行为测试（pkg#25）：
//   - 无后台清扫 goroutine（NoBackgroundSweep）与公开清扫口 Sweep；
//   - 读命中返回副本（CloneOnRead）；
//   - 淘汰策略可选（EvictFIFO 缺省 / EvictLRU 读命中也提升顺序）；
//   - Get 的 nil 语义（GetEx 区分「不存在」与「有值但值为 nil」）。
//
// 契约套件（cachetest）里的对应条目见 cachetest/extras.go——本文件覆盖
// LocalCache 专属的配置面（开关组合与缺省行为）。

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

// TestLocalCache_NoBackgroundSweep_NoGoroutine 无后台清扫模式：构造前后
// 进程内 goroutine 数不变。对照组证明该检测看得见后台协程（缺省配置
// TTL>0 会起一个清扫协程）——避免「断言恒真」的假绿。
func TestLocalCache_NoBackgroundSweep_NoGoroutine(t *testing.T) {
	ctx := context.Background()

	// 无后台件：构造 5 个实例，goroutine 数不得增加
	before := runtime.NumGoroutine()
	quiet := make([]*LocalCache[string], 0, 5)
	for i := 0; i < 5; i++ {
		quiet = append(quiet, NewLocalCache(LocalConfig[string]{
			TTL:               5 * time.Minute,
			NoBackgroundSweep: true,
		}))
	}
	runtime.Gosched()
	after := runtime.NumGoroutine()
	if after > before {
		t.Fatalf("无后台清扫时构造不得新增 goroutine：before=%d after=%d", before, after)
	}
	// Close 仍安全且幂等（无后台件时也不 panic）
	for _, c := range quiet {
		if err := c.Close(ctx); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if err := c.Close(ctx); err != nil {
			t.Fatalf("重复 Close 应幂等: %v", err)
		}
	}

	// 对照组：缺省配置（后台清扫开）确实新增 goroutine
	before2 := runtime.NumGoroutine()
	noisy := make([]*LocalCache[string], 0, 5)
	for i := 0; i < 5; i++ {
		noisy = append(noisy, NewLocalCache(LocalConfig[string]{TTL: 5 * time.Minute}))
	}
	runtime.Gosched()
	after2 := runtime.NumGoroutine()
	if after2-before2 < len(noisy) {
		t.Fatalf("对照组：缺省配置应各起一个后台清扫 goroutine，before=%d after=%d", before2, after2)
	}
	for _, c := range noisy {
		_ = c.Close(ctx)
	}
}

// TestLocalCache_Sweep 公开清扫口：手动 Sweep 清掉过期条目并返回条数；
// 未过期条目保留；重复清扫返回 0（幂等）。
func TestLocalCache_Sweep(t *testing.T) {
	clock := &fakeNowClock{t: nowTestStart}
	c := NewLocalCache(LocalConfig[string]{
		TTL:               30 * time.Second,
		Now:               clock.Now,
		NoBackgroundSweep: true,
	})
	ctx := context.Background()
	t.Cleanup(func() { _ = c.Close(ctx) })

	if err := c.SetWithTTL(ctx, "e1", ptr("v1"), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	if err := c.SetWithTTL(ctx, "e2", ptr("v2"), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	if err := c.SetWithTTL(ctx, "alive", ptr("v3"), 0); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}

	// 未过期时清扫：0 条
	if n := c.Sweep(ctx); n != 0 {
		t.Fatalf("未过期时 Sweep 应返回 0，got %d", n)
	}

	clock.Advance(31 * time.Second)
	if n := c.Sweep(ctx); n != 2 {
		t.Fatalf("Sweep 应清除 2 条过期条目，got %d", n)
	}
	if got, err := c.Get(ctx, "e1"); err != nil || got != nil {
		t.Fatalf("清扫后 e1 应 miss，got (%v, %v)", got, err)
	}
	if got, err := c.Get(ctx, "alive"); err != nil || got == nil || *got != "v3" {
		t.Fatalf("清扫不得动未过期条目，got (%v, %v)", got, err)
	}
	if c.Size() != 1 {
		t.Fatalf("Size = %d, want 1", c.Size())
	}
	// 幂等：再扫一次没有可清的
	if n := c.Sweep(ctx); n != 0 {
		t.Fatalf("重复 Sweep 应返回 0，got %d", n)
	}
	// ctx 已取消时不执行清扫
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := c.SetWithTTL(ctx, "e3", ptr("v4"), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(31 * time.Second)
	if n := c.Sweep(cancelled); n != 0 {
		t.Fatalf("ctx 已取消时 Sweep 应返回 0，got %d", n)
	}
}

// TestLocalCache_CloneOnRead 读命中返回副本（pkg#25）：
//   - 开启 CloneOnRead：调用方改写返回值不影响缓存里的那份；
//   - 缺省（nil）：保持零拷贝——返回的就是缓存内部那一份（共享引用）。
//
// 覆盖读路径：Get / GetOrLoad 命中 / GetOrLoad stale / MGet。
func TestLocalCache_CloneOnRead(t *testing.T) {
	ctx := context.Background()
	clock := &fakeNowClock{t: nowTestStart}

	clone := func(v *string) *string {
		cp := *v
		return &cp
	}
	c := NewLocalCache(LocalConfig[string]{
		TTL:         30 * time.Second,
		Now:         clock.Now,
		CloneOnRead: clone,
	})
	t.Cleanup(func() { _ = c.Close(ctx) })

	// 1. Get：改写返回值不影响缓存
	v := "orig"
	if err := c.Set(ctx, "k", &v); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := c.Get(ctx, "k")
	if err != nil || got == nil {
		t.Fatalf("Get: (%v, %v)", got, err)
	}
	if got == &v {
		t.Fatal("开启 CloneOnRead 后 Get 应返回副本，不得是同一指针")
	}
	*got = "mutated"
	again, err := c.Get(ctx, "k")
	if err != nil || again == nil || *again != "orig" {
		t.Fatalf("CloneOnRead：改写返回值不得污染缓存内部，got (%v, %v)", again, err)
	}

	// 2. GetOrLoad 命中路径：改写返回值不影响缓存
	if serr := c.Set(ctx, "k2", ptr("orig2")); serr != nil {
		t.Fatalf("Set: %v", serr)
	}
	hit, err := c.GetOrLoad(ctx, "k2")
	if err != nil || hit == nil {
		t.Fatalf("GetOrLoad: (%v, %v)", hit, err)
	}
	*hit = "mutated2"
	if again, gerr := c.Get(ctx, "k2"); gerr != nil || again == nil || *again != "orig2" {
		t.Fatalf("GetOrLoad 命中路径应返回副本，got (%v, %v)", again, gerr)
	}

	// 3. MGet 批量路径：改写返回值不影响缓存
	if serr := c.Set(ctx, "k3", ptr("orig3")); serr != nil {
		t.Fatalf("Set: %v", serr)
	}
	batch, err := c.MGet(ctx, "k3")
	if err != nil || len(batch) != 1 || batch["k3"] == nil {
		t.Fatalf("MGet: (%v, %v)", batch, err)
	}
	*batch["k3"] = "mutated3"
	if again, gerr := c.Get(ctx, "k3"); gerr != nil || again == nil || *again != "orig3" {
		t.Fatalf("MGet 应返回副本，got (%v, %v)", again, gerr)
	}

	// 4. GetOrLoad stale 路径：改写返回值不影响缓存里的过期真值
	if serr := c.SetWithTTL(ctx, "k4", ptr("orig4"), 30*time.Second); serr != nil {
		t.Fatalf("SetWithTTL: %v", serr)
	}
	clock.Advance(31 * time.Second)
	c.cfg.Source = func(ctx context.Context, key string) (*string, error) {
		return nil, errors.New("source down")
	}
	c.cfg.StaleIfError = true
	stale, err := c.GetOrLoad(ctx, "k4")
	if err != nil || stale == nil || *stale != "orig4" {
		t.Fatalf("stale 兜底 = (%v, %v), want orig4", stale, err)
	}
	*stale = "mutated4"
	if entry, ok := c.data["k4"]; !ok || entry.value == nil || *entry.value != "orig4" {
		t.Fatalf("stale 路径应返回副本：缓存内部真值不得被改写，got %v", entry)
	}

	// 5. CloneOnRead 返回 nil 时视为不克隆（返回原值）
	c2 := NewLocalCache(LocalConfig[string]{
		TTL: 30 * time.Second,
		Now: clock.Now,
		CloneOnRead: func(v *string) *string {
			return nil
		},
	})
	t.Cleanup(func() { _ = c2.Close(ctx) })
	v2 := "orig5"
	if serr := c2.Set(ctx, "k5", &v2); serr != nil {
		t.Fatalf("Set: %v", serr)
	}
	got2, err := c2.Get(ctx, "k5")
	if err != nil || got2 == nil {
		t.Fatalf("Get: (%v, %v)", got2, err)
	}
	if got2 != &v2 {
		t.Fatal("CloneOnRead 返回 nil 时应视为不克隆，返回原值")
	}
}

// TestLocalCache_CloneOnRead_Disabled 缺省行为不变：未配置 CloneOnRead 时
// Get 返回缓存内部那一份（零拷贝共享引用）。
func TestLocalCache_CloneOnRead_Disabled(t *testing.T) {
	ctx := context.Background()
	c := NewLocalCache(LocalConfig[string]{TTL: 30 * time.Second})
	t.Cleanup(func() { _ = c.Close(ctx) })

	v := "orig"
	if err := c.Set(ctx, "k", &v); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := c.Get(ctx, "k")
	if err != nil || got == nil {
		t.Fatalf("Get: (%v, %v)", got, err)
	}
	if got != &v {
		t.Fatal("缺省应保持零拷贝：Get 返回缓存内部共享引用")
	}
}

// TestLocalCache_EvictPolicy_LRU LRU 策略（pkg#25）：读命中也提升顺序——
// 容量压力下热点 key 不被淘汰，最久未读到的先淘汰。
// 同一场景在缺省 FIFO 下热点 key 必被淘汰（两组断言对照）。
func TestLocalCache_EvictPolicy_LRU(t *testing.T) {
	ctx := context.Background()

	// LRU
	lru := NewLocalCache(LocalConfig[string]{
		MaxSize:     3,
		EvictPolicy: EvictLRU,
	})
	t.Cleanup(func() { _ = lru.Close(ctx) })

	for _, k := range []string{"a", "b", "c"} {
		if err := lru.Set(ctx, k, ptr(k)); err != nil {
			t.Fatalf("Set %s: %v", k, err)
		}
	}
	// 反复读命中最旧的 key（提升顺序）
	for i := 0; i < 3; i++ {
		if _, err := lru.Get(ctx, "a"); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}
	if err := lru.Set(ctx, "d", ptr("d")); err != nil {
		t.Fatalf("Set d: %v", err)
	}
	if got, err := lru.Get(ctx, "a"); err != nil || got == nil {
		t.Fatalf("LRU：读命中的热点 key 不应被淘汰，got (%v, %v)", got, err)
	}
	if got, err := lru.Get(ctx, "b"); err != nil || got != nil {
		t.Fatalf("LRU：最久未读到的 b 应被淘汰，got (%v, %v)", got, err)
	}

	// 缺省 FIFO：读命中不提升——热点 key 被淘汰
	fifo := NewLocalCache(LocalConfig[string]{MaxSize: 3})
	t.Cleanup(func() { _ = fifo.Close(ctx) })
	for _, k := range []string{"a", "b", "c"} {
		if err := fifo.Set(ctx, k, ptr(k)); err != nil {
			t.Fatalf("Set %s: %v", k, err)
		}
	}
	for i := 0; i < 3; i++ {
		if _, err := fifo.Get(ctx, "a"); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}
	if err := fifo.Set(ctx, "d", ptr("d")); err != nil {
		t.Fatalf("Set d: %v", err)
	}
	if got, err := fifo.Get(ctx, "a"); err != nil || got != nil {
		t.Fatalf("FIFO：读命中不提升，热点 key 应被淘汰，got (%v, %v)", got, err)
	}
}

// TestLocalCache_EvictPolicy_LRU_MGet LRU 下批量读命中同样提升顺序。
func TestLocalCache_EvictPolicy_LRU_MGet(t *testing.T) {
	ctx := context.Background()
	c := NewLocalCache(LocalConfig[string]{MaxSize: 3, EvictPolicy: EvictLRU})
	t.Cleanup(func() { _ = c.Close(ctx) })

	for _, k := range []string{"a", "b", "c"} {
		if err := c.Set(ctx, k, ptr(k)); err != nil {
			t.Fatalf("Set %s: %v", k, err)
		}
	}
	if _, err := c.MGet(ctx, "a"); err != nil {
		t.Fatalf("MGet: %v", err)
	}
	if err := c.Set(ctx, "d", ptr("d")); err != nil {
		t.Fatalf("Set d: %v", err)
	}
	if got, err := c.Get(ctx, "a"); err != nil || got == nil {
		t.Fatalf("LRU：MGet 命中的 key 同样应提升顺序，got (%v, %v)", got, err)
	}
	if got, err := c.Get(ctx, "b"); err != nil || got != nil {
		t.Fatalf("LRU：最久未读到的 b 应被淘汰，got (%v, %v)", got, err)
	}
}

// TestLocalCache_GetEx Get 的 nil 语义（pkg#25）：GetEx 区分「不存在」
// 与「有值但值为 nil」；既有 Get 签名与行为不变。
func TestLocalCache_GetEx(t *testing.T) {
	ctx := context.Background()
	clock := &fakeNowClock{t: nowTestStart}
	c := NewLocalCache(LocalConfig[string]{
		TTL: 30 * time.Second,
		Now: clock.Now,
		Source: func(ctx context.Context, key string) (*string, error) {
			return nil, nil // null marker
		},
	})
	t.Cleanup(func() { _ = c.Close(ctx) })

	// 1. 不存在
	if v, hit, err := c.GetEx(ctx, "missing"); err != nil || hit || v != nil {
		t.Fatalf("不存在应返回 (nil, false, nil)，got (%v, %v, %v)", v, hit, err)
	}

	// 2. 命中显式 nil 值（真值条目，值为 nil）——Get 无法区分
	if err := c.Set(ctx, "nil-value", nil); err != nil {
		t.Fatalf("Set(nil): %v", err)
	}
	if v, hit, err := c.GetEx(ctx, "nil-value"); err != nil || !hit || v != nil {
		t.Fatalf("显式 nil 值应命中 (nil, true, nil)，got (%v, %v, %v)", v, hit, err)
	}
	if got, err := c.Get(ctx, "nil-value"); err != nil || got != nil {
		t.Fatalf("Get 语义不变：(nil, nil)，got (%v, %v)", got, err)
	}

	// 3. 命中真值
	if err := c.Set(ctx, "value", ptr("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, hit, err := c.GetEx(ctx, "value"); err != nil || !hit || v == nil || *v != "v" {
		t.Fatalf("真值应命中 (v, true, nil)，got (%v, %v, %v)", v, hit, err)
	}

	// 4. null marker：按未命中处理（数据不存在）
	if _, err := c.GetOrLoad(ctx, "null"); err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	if v, hit, err := c.GetEx(ctx, "null"); err != nil || hit || v != nil {
		t.Fatalf("null marker 应按未命中处理 (nil, false, nil)，got (%v, %v, %v)", v, hit, err)
	}

	// 5. 过期：按未命中处理
	if err := c.SetWithTTL(ctx, "ttl", ptr("v"), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(31 * time.Second)
	if v, hit, err := c.GetEx(ctx, "ttl"); err != nil || hit || v != nil {
		t.Fatalf("过期应按未命中处理 (nil, false, nil)，got (%v, %v, %v)", v, hit, err)
	}
}

// TestLocalCache_Peek 非删除式读取（pkg#72）：与 Get 的唯一差别是**读到过期
// 条目时不删除它**——过期真值留在表内，stale-if-error 兜底才有值可退。
//
// 对照点：Get 读同一过期 key 会惰性删除（条目从表内消失），Peek 不删。
func TestLocalCache_Peek(t *testing.T) {
	ctx := context.Background()
	clock := &fakeNowClock{t: nowTestStart}
	c := NewLocalCache(LocalConfig[string]{
		TTL: 30 * time.Second,
		Now: clock.Now,
		Source: func(ctx context.Context, key string) (*string, error) {
			return nil, nil // null marker
		},
	})
	t.Cleanup(func() { _ = c.Close(ctx) })

	// 1. 不存在
	if v, hit, err := c.Peek(ctx, "missing"); err != nil || hit || v != nil {
		t.Fatalf("不存在应返回 (nil, false, nil)，got (%v, %v, %v)", v, hit, err)
	}

	// 2. 命中真值
	if err := c.Set(ctx, "value", ptr("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, hit, err := c.Peek(ctx, "value"); err != nil || !hit || v == nil || *v != "v" {
		t.Fatalf("真值应命中 (v, true, nil)，got (%v, %v, %v)", v, hit, err)
	}

	// 3. 命中显式 nil 值（真值条目，值为 nil）——与 GetEx 同口径
	if err := c.Set(ctx, "nil-value", nil); err != nil {
		t.Fatalf("Set(nil): %v", err)
	}
	if v, hit, err := c.Peek(ctx, "nil-value"); err != nil || !hit || v != nil {
		t.Fatalf("显式 nil 值应命中 (nil, true, nil)，got (%v, %v, %v)", v, hit, err)
	}

	// 4. null marker：按未命中处理
	if _, err := c.GetOrLoad(ctx, "null"); err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	if v, hit, err := c.Peek(ctx, "null"); err != nil || hit || v != nil {
		t.Fatalf("null marker 应按未命中处理 (nil, false, nil)，got (%v, %v, %v)", v, hit, err)
	}

	// 5. 过期条目：Peek 不命中，但**条目留在表内**（不被删除）
	if err := c.SetWithTTL(ctx, "ttl", ptr("v"), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(31 * time.Second)
	sizeBefore := c.Size()
	if v, hit, err := c.Peek(ctx, "ttl"); err != nil || hit || v != nil {
		t.Fatalf("过期应按未命中处理 (nil, false, nil)，got (%v, %v, %v)", v, hit, err)
	}
	if got := c.Size(); got != sizeBefore {
		t.Fatalf("Peek 不得删除过期条目：Size %d → %d", sizeBefore, got)
	}

	// 对照：Get 读同一过期 key 会惰性删除（条目从表内消失）
	if _, err := c.Get(ctx, "ttl"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := c.Size(); got != sizeBefore-1 {
		t.Fatalf("Get 读过期条目应惰性删除：Size %d → %d（want %d）", sizeBefore, got, sizeBefore-1)
	}
}
