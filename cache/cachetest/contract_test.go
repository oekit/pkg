package cachetest

import (
	"context"
	"testing"
	"time"

	"github.com/oekit/pkg/cache"
)

// TestRun_LocalCache 套件自证（常驻冒烟）：用 LocalCache 跑完整契约场景，
// 同时保证本套件自身不被破坏——实现包接入契约时套件必然可用。
func TestRun_LocalCache(t *testing.T) {
	Run(t, Comparable[int](), func(t *testing.T, source cache.SourceFunc[int]) cache.CacheAside[int] {
		c := cache.NewLocalCache(cache.LocalConfig[int]{
			TTL:    5 * time.Minute,
			Source: source,
		})
		t.Cleanup(func() { _ = c.Close(context.Background()) })
		return c
	})
}

// TestRun_LocalCache_String 覆盖字符串值类型（Comparable 的反射枚举路径）。
func TestRun_LocalCache_String(t *testing.T) {
	Run(t, Comparable[string](), func(t *testing.T, source cache.SourceFunc[string]) cache.CacheAside[string] {
		c := cache.NewLocalCache(cache.LocalConfig[string]{
			TTL:    5 * time.Minute,
			Source: source,
		})
		t.Cleanup(func() { _ = c.Close(context.Background()) })
		return c
	})
}

// TestRunWithClock_LocalCache 假时钟 TTL 套件自证（#9）：LocalCache 经
// LocalConfig.Now 接入，4 个确定性边界场景无 sleep 通过。
func TestRunWithClock_LocalCache(t *testing.T) {
	RunWithClock(t, Comparable[string](), func(t *testing.T, source cache.SourceFunc[string], clock *FakeClock) cache.CacheAside[string] {
		c := cache.NewLocalCache(cache.LocalConfig[string]{
			TTL:     30 * time.Second,
			NullTTL: time.Minute,
			Source:  source,
			Now:     clock.Now,
		})
		t.Cleanup(func() { _ = c.Close(context.Background()) })
		return c
	})
}

// ---------- pkg#25 可选能力契约自证 ----------

// TestRunSweep_LocalCache 手动清扫契约：LocalCache 提供 Sweep(ctx) int
// （NoBackgroundSweep 关闭后台件，由调用方显式回收过期条目）。
func TestRunSweep_LocalCache(t *testing.T) {
	RunSweepWithClock(t, Comparable[string](), func(t *testing.T, source cache.SourceFunc[string], clock *FakeClock) cache.CacheAside[string] {
		c := cache.NewLocalCache(cache.LocalConfig[string]{
			TTL:               30 * time.Second,
			NullTTL:           time.Minute,
			Source:            source,
			Now:               clock.Now,
			NoBackgroundSweep: true,
		})
		t.Cleanup(func() { _ = c.Close(context.Background()) })
		return c
	})
}

// TestRunHit_LocalCache 命中语义契约：LocalCache 提供 GetEx
// （区分「不存在」与「有值但值为 nil」）。
func TestRunHit_LocalCache(t *testing.T) {
	RunHitWithClock(t, Comparable[string](), func(t *testing.T, source cache.SourceFunc[string], clock *FakeClock) cache.CacheAside[string] {
		c := cache.NewLocalCache(cache.LocalConfig[string]{
			TTL:     30 * time.Second,
			NullTTL: time.Minute,
			Source:  source,
			Now:     clock.Now,
		})
		t.Cleanup(func() { _ = c.Close(context.Background()) })
		return c
	})
}

// TestRunReadIsolation_LocalCache 读命中返回副本契约：LocalCache 经
// LocalConfig.CloneOnRead 开启副本模式。
func TestRunReadIsolation_LocalCache(t *testing.T) {
	RunReadIsolation(t, Comparable[string](), func(t *testing.T, source cache.SourceFunc[string]) cache.CacheAside[string] {
		c := cache.NewLocalCache(cache.LocalConfig[string]{
			TTL:    5 * time.Minute,
			Source: source,
			CloneOnRead: func(v *string) *string {
				cp := *v
				return &cp
			},
		})
		t.Cleanup(func() { _ = c.Close(context.Background()) })
		return c
	})
}

// TestRunReadPromotion_LocalCache 读命中提升顺序契约：LocalCache 经
// LocalConfig.EvictPolicy=EvictLRU + MaxSize 构造（容量 3）。
func TestRunReadPromotion_LocalCache(t *testing.T) {
	RunReadPromotion(t, Comparable[string](), 3, func(t *testing.T, source cache.SourceFunc[string]) cache.CacheAside[string] {
		c := cache.NewLocalCache(cache.LocalConfig[string]{
			TTL:         5 * time.Minute,
			MaxSize:     3,
			EvictPolicy: cache.EvictLRU,
			Source:      source,
		})
		t.Cleanup(func() { _ = c.Close(context.Background()) })
		return c
	})
}

// TestRunNoBackgroundGoroutine_LocalCache 无后台 goroutine 契约：
// LocalCache 经 LocalConfig.NoBackgroundSweep 关闭后台清扫协程。
func TestRunNoBackgroundGoroutine_LocalCache(t *testing.T) {
	RunNoBackgroundGoroutine(t, func(t *testing.T, source cache.SourceFunc[string]) cache.CacheAside[string] {
		c := cache.NewLocalCache(cache.LocalConfig[string]{
			TTL:               5 * time.Minute,
			Source:            source,
			NoBackgroundSweep: true,
		})
		t.Cleanup(func() { _ = c.Close(context.Background()) })
		return c
	})
}
