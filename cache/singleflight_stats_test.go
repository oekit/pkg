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

// TestSingleFlightCache_Stats_ConcurrentMissCounted 钉住并发口径：N 个并发
// 未命中记 N 次未命中（与内层直连一致），回源仍只调用一次（单飞有效）。
//
// 这是「一次读恰好计入命中或未命中之一」在并发下的口径：每个等待者的
// 前置检查各计一次未命中，回源加载路径不再重复计。
func TestSingleFlightCache_Stats_ConcurrentMissCounted(t *testing.T) {
	var calls atomic.Int64
	src := func(context.Context, string) (*string, error) {
		calls.Add(1)
		// 拉长窗口，让并发请求落在同一次单飞里
		time.Sleep(20 * time.Millisecond)
		v := "value"
		return &v, nil
	}

	inner := NewLocalCache(LocalConfig[string]{TTL: 0})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache[string](inner)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := sf.GetOrLoadWithSource(context.Background(), "k", src); err != nil {
				t.Errorf("GetOrLoadWithSource: %v", err)
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
