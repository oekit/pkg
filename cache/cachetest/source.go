package cachetest

// 本文件是 pkg#77 的「按调用注入回源」契约套件：SourceInjector 能力
// （GetOrLoadWithSource / GetOrLoadWithSourceTTL）。
//
// 显式调用即视为声明该能力 ⇒ 能力缺失硬失败（不静默跳过，避免"假绿"）。

import (
	"context"
	"testing"
	"time"

	"github.com/oekit/pkg/cache"
)

// RunSourceInjectionWithClock 跑「按调用注入回源」契约（pkg#77）。
//
// 实现须提供 cache.SourceInjector——未提供即判失败。
//
// 覆盖五点：
//
//  1. 按调用注入的 source 被用于回源（构造期 source 不被调用）；
//  2. 命中缓存时不回源（注入的 source 也不被调用）；
//  3. 注入的 source 为 nil 时退回构造期 source；
//  4. 注入的动态 TTL 生效（回源给出的 TTL 决定回填窗口）；
//  5. 显式传入的 ttl 覆盖回源给出的动态 TTL。
//
// 场景 4/5 需要推进时钟才能观察 TTL 窗口，故用 ClockFactory。
func RunSourceInjectionWithClock[T any](t *testing.T, c Contract[T], factory ClockFactory[T]) {
	t.Helper()

	clock := NewFakeClock()
	var ctorCalls int
	ctorValue := c.NewValue(0)
	cc := factory(t, func(ctx context.Context, key string) (*T, error) {
		ctorCalls++
		return ctorValue, nil
	}, clock)
	si, ok := cc.(cache.SourceInjector[T])
	if !ok {
		t.Fatalf("实现须提供 cache.SourceInjector 能力（GetOrLoadWithSource 等）")
	}
	ctx := context.Background()

	// 1. 按调用注入的 source 被用于回源
	injectedValue := c.NewValue(1)
	var injectedCalls int
	got, err := si.GetOrLoadWithSource(ctx, "inject-1", func(ctx context.Context, key string) (*T, error) {
		injectedCalls++
		return injectedValue, nil
	})
	if err != nil {
		t.Fatalf("GetOrLoadWithSource: %v", err)
	}
	if got == nil || !c.Equal(got, injectedValue) {
		t.Fatalf("应返回注入 source 的值，got %+v", got)
	}
	if injectedCalls != 1 {
		t.Fatalf("注入 source 调用计数应恰 1，got %d", injectedCalls)
	}
	if ctorCalls != 0 {
		t.Fatalf("注入 source 生效时构造期 source 不应被调用，got %d", ctorCalls)
	}

	// 2. 命中缓存时不回源（注入的 source 也不被调用）
	got, err = si.GetOrLoadWithSource(ctx, "inject-1", func(ctx context.Context, key string) (*T, error) {
		injectedCalls++
		return injectedValue, nil
	})
	if err != nil {
		t.Fatalf("GetOrLoadWithSource second: %v", err)
	}
	if got == nil || !c.Equal(got, injectedValue) {
		t.Fatalf("第二次应命中缓存，got %+v", got)
	}
	if injectedCalls != 1 {
		t.Fatalf("命中缓存不应回源：注入 source 计数 want 1 got %d", injectedCalls)
	}

	// 3. 注入的 source 为 nil 时退回构造期 source
	got, err = si.GetOrLoadWithSource(ctx, "inject-2", nil)
	if err != nil {
		t.Fatalf("GetOrLoadWithSource(nil): %v", err)
	}
	if got == nil || !c.Equal(got, ctorValue) {
		t.Fatalf("source 为 nil 应退回构造期 source 的值，got %+v", got)
	}
	if ctorCalls != 1 {
		t.Fatalf("构造期 source 调用计数应恰 1，got %d", ctorCalls)
	}

	// 4. 注入的动态 TTL 生效：回源给出 30s ⇒ 推进 31s 后过期并重新回源
	var ttlCalls int
	ttlValue := c.NewValue(2)
	if _, err := si.GetOrLoadWithSourceTTL(ctx, "inject-ttl",
		func(ctx context.Context, key string) (*T, time.Duration, error) {
			ttlCalls++
			return ttlValue, 30 * time.Second, nil
		}, 0); err != nil {
		t.Fatalf("GetOrLoadWithSourceTTL: %v", err)
	}
	if ttlCalls != 1 {
		t.Fatalf("动态 TTL source 调用计数应恰 1，got %d", ttlCalls)
	}
	clock.Advance(31 * time.Second)
	if _, err := si.GetOrLoadWithSourceTTL(ctx, "inject-ttl",
		func(ctx context.Context, key string) (*T, time.Duration, error) {
			ttlCalls++
			return ttlValue, 30 * time.Second, nil
		}, 0); err != nil {
		t.Fatalf("GetOrLoadWithSourceTTL after expiry: %v", err)
	}
	if ttlCalls != 2 {
		t.Fatalf("动态 TTL 到期后应重新回源：调用计数 want 2 got %d", ttlCalls)
	}

	// 5. 显式传入的 ttl 覆盖回源给出的动态 TTL：回源给 30s、显式给 1s
	//    ⇒ 推进 2s 后即过期
	var overrideCalls int
	overrideValue := c.NewValue(3)
	if _, err := si.GetOrLoadWithSourceTTL(ctx, "inject-override",
		func(ctx context.Context, key string) (*T, time.Duration, error) {
			overrideCalls++
			return overrideValue, 30 * time.Second, nil
		}, time.Second); err != nil {
		t.Fatalf("GetOrLoadWithSourceTTL override: %v", err)
	}
	clock.Advance(2 * time.Second)
	if _, err := si.GetOrLoadWithSourceTTL(ctx, "inject-override",
		func(ctx context.Context, key string) (*T, time.Duration, error) {
			overrideCalls++
			return overrideValue, 30 * time.Second, nil
		}, time.Second); err != nil {
		t.Fatalf("GetOrLoadWithSourceTTL override after expiry: %v", err)
	}
	if overrideCalls != 2 {
		t.Fatalf("显式 ttl 应覆盖回源给出的动态 TTL：调用计数 want 2 got %d", overrideCalls)
	}
}
