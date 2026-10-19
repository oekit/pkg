package cache_test

// cache 契约套件接入点。放外部测试包（package cache_test）而非内部包：
// cachetest 反向 import cache（被测接口），内部测试包再 import cachetest
// 会构成 import cycle；外部测试包允许同时 import 两者。
//
// 后续新增 CacheAside 后端（etcd/consul/...，可能在他仓）直接 import
// github.com/oekit/pkg/cache/cachetest 跑 Run 即可，无需复刻本文件。

import (
	"context"
	"testing"
	"time"

	"github.com/oekit/pkg/cache"
	cachetest "github.com/oekit/pkg/cache/cachetest"
)

// TestContract_LocalCache 跑 cachetest 契约套件（LocalCache 直连）。
// 通用语义（miss (nil,nil)、往返/覆盖、显式 TTL、Del 幂等、回填计数、
// null marker、Refresh、并发混合）由套件固化，本包不再重复编写。
func TestContract_LocalCache(t *testing.T) {
	cachetest.Run(t, cachetest.Comparable[int](), func(t *testing.T, source cache.SourceFunc[int]) cache.CacheAside[int] {
		c := cache.NewLocalCache(cache.LocalConfig[int]{TTL: 5 * time.Minute, Source: source})
		t.Cleanup(func() { _ = c.Close(context.Background()) })
		return c
	})
}

// TestContract_SingleFlightCache_LocalCache 跑契约套件
// （SingleFlightCache 包装 LocalCache 组合场景——装饰器必须透明满足同一契约）。
func TestContract_SingleFlightCache_LocalCache(t *testing.T) {
	cachetest.Run(t, cachetest.Comparable[int](), func(t *testing.T, source cache.SourceFunc[int]) cache.CacheAside[int] {
		inner := cache.NewLocalCache(cache.LocalConfig[int]{TTL: 5 * time.Minute, Source: source})
		t.Cleanup(func() { _ = inner.Close(context.Background()) })
		return cache.NewSingleFlightCache(inner)
	})
}

// TestContract_LocalCache_Extras 跑 pkg#25 的可选能力契约（手动清扫、
// 命中语义、读命中返回副本、读命中提升顺序、无后台 goroutine）——
// 这些是 LocalCache 显式提供的能力，套件硬断言（能力缺失即红）。
func TestContract_LocalCache_Extras(t *testing.T) {
	// 手动清扫（无后台件，由调用方显式回收）
	cachetest.RunSweepWithClock(t, cachetest.Comparable[string](),
		func(t *testing.T, source cache.SourceFunc[string], clock *cachetest.FakeClock) cache.CacheAside[string] {
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

	// 命中语义（GetEx）
	cachetest.RunHitWithClock(t, cachetest.Comparable[string](),
		func(t *testing.T, source cache.SourceFunc[string], clock *cachetest.FakeClock) cache.CacheAside[string] {
			c := cache.NewLocalCache(cache.LocalConfig[string]{
				TTL:     30 * time.Second,
				NullTTL: time.Minute,
				Source:  source,
				Now:     clock.Now,
			})
			t.Cleanup(func() { _ = c.Close(context.Background()) })
			return c
		})

	// 读命中返回副本
	cachetest.RunReadIsolation(t, cachetest.Comparable[string](),
		func(t *testing.T, source cache.SourceFunc[string]) cache.CacheAside[string] {
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

	// 读命中提升顺序（LRU，容量 3）
	cachetest.RunReadPromotion(t, cachetest.Comparable[string](), 3,
		func(t *testing.T, source cache.SourceFunc[string]) cache.CacheAside[string] {
			c := cache.NewLocalCache(cache.LocalConfig[string]{
				TTL:         5 * time.Minute,
				MaxSize:     3,
				EvictPolicy: cache.EvictLRU,
				Source:      source,
			})
			t.Cleanup(func() { _ = c.Close(context.Background()) })
			return c
		})

	// 无后台 goroutine
	cachetest.RunNoBackgroundGoroutine(t,
		func(t *testing.T, source cache.SourceFunc[string]) cache.CacheAside[string] {
			c := cache.NewLocalCache(cache.LocalConfig[string]{
				TTL:               5 * time.Minute,
				Source:            source,
				NoBackgroundSweep: true,
			})
			t.Cleanup(func() { _ = c.Close(context.Background()) })
			return c
		})
}
