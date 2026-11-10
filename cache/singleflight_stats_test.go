package cache

// SingleFlightCache 观测计数口径测试（pkg#90）：
//
// 缺陷：装饰器的前置检查（Peek）计一次未命中，内层回源加载路径再计一次
// ⇒ 一次未命中被计成两次 ⇒ 命中率指标失真（实测 90% 的真实命中率被报成
// 81.8%）。
//
// 修正：内层提供「加载不计未命中」能力（LocalCache：getOrLoadNoMiss），
// 装饰器优先走该路径 ⇒ 一次读恰好计入命中或未命中之一，与内层直连口径
// 一致。
//
// 本文件的判据一律是「装饰器口径 == 内层直连口径」——不写死期望数字，
// 免得内层口径变化时测试跟着漂。

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// statsSnapshot 是用于比对的计数快照（只取与本文件相关的两个计数）。
type statsSnapshot struct {
	Hits   int64
	Misses int64
}

func snapshot(c StatsProvider) statsSnapshot {
	s := c.Stats()
	return statsSnapshot{Hits: s.Hits, Misses: s.Misses}
}

// loader 是被测的取数入口：装饰器与内层直连各跑一遍，比对计数口径。
type loader func(t *testing.T, c loaderCache, key string) *string

// loaderCache 是被测入口的能力集合：CacheAside（GetOrLoad）+ SourceInjector
// （按调用注入回源的两个变体）——LocalCache 与 SingleFlightCache 都实现。
type loaderCache interface {
	CacheAside[string]
	SourceInjector[string]
}

// runParity 跑「装饰器 vs 内层直连」的计数口径比对：同一组操作在两种形态
// 下必须得到**完全相同**的命中/未命中计数。
func runParity(t *testing.T, name string, ops int, call loader) {
	t.Helper()

	// 形态 A：内层直连
	direct := NewLocalCache(LocalConfig[string]{TTL: 0})
	t.Cleanup(func() { _ = direct.Close(context.Background()) })

	// 形态 B：SingleFlightCache 包装内层
	inner := NewLocalCache(LocalConfig[string]{TTL: 0})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	decorated := NewSingleFlightCache[string](inner)

	for i := 0; i < ops; i++ {
		call(t, direct, "k")
	}
	for i := 0; i < ops; i++ {
		call(t, decorated, "k")
	}

	want := snapshot(direct)
	got := snapshot(inner)
	if got != want {
		t.Fatalf("%s：装饰器计数口径应与内层直连一致，direct=%+v decorated=%+v", name, want, got)
	}
}

// TestSingleFlightCache_Stats_CountParity 钉住计数口径：一次读恰好计入命中
// 或未命中之一，装饰器与内层直连一致。
//
// 覆盖三个回源变体（GetOrLoad / GetOrLoadWithSource / GetOrLoadWithSourceTTL）——
// 三条路径的前置检查与加载路径同构，缺陷同源（pkg#90 实测两个变体都中招）。
func TestSingleFlightCache_Stats_CountParity(t *testing.T) {
	src := func(context.Context, string) (*string, error) {
		v := "value"
		return &v, nil
	}

	// 1 冷 + 2 热 = 3 次查找：期望 hits=2 / misses=1
	runParity(t, "GetOrLoad", 3, func(t *testing.T, c loaderCache, key string) *string {
		v, err := c.GetOrLoad(context.Background(), key)
		if err != nil {
			t.Fatalf("GetOrLoad: %v", err)
		}
		return v
	})

	runParity(t, "GetOrLoadWithSource", 3, func(t *testing.T, c loaderCache, key string) *string {
		v, err := c.GetOrLoadWithSource(context.Background(), key, src)
		if err != nil {
			t.Fatalf("GetOrLoadWithSource: %v", err)
		}
		return v
	})

	runParity(t, "GetOrLoadWithSourceTTL", 3, func(t *testing.T, c loaderCache, key string) *string {
		v, err := c.GetOrLoadWithSourceTTL(context.Background(), key,
			func(ctx context.Context, k string) (*string, time.Duration, error) {
				v, err := src(ctx, k)
				return v, time.Minute, err
			}, 0)
		if err != nil {
			t.Fatalf("GetOrLoadWithSourceTTL: %v", err)
		}
		return v
	})
}

// TestSingleFlightCache_Stats_ColdMissNotDoubleCounted 直接钉住缺陷本体：
// 一次冷未命中只计 1 次未命中（不是 2 次）。
func TestSingleFlightCache_Stats_ColdMissNotDoubleCounted(t *testing.T) {
	inner := NewLocalCache(LocalConfig[string]{TTL: 0})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache[string](inner)

	if _, err := sf.GetOrLoad(context.Background(), "cold"); err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	s := inner.Stats()
	if s.Misses != 1 || s.Hits != 0 {
		t.Fatalf("一次冷未命中应计 1 次未命中，got %+v", s)
	}
}

// TestSingleFlightCache_Stats_HitRateNotDistorted 钉住下游可见的后果：
// 命中率不得因装饰器而失真（10 次查找 = 1 冷 + 9 热 ⇒ 命中率 90%）。
func TestSingleFlightCache_Stats_HitRateNotDistorted(t *testing.T) {
	inner := NewLocalCache(LocalConfig[string]{TTL: 0})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache[string](inner)

	src := func(context.Context, string) (*string, error) {
		v := "value"
		return &v, nil
	}
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		if _, err := sf.GetOrLoadWithSource(ctx, "k", src); err != nil {
			t.Fatalf("GetOrLoadWithSource: %v", err)
		}
	}
	s := inner.Stats()
	if s.Hits != 9 || s.Misses != 1 {
		t.Fatalf("10 次查找应计 hits=9/misses=1，got %+v", s)
	}
	total := s.Hits + s.Misses
	if total != 10 {
		t.Fatalf("10 次查找应恰好计入 10 次读，got %d", total)
	}
}

// TestSingleFlightCache_Stats_NullMarkerCountsOnce：null marker 命中算未命中
// （无值可返），且只计一次。
func TestSingleFlightCache_Stats_NullMarkerCountsOnce(t *testing.T) {
	inner := NewLocalCache(LocalConfig[string]{TTL: 0, NullTTL: time.Minute})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache[string](inner)

	// 回源返回 (nil, nil) ⇒ 缓存 null marker
	if _, err := sf.GetOrLoad(context.Background(), "null"); err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	s := inner.Stats()
	if s.Misses != 1 || s.Hits != 0 {
		t.Fatalf("null marker 回源应计 1 次未命中，got %+v", s)
	}
	// 再次读：前置检查命中 null marker（未命中），加载路径不再重复计
	if _, err := sf.GetOrLoad(context.Background(), "null"); err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	s = inner.Stats()
	if s.Misses != 2 || s.Hits != 0 {
		t.Fatalf("null marker 二次读应再计 1 次未命中，got %+v", s)
	}
}

// TestSingleFlightCache_Stats_StaleFallbackMatchesDirect：stale-if-error 兜底
// 路径的计数口径同样与内层直连一致（内层直连：一次未命中 + 一次读命中）。
func TestSingleFlightCache_Stats_StaleFallbackMatchesDirect(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }

	newCache := func() (*LocalCache[string], *SingleFlightCache[string]) {
		inner := NewLocalCache(LocalConfig[string]{
			TTL:          time.Minute,
			StaleIfError: true,
			Now:          clock,
			Source: func(context.Context, string) (*string, error) {
				v := "value"
				return &v, nil
			},
		})
		t.Cleanup(func() { _ = inner.Close(context.Background()) })
		return inner, NewSingleFlightCache[string](inner)
	}

	direct, _ := newCache()
	inner, sf := newCache()

	ctx := context.Background()
	// 先回源成功写入真值，再把时钟推过 TTL ⇒ 条目过期
	for _, c := range []CacheAside[string]{direct, sf} {
		if _, err := c.GetOrLoad(ctx, "k"); err != nil {
			t.Fatalf("GetOrLoad: %v", err)
		}
	}
	now = now.Add(2 * time.Minute)

	// 回源失败 ⇒ 走 stale 兜底
	failing := func(context.Context, string) (*string, error) {
		return nil, errors.New("source down")
	}
	if _, err := direct.GetOrLoadWithSource(ctx, "k", failing); err != nil {
		t.Fatalf("直连 stale 兜底应成功，got %v", err)
	}
	if _, err := sf.GetOrLoadWithSource(ctx, "k", failing); err != nil {
		t.Fatalf("装饰器 stale 兜底应成功，got %v", err)
	}

	want, got := snapshot(direct), snapshot(inner)
	if got != want {
		t.Fatalf("stale 兜底路径计数口径应与内层直连一致，direct=%+v decorated=%+v", want, got)
	}
}

// TestSingleFlightCache_Stats_NilValueHitCountedOnce 钉住 #203：命中**值为
// nil 的真值条目**（Set(ctx, key, nil) 存入）时，一次读恰好计一次命中——
// 不是两次。
//
// 缺陷形态：GetOrLoad 前置 Peek 命中 nil 值条目已计一次 hit，但早退条件是
// hit && value != nil，nil 值命中落入 flight.Do → 加载路径的命中分支再计
// 一次 hit ⇒ 命中率被系统性抬高（违背 pkg#90「一次读恰好计入命中或未命中
// 之一」）。
func TestSingleFlightCache_Stats_NilValueHitCountedOnce(t *testing.T) {
	inner := NewLocalCache(LocalConfig[string]{TTL: 0})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache[string](inner)

	// 显式存入 nil 真值（isNull=false、value=nil 的条目——包级契约
	// 「(nil, nil) 三种情形」的第 3 种）
	if err := sf.Set(context.Background(), "nil", nil); err != nil {
		t.Fatalf("Set: %v", err)
	}

	const reads = 5
	for i := 0; i < reads; i++ {
		v, err := sf.GetOrLoad(context.Background(), "nil")
		if err != nil {
			t.Fatalf("GetOrLoad: %v", err)
		}
		if v != nil {
			t.Fatalf("nil 真值条目应返回 nil，got %q", *v)
		}
	}
	s := inner.Stats()
	if s.Hits != reads || s.Misses != 0 {
		t.Fatalf("%d 次 nil 值命中应恰好计 %d 次命中、0 次未命中，got %+v", reads, reads, s)
	}
	if total := s.Hits + s.Misses; total != reads {
		t.Fatalf("%d 次读应恰好计入 %d 次读，got %d", reads, reads, total)
	}
}

// TestSingleFlightCache_Stats_NilValueHitCountParity：nil 值命中的计数口径
// 与内层直连一致（#203——三个回源变体同源缺陷，这里覆盖按调用注入的变体）。
func TestSingleFlightCache_Stats_NilValueHitCountParity(t *testing.T) {
	// 形态 A：内层直连
	direct := NewLocalCache(LocalConfig[string]{TTL: 0})
	t.Cleanup(func() { _ = direct.Close(context.Background()) })
	// 形态 B：装饰器
	inner := NewLocalCache(LocalConfig[string]{TTL: 0})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	decorated := NewSingleFlightCache[string](inner)

	ctx := context.Background()
	for _, c := range []CacheAside[string]{direct, decorated} {
		if err := c.Set(ctx, "nil", nil); err != nil {
			t.Fatalf("Set: %v", err)
		}
		for i := 0; i < 3; i++ {
			if _, err := c.GetOrLoad(ctx, "nil"); err != nil {
				t.Fatalf("GetOrLoad: %v", err)
			}
		}
	}

	want, got := snapshot(direct), snapshot(inner)
	if got != want {
		t.Fatalf("nil 值命中计数口径应与内层直连一致，direct=%+v decorated=%+v", want, got)
	}
}

// TestSingleFlightCache_Stats_ConcurrentMissCounted 钉住并发口径：N 个并发
// 未命中记 N 次未命中（与内层直连一致），语义同一的构造期回源仍只调用
// 一次（单飞有效）。
//
// 这是「一次读恰好计入命中或未命中之一」在并发下的口径：每个等待者的
// 前置检查各计一次未命中，回源加载路径不再重复计。
//
// 单飞断言落在 GetOrLoad（构造期 Source）上——按调用注入的路径自 pkg#353
// 起不再合并（source 不可比较，合并即串味），见
// TestSingleFlightCache_GetOrLoadWithSource_NoCrossMerge。
func TestSingleFlightCache_Stats_ConcurrentMissCounted(t *testing.T) {
	var calls atomic.Int64
	src := func(context.Context, string) (*string, error) {
		calls.Add(1)
		// 拉长窗口，让并发请求落在同一次单飞里
		time.Sleep(20 * time.Millisecond)
		v := "value"
		return &v, nil
	}

	inner := NewLocalCache(LocalConfig[string]{TTL: 0, Source: src})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache[string](inner)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := sf.GetOrLoad(context.Background(), "k"); err != nil {
				t.Errorf("GetOrLoad: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("单飞应只回源 1 次，got %d", got)
	}
	s := inner.Stats()
	if s.Misses != n || s.Hits != 0 {
		t.Fatalf("%d 次并发未命中应计 %d 次未命中，got %+v", n, n, s)
	}
}

// TestSingleFlightCache_Stats_ConcurrentWriteNotDoubleCounted 钉住 #250：
// 前置 Peek 与内层 load 自检之间有并发写入时，同一次逻辑读不得被计成
// hit+miss 两次。
//
// 缺陷形态（确定性注入）：注入时钟在第 2 次 now()——即 load 路径取时钟、
// 尚未持锁——写入 key，精确制造「前置 Peek 与内层 load 自检之间有并发
// 写入者」的窗口。前置 Peek 计 1 次 miss；内层 load 自检发现条目已存在
// 且未过期，命中分支**无条件**补计 1 次 hit ⇒ 一次读被计成两次（契约
// interface.go：「一次读恰好计入命中或未命中之一」）。pkg#90 的修复
// （countMiss=false 不计 miss）只覆盖了未命中侧，命中侧仍漏。
func TestSingleFlightCache_Stats_ConcurrentWriteNotDoubleCounted(t *testing.T) {
	ctx := context.Background()
	base := time.Now()
	v := "concurrent-write"

	var nowCalls atomic.Int64
	var injected atomic.Bool
	var inner *LocalCache[string]
	inner = NewLocalCache(LocalConfig[string]{
		TTL: time.Minute,
		Now: func() time.Time {
			// 第 1 次 now()：前置 Peek；第 2 次：内层 load 取时钟（尚未持锁）
			// ⇒ 此刻注入并发写入。Set 内部的 now() 调用由 injected 标记放行。
			if !injected.Load() && nowCalls.Add(1) == 2 {
				injected.Store(true)
				if err := inner.Set(ctx, "k", &v); err != nil {
					t.Errorf("注入写入: %v", err)
				}
			}
			return base
		},
	})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache[string](inner)

	got, err := sf.GetOrLoad(ctx, "k")
	if err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	if got == nil || *got != v {
		t.Fatalf("并发写入后的加载自检应命中新值，got %v", got)
	}
	if !injected.Load() {
		t.Fatal("注入未生效——测试自身失效")
	}

	s := inner.Stats()
	if total := s.Hits + s.Misses; total != 1 {
		t.Fatalf("契约违规：一次逻辑读应恰好计入命中或未命中之一（#250），实际 hits=%d misses=%d total=%d", s.Hits, s.Misses, total)
	}
	if s.Misses != 1 || s.Hits != 0 {
		t.Fatalf("前置 Peek 已计这次读为未命中，加载路径不得补计命中：got %+v", s)
	}
}

// TestSingleFlightCache_Stats_ConcurrentWriteStress 真实并发版 #250：一个
// goroutine 反复 GetOrLoad 冷 key，另一个并发 Set 同一 key——任何一轮出现
// 「一次读计入 hit+miss 两次」即契约违规。
func TestSingleFlightCache_Stats_ConcurrentWriteStress(t *testing.T) {
	inner := NewLocalCache(LocalConfig[string]{TTL: 0})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache[string](inner)

	ctx := context.Background()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			i++
			w := "w"
			_ = inner.Set(ctx, "k", &w)
		}
	}()

	violations := 0
	for round := 0; round < 400; round++ {
		_ = inner.Del(ctx, "k")
		before := inner.Stats()
		if _, err := sf.GetOrLoadWithSource(ctx, "k", func(context.Context, string) (*string, error) {
			x := "loaded"
			return &x, nil
		}); err != nil {
			t.Fatalf("GetOrLoadWithSource: %v", err)
		}
		after := inner.Stats()
		if dH, dM := after.Hits-before.Hits, after.Misses-before.Misses; dH+dM != 1 {
			violations++
		}
	}
	close(stop)
	wg.Wait()

	if violations > 0 {
		t.Fatalf("400 轮并发中有 %d 轮出现「一次读计入 hit+miss 两次」（#250）", violations)
	}
}
