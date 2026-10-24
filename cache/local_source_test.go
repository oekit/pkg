package cache

// LocalCache 按调用注入回源的测试（pkg#77）：
//   - GetOrLoadWithSource：注入的 source 生效，构造期 source 不被调用；
//   - source 为 nil 时退回构造期 Source/SourceWithTTL；
//   - GetOrLoadWithSourceTTL：动态 TTL 生效、显式 ttl 覆盖动态 TTL；
//   - 注入回源路径同样受 stale-if-error 保护。
//
// 契约套件（cachetest）里的对应条目见 cachetest/source.go——本文件覆盖
// LocalCache 专属的优先级细节（注入与构造期的先后）。

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestLocalCache_GetOrLoadWithSource 钉住「按调用注入回源」：
// 注入的 source 被用于回源，构造期 source 完全不被调用。
func TestLocalCache_GetOrLoadWithSource(t *testing.T) {
	var ctorCalls, injectedCalls int
	c := NewLocalCache(LocalConfig[string]{
		TTL: time.Minute,
		Source: func(ctx context.Context, key string) (*string, error) {
			ctorCalls++
			return ptr("ctor"), nil
		},
	})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	ctx := context.Background()

	got, err := c.GetOrLoadWithSource(ctx, "k", func(ctx context.Context, key string) (*string, error) {
		injectedCalls++
		return ptr("injected"), nil
	})
	if err != nil {
		t.Fatalf("GetOrLoadWithSource: %v", err)
	}
	if got == nil || *got != "injected" {
		t.Fatalf("应返回注入 source 的值，got %v", got)
	}
	if injectedCalls != 1 {
		t.Fatalf("注入 source 调用计数应恰 1，got %d", injectedCalls)
	}
	if ctorCalls != 0 {
		t.Fatalf("注入 source 生效时构造期 source 不得被调用，got %d", ctorCalls)
	}
}

// TestLocalCache_GetOrLoadWithSource_NilFallsBack 钉住 source 为 nil 时
// 退回构造期 Source（与 GetOrLoad 同口径）。
func TestLocalCache_GetOrLoadWithSource_NilFallsBack(t *testing.T) {
	var ctorCalls int
	c := NewLocalCache(LocalConfig[string]{
		TTL: time.Minute,
		Source: func(ctx context.Context, key string) (*string, error) {
			ctorCalls++
			return ptr("ctor"), nil
		},
	})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	ctx := context.Background()

	got, err := c.GetOrLoadWithSource(ctx, "k", nil)
	if err != nil {
		t.Fatalf("GetOrLoadWithSource(nil): %v", err)
	}
	if got == nil || *got != "ctor" {
		t.Fatalf("source 为 nil 应退回构造期 source，got %v", got)
	}
	if ctorCalls != 1 {
		t.Fatalf("构造期 source 调用计数应恰 1，got %d", ctorCalls)
	}
}

// TestLocalCache_GetOrLoadWithSource_HitSkipsSource 钉住命中缓存时
// 不回源（注入的 source 也不被调用）。
func TestLocalCache_GetOrLoadWithSource_HitSkipsSource(t *testing.T) {
	var injectedCalls int
	c := NewLocalCache(LocalConfig[string]{TTL: time.Minute})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	ctx := context.Background()

	if err := c.Set(ctx, "k", ptr("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := c.GetOrLoadWithSource(ctx, "k", func(ctx context.Context, key string) (*string, error) {
		injectedCalls++
		return ptr("injected"), nil
	})
	if err != nil {
		t.Fatalf("GetOrLoadWithSource: %v", err)
	}
	if got == nil || *got != "v" {
		t.Fatalf("命中缓存应返回缓存值，got %v", got)
	}
	if injectedCalls != 0 {
		t.Fatalf("命中缓存不得回源，注入 source 计数 got %d", injectedCalls)
	}
}

// TestLocalCache_GetOrLoadWithSourceTTL 钉住动态 TTL 与显式 ttl 覆盖：
//   - 注入的 source 返回的动态 TTL 决定回填窗口；
//   - 显式传入的 ttl > 0 时覆盖动态 TTL；
//   - ttl <= 0 时按动态 TTL。
func TestLocalCache_GetOrLoadWithSourceTTL(t *testing.T) {
	now := time.Now()
	c := NewLocalCache(LocalConfig[string]{
		TTL: time.Minute,
		Now: func() time.Time { return now },
	})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	ctx := context.Background()

	// 动态 TTL=30s：推进 31s 后过期
	var calls int
	if _, err := c.GetOrLoadWithSourceTTL(ctx, "dyn",
		func(ctx context.Context, key string) (*string, time.Duration, error) {
			calls++
			return ptr("v"), 30 * time.Second, nil
		}, 0); err != nil {
		t.Fatalf("GetOrLoadWithSourceTTL: %v", err)
	}
	now = now.Add(31 * time.Second)
	if _, err := c.GetOrLoadWithSourceTTL(ctx, "dyn",
		func(ctx context.Context, key string) (*string, time.Duration, error) {
			calls++
			return ptr("v"), 30 * time.Second, nil
		}, 0); err != nil {
		t.Fatalf("GetOrLoadWithSourceTTL after expiry: %v", err)
	}
	if calls != 2 {
		t.Fatalf("动态 TTL 到期后应重新回源，调用计数 want 2 got %d", calls)
	}

	// 显式 ttl=1s 覆盖动态 TTL=30s：推进 2s 后即过期
	var overrideCalls int
	if _, err := c.GetOrLoadWithSourceTTL(ctx, "override",
		func(ctx context.Context, key string) (*string, time.Duration, error) {
			overrideCalls++
			return ptr("v"), 30 * time.Second, nil
		}, time.Second); err != nil {
		t.Fatalf("GetOrLoadWithSourceTTL override: %v", err)
	}
	now = now.Add(2 * time.Second)
	if _, err := c.GetOrLoadWithSourceTTL(ctx, "override",
		func(ctx context.Context, key string) (*string, time.Duration, error) {
			overrideCalls++
			return ptr("v"), 30 * time.Second, nil
		}, time.Second); err != nil {
		t.Fatalf("GetOrLoadWithSourceTTL override after expiry: %v", err)
	}
	if overrideCalls != 2 {
		t.Fatalf("显式 ttl 应覆盖动态 TTL，调用计数 want 2 got %d", overrideCalls)
	}
}

// TestLocalCache_GetOrLoadWithSource_StaleIfError 钉住注入回源路径同样受
// stale-if-error 保护：回源失败且存在过期真值时回退陈旧值（fail-open）。
func TestLocalCache_GetOrLoadWithSource_StaleIfError(t *testing.T) {
	now := time.Now()
	c := NewLocalCache(LocalConfig[string]{
		TTL:          time.Minute,
		Now:          func() time.Time { return now },
		StaleIfError: true,
	})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	ctx := context.Background()

	if err := c.Set(ctx, "k", ptr("old")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	now = now.Add(2 * time.Minute) // 过期，但条目留在表内（stale 兜底要用）

	got, err := c.GetOrLoadWithSource(ctx, "k", func(ctx context.Context, key string) (*string, error) {
		return nil, errors.New("upstream down")
	})
	if err != nil {
		t.Fatalf("stale 兜底应吞掉回源错误，got %v", err)
	}
	if got == nil || *got != "old" {
		t.Fatalf("stale 兜底应返回历史真值，got %v", got)
	}
}
