package cache

import (
	"context"
	"sync"
	"testing"
	"time"
)

// LocalCache 时钟注入测试（#9）：假时钟确定性驱动 TTL/NullTTL 全路径，
// 无 sleep 无 flake。

var nowTestStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type fakeNowClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeNowClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeNowClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// newNowTestCache 构造注入假时钟的 LocalCache（TTL 30s / NullTTL 1m 为缺省口径）。
func newNowTestCache(t *testing.T, clock *fakeNowClock, mutate func(*LocalConfig[string])) *LocalCache[string] {
	t.Helper()
	cfg := LocalConfig[string]{
		TTL:     30 * time.Second,
		NullTTL: time.Minute,
		Now:     clock.Now,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	c := NewLocalCache(cfg)
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// TestLocalCache_FakeClock_TTLBoundary 过期闭边界：恰到期时刻仍命中（After 判定），
// 越过 1ns 即 miss；MGet/Exists 同口径。
func TestLocalCache_FakeClock_TTLBoundary(t *testing.T) {
	clock := &fakeNowClock{t: nowTestStart}
	c := newNowTestCache(t, clock, nil)
	ctx := context.Background()
	v := "v"

	if err := c.SetWithTTL(ctx, "k", &v, 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(30 * time.Second)
	if got, err := c.Get(ctx, "k"); err != nil || got == nil || *got != v {
		t.Fatalf("恰到期时刻应仍命中（闭边界），got (%v, %v)", got, err)
	}
	if ok, err := c.Exists(ctx, "k"); err != nil || !ok {
		t.Fatalf("恰到期时刻 Exists 应 true，got (%v, %v)", ok, err)
	}
	if got, err := c.MGet(ctx, "k"); err != nil || len(got) != 1 {
		t.Fatalf("恰到期时刻 MGet 应命中，got (%v, %v)", got, err)
	}

	clock.Advance(time.Nanosecond)
	if got, err := c.Get(ctx, "k"); err != nil || got != nil {
		t.Fatalf("越过过期时刻 1ns 应 miss，got (%v, %v)", got, err)
	}
	if ok, err := c.Exists(ctx, "k"); err != nil || ok {
		t.Fatalf("过期后 Exists 应 false，got (%v, %v)", ok, err)
	}
	if got, err := c.MGet(ctx, "k"); err != nil || len(got) != 0 {
		t.Fatalf("过期后 MGet 应缺席，got (%v, %v)", got, err)
	}
}

// TestLocalCache_FakeClock_NullWindow NullTTL 窗口：窗口内 null marker 防穿透，
// 过窗重源；负 NullTTL 禁用空值缓存（立即重源）。
func TestLocalCache_FakeClock_NullWindow(t *testing.T) {
	clock := &fakeNowClock{t: nowTestStart}
	var calls int
	c := newNowTestCache(t, clock, func(cfg *LocalConfig[string]) {
		cfg.Source = func(ctx context.Context, key string) (*string, error) {
			calls++
			return nil, nil
		}
	})
	ctx := context.Background()

	if got, err := c.GetOrLoad(ctx, "nk"); err != nil || got != nil {
		t.Fatalf("GetOrLoad null source = (%v, %v), want (nil, nil)", got, err)
	}
	clock.Advance(30 * time.Second) // 窗口 1m 内
	if _, err := c.GetOrLoad(ctx, "nk"); err != nil {
		t.Fatalf("GetOrLoad in window: %v", err)
	}
	if calls != 1 {
		t.Fatalf("null 窗口内不应回源：calls = %d, want 1", calls)
	}

	clock.Advance(31 * time.Second) // 累计 61s > 1m：过窗重源
	if _, err := c.GetOrLoad(ctx, "nk"); err != nil {
		t.Fatalf("GetOrLoad past window: %v", err)
	}
	if calls != 2 {
		t.Fatalf("null 过窗应重源：calls = %d, want 2", calls)
	}

	// 负 NullTTL 禁用空值缓存：每次都回源
	clock2 := &fakeNowClock{t: nowTestStart}
	calls2 := 0
	c2 := newNowTestCache(t, clock2, func(cfg *LocalConfig[string]) {
		cfg.NullTTL = -1
		cfg.Source = func(ctx context.Context, key string) (*string, error) {
			calls2++
			return nil, nil
		}
	})
	_, _ = c2.GetOrLoad(ctx, "x")
	_, _ = c2.GetOrLoad(ctx, "x")
	if calls2 != 2 {
		t.Fatalf("负 NullTTL 应禁用空值缓存（每次回源）：calls = %d, want 2", calls2)
	}
}

// TestLocalCache_FakeClock_TTLZeroAndOverwrite TTL=0 永不过期；Set（默认 TTL）
// 与重写刷新过期时刻。
func TestLocalCache_FakeClock_TTLZeroAndOverwrite(t *testing.T) {
	clock := &fakeNowClock{t: nowTestStart}
	c := newNowTestCache(t, clock, nil)
	ctx := context.Background()

	if err := c.SetWithTTL(ctx, "z", ptr("zv"), 0); err != nil {
		t.Fatalf("SetWithTTL(0): %v", err)
	}
	clock.Advance(100 * 365 * 24 * time.Hour)
	if got, err := c.Get(ctx, "z"); err != nil || got == nil || *got != "zv" {
		t.Fatalf("TTL=0 永不过期，got (%v, %v)", got, err)
	}

	// 重写刷新过期时刻：旧 TTL 已越过、新 TTL 仍在窗
	clock = &fakeNowClock{t: nowTestStart}
	c2 := newNowTestCache(t, clock, nil)
	if err := c2.SetWithTTL(ctx, "o", ptr("old"), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(20 * time.Second)
	if err := c2.SetWithTTL(ctx, "o", ptr("new"), 30*time.Second); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	clock.Advance(15 * time.Second) // 距首写 35s > 30s
	if got, err := c2.Get(ctx, "o"); err != nil || got == nil || *got != "new" {
		t.Fatalf("重写应刷新过期时刻并命中新值，got (%v, %v)", got, err)
	}
}

// TestLocalCache_FakeClock_CleanupNoFalseSweep 清扫判定走注入时钟：真实 ticker
// 多轮触发期间假时钟未推进，条目不得被误判过期清空（真实/假时间戳不可比）。
func TestLocalCache_FakeClock_CleanupNoFalseSweep(t *testing.T) {
	clock := &fakeNowClock{t: nowTestStart}
	// TTL=50ms → 清扫 ticker 25ms 真实节奏，100ms 内多轮触发
	c := newNowTestCache(t, clock, func(cfg *LocalConfig[string]) { cfg.TTL = 50 * time.Millisecond })
	ctx := context.Background()

	if err := c.Set(ctx, "survivor", ptr("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // 假时钟未动，真实清扫已跑多轮
	if c.Size() != 1 {
		t.Fatalf("清扫不得按真实时间误删：size = %d, want 1", c.Size())
	}
	if got, err := c.Get(ctx, "survivor"); err != nil || got == nil {
		t.Fatalf("假时钟未推进应仍命中，got (%v, %v)", got, err)
	}

	// 推进假时钟越过 TTL：惰性路径与清扫路径都应判过期
	clock.Advance(60 * time.Millisecond)
	if got, err := c.Get(ctx, "survivor"); err != nil || got != nil {
		t.Fatalf("假时钟推进后应 miss，got (%v, %v)", got, err)
	}
}

func ptr[T any](v T) *T { return &v }
