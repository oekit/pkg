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
