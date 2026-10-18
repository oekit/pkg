package cachetest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/oekit/pkg/cache"
)

// FakeClock 是套件提供的假时钟（并发安全；测试内手动推进）。
// 实现把 clock.Now 注入配置后，场景经 Advance 确定性驱动 TTL 过期——
// 无 sleep 无 flake（#9）。
type FakeClock struct {
	mu sync.Mutex
	t  time.Time
}

// NewFakeClock 创建起点为 2026-01-01T00:00:00Z 的假时钟
// （取非零真实量级时刻，避开零值 time.Time 的「无过期」歧义）。
func NewFakeClock() *FakeClock {
	return &FakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

// Now 返回当前假时刻（可直接作为 LocalConfig.Now 等注入点）。
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance 把假时钟前进 d。
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// ClockFactory 是支持时钟注入的缓存工厂形态：实现把 clock.Now 路由到
// 全部 TTL 判定（如 LocalConfig.Now），后台资源由 factory 经 t.Cleanup 清理。
// 支持空值缓存的实现请配置正值 NullTTL（套件以「远超缺省空值窗口」的推进
// 验证过窗重源）。
type ClockFactory[T any] func(t *testing.T, source cache.SourceFunc[T], clock *FakeClock) cache.CacheAside[T]

// RunWithClock 跑时钟驱动的 TTL 边界场景（4 个子测试，#9——供支持时钟注入的
// 实现接入，补 Run 真实时间套件覆盖不了的确定性边界）：
//
//  1. ttl_expiry_boundary      过期边界语义：SetWithTTL 后推进到过期时刻**仍命中**
//     （expiresAt 为闭边界，After 判定），越过 1ns 即 miss
//  2. ttl_zero_never_expires   TTL=0（自定义 TTL 传 0）永不过期——推进 100 年仍命中
//  3. null_marker_out_of_window Source (nil,nil) 写 null 标记；推进远超空值窗口
//     后重新回源（source 计数 1→2）
//  4. overwrite_resets_expiry  重写刷新过期时刻：原 TTL 已越过、新 TTL 仍在窗内 →
//     命中新值
func RunWithClock[T any](t *testing.T, c Contract[T], factory ClockFactory[T]) {
	t.Helper()

	t.Run("ttl_expiry_boundary", func(t *testing.T) { runTTLExpiryBoundary(t, c, factory) })
	t.Run("ttl_zero_never_expires", func(t *testing.T) { runTTLZeroNeverExpires(t, c, factory) })
	t.Run("null_marker_out_of_window", func(t *testing.T) { runNullMarkerOutOfWindow(t, factory) })
	t.Run("overwrite_resets_expiry", func(t *testing.T) { runOverwriteResetsExpiry(t, c, factory) })
}

// runTTLExpiryBoundary 场景 1：TTL 30s——推进恰 30s（= expiresAt）仍命中，
// 再推 1ns 后 miss。闭边界语义（isExpired 用 now.After）在此处契约化。
func runTTLExpiryBoundary[T any](t *testing.T, c Contract[T], factory ClockFactory[T]) {
	t.Helper()
	clock := NewFakeClock()
	cc := factory(t, unusedSource[T](t), clock)
	ctx := context.Background()

	if err := cc.SetWithTTL(ctx, "clock-ttl", c.NewValue(0), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(30 * time.Second)
	if got, err := cc.Get(ctx, "clock-ttl"); err != nil || !c.Equal(got, c.NewValue(0)) {
		t.Fatalf("过期时刻（恰 30s）仍应命中（闭边界），got (%v, %v)", got, err)
	}
	clock.Advance(time.Nanosecond)
	if got, err := cc.Get(ctx, "clock-ttl"); err != nil || got != nil {
		t.Fatalf("越过过期时刻 1ns 应 miss，got (%v, %v)", got, err)
	}
	if exists, err := cc.Exists(ctx, "clock-ttl"); err != nil || exists {
		t.Fatalf("过期后 Exists 应 false，got (%v, %v)", exists, err)
	}
}

// runTTLZeroNeverExpires 场景 2：SetWithTTL 传 ttl=0 = 永不过期。
func runTTLZeroNeverExpires[T any](t *testing.T, c Contract[T], factory ClockFactory[T]) {
	t.Helper()
	clock := NewFakeClock()
	cc := factory(t, unusedSource[T](t), clock)
	ctx := context.Background()

	if err := cc.SetWithTTL(ctx, "clock-zero", c.NewValue(1), 0); err != nil {
		t.Fatalf("SetWithTTL(0): %v", err)
	}
	clock.Advance(100 * 365 * 24 * time.Hour)
	if got, err := cc.Get(ctx, "clock-zero"); err != nil || !c.Equal(got, c.NewValue(1)) {
		t.Fatalf("TTL=0 永不过期，推进 100 年后仍应命中，got (%v, %v)", got, err)
	}
}

// runNullMarkerOutOfWindow 场景 3：null 标记过窗重源——窗口内防穿透
// （source 计数恒 1），推进远超空值窗口后重新回源（计数 2）。
func runNullMarkerOutOfWindow[T any](t *testing.T, factory ClockFactory[T]) {
	t.Helper()
	clock := NewFakeClock()
	var calls int
	cc := factory(t, func(ctx context.Context, key string) (*T, error) {
		calls++
		return nil, nil
	}, clock)
	ctx := context.Background()

	if _, err := cc.GetOrLoad(ctx, "clock-null"); err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	if calls != 1 {
		t.Fatalf("source 计数应恰 1，got %d", calls)
	}

	// 窗口内推进（远小于任何 sane NullTTL）：null 标记仍有效，不回源
	clock.Advance(time.Second)
	if _, err := cc.GetOrLoad(ctx, "clock-null"); err != nil {
		t.Fatalf("GetOrLoad in null window: %v", err)
	}
	if calls != 1 {
		t.Fatalf("null 标记窗口内不应回源：source 计数 want 1 got %d", calls)
	}

	// 推进 1h（远超缺省 1 分钟空值窗口与任何 sane 配置）：过窗重源
	clock.Advance(time.Hour)
	if _, err := cc.GetOrLoad(ctx, "clock-null"); err != nil {
		t.Fatalf("GetOrLoad past null window: %v", err)
	}
	if calls != 2 {
		t.Fatalf("null 标记过窗应重新回源：source 计数 want 2 got %d", calls)
	}
}

// runOverwriteResetsExpiry 场景 4：重写刷新过期时刻——原 30s TTL 推进 20s 后
// 重写（新过期 = +50s），再推 15s（原窗口已过、新窗口仍在）应命中新值。
func runOverwriteResetsExpiry[T any](t *testing.T, c Contract[T], factory ClockFactory[T]) {
	t.Helper()
	clock := NewFakeClock()
	cc := factory(t, unusedSource[T](t), clock)
	ctx := context.Background()

	if err := cc.SetWithTTL(ctx, "clock-overwrite", c.NewValue(0), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(20 * time.Second)
	if err := cc.SetWithTTL(ctx, "clock-overwrite", c.NewValue(1), 30*time.Second); err != nil {
		t.Fatalf("overwrite SetWithTTL: %v", err)
	}
	clock.Advance(15 * time.Second) // 距首次写入 35s > 30s：旧过期已越过
	if got, err := cc.Get(ctx, "clock-overwrite"); err != nil || !c.Equal(got, c.NewValue(1)) {
		t.Fatalf("重写应刷新过期时刻并命中新值，got (%v, %v)", got, err)
	}
}
