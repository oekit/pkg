package cache

import (
	"context"
	"errors"
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

// ---------- 动态回填 TTL 与 stale-if-error（pkg#11） ----------

// TestLocalCache_SourceWithTTL 动态回填 TTL：ttl>0 按其缓存（非配置 TTL），
// ttl<=0 回退配置 TTL；返回 nil 值仍走 null marker。
func TestLocalCache_SourceWithTTL(t *testing.T) {
	clock := &fakeNowClock{t: nowTestStart}
	const (
		configTTL = 30 * time.Second
		dynTTL    = 5 * time.Second
	)
	var withTTLCalls, plainCalls int
	c := newNowTestCache(t, clock, func(cfg *LocalConfig[string]) {
		cfg.Source = func(ctx context.Context, key string) (*string, error) {
			plainCalls++
			v := "plain"
			return &v, nil
		}
		cfg.SourceWithTTL = func(ctx context.Context, key string) (*string, time.Duration, error) {
			withTTLCalls++
			v := "dyn"
			return &v, dynTTL, nil
		}
	})
	ctx := context.Background()

	// SourceWithTTL 优先于 Source（plain 恒不被调）
	if got, err := c.GetOrLoad(ctx, "k"); err != nil || *got != "dyn" {
		t.Fatalf("GetOrLoad = (%v, %v), want dyn", got, err)
	}
	if plainCalls != 0 {
		t.Fatalf("Source must not be called when SourceWithTTL set, calls=%d", plainCalls)
	}
	clock.Advance(dynTTL + time.Second) // 过动态 TTL、未过配置 TTL
	if got, err := c.GetOrLoad(ctx, "k"); err != nil || *got != "dyn" {
		t.Fatalf("after dynTTL GetOrLoad = (%v, %v)", got, err)
	}
	if withTTLCalls != 2 {
		t.Fatalf("source calls = %d, want 2 (dynamic TTL must govern expiry)", withTTLCalls)
	}

	// ttl<=0：回退配置 TTL
	clock2 := &fakeNowClock{t: nowTestStart}
	c2 := newNowTestCache(t, clock2, func(cfg *LocalConfig[string]) {
		cfg.SourceWithTTL = func(ctx context.Context, key string) (*string, time.Duration, error) {
			v := "fallback"
			return &v, 0, nil
		}
	})
	if got, _ := c2.GetOrLoad(ctx, "k"); *got != "fallback" {
		t.Fatalf("got %v", got)
	}
	clock2.Advance(configTTL + time.Second) // > 配置 30s
	if _, err := c2.GetOrLoad(ctx, "k"); err != nil {
		t.Fatal("ttl<=0 must fall back to config TTL")
	}

	// nil 值：仍走 null marker
	clock3 := &fakeNowClock{t: nowTestStart}
	nullCalls := 0
	c3 := newNowTestCache(t, clock3, func(cfg *LocalConfig[string]) {
		cfg.SourceWithTTL = func(ctx context.Context, key string) (*string, time.Duration, error) {
			nullCalls++
			return nil, dynTTL, nil
		}
	})
	if got, err := c3.GetOrLoad(ctx, "nk"); err != nil || got != nil {
		t.Fatalf("nil source = (%v, %v), want (nil, nil)", got, err)
	}
	clock3.Advance(time.Second) // NullTTL 1m 窗口内
	if _, err := c3.GetOrLoad(ctx, "nk"); err != nil {
		t.Fatal("null marker must hold")
	}
	if nullCalls != 1 {
		t.Fatalf("null marker 防穿透失效：calls = %d, want 1", nullCalls)
	}
}

// TestLocalCache_StaleIfError stale 三场景：过期真值兜底（fail-open）、
// OnStale 回调、未开启时错误原样返回；stale 可反复兜底（条目保留于表内）。
func TestLocalCache_StaleIfError(t *testing.T) {
	boom := errors.New("source down")
	failSrc := func(ctx context.Context, key string) (*string, error) {
		return nil, boom
	}

	// 场景 1：开启 StaleIfError——回源失败回退过期真值
	clock := &fakeNowClock{t: nowTestStart}
	var staleCalls []string
	c := newNowTestCache(t, clock, func(cfg *LocalConfig[string]) {
		cfg.StaleIfError = true
		cfg.OnStale = func(ctx context.Context, key string, stale *string) {
			staleCalls = append(staleCalls, key+":"+*stale)
		}
	})
	ctx := context.Background()
	if err := c.SetWithTTL(ctx, "k", ptr("fresh"), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(31 * time.Second) // 真值过期

	c.cfg.Source = failSrc
	got, err := c.GetOrLoad(ctx, "k")
	if err != nil || got == nil || *got != "fresh" {
		t.Fatalf("stale fallback = (%v, %v), want fresh/nil", got, err)
	}
	if len(staleCalls) != 1 || staleCalls[0] != "k:fresh" {
		t.Fatalf("OnStale = %v, want [k:fresh]", staleCalls)
	}

	// 场景 2：反复回源失败反复兜底（过期条目保留于表内）
	clock.Advance(time.Second)
	for i := 0; i < 3; i++ {
		if got, err := c.GetOrLoad(ctx, "k"); err != nil || *got != "fresh" {
			t.Fatalf("repeat stale #%d = (%v, %v), want fresh/nil", i+1, got, err)
		}
	}

	// 场景 3：无历史真值时错误原样返回
	if got, err := c.GetOrLoad(ctx, "no-such"); err == nil || got != nil {
		t.Fatalf("no stale available = (%v, %v), want (nil, boom)", got, err)
	}

	// 场景 4：未开启 StaleIfError——错误原样返回（缺省行为不变）
	clock2 := &fakeNowClock{t: nowTestStart}
	c2 := newNowTestCache(t, clock2, nil)
	_ = c2.SetWithTTL(ctx, "k", ptr("old"), time.Second)
	clock2.Advance(2 * time.Second)
	c2.cfg.Source = failSrc
	if got, err := c2.GetOrLoad(ctx, "k"); !errors.Is(err, boom) || got != nil {
		t.Fatalf("stale disabled = (%v, %v), want (nil, boom)", got, err)
	}
}

// TestLocalCache_StaleNullNotServed null marker 不参与 stale：过期的 null
// 标记 + 回源失败 → 错误原样返回（不会把「不存在」兜底成真值）。
func TestLocalCache_StaleNullNotServed(t *testing.T) {
	clock := &fakeNowClock{t: nowTestStart}
	ctx := context.Background()
	c := newNowTestCache(t, clock, func(cfg *LocalConfig[string]) {
		cfg.StaleIfError = true
		cfg.Source = func(ctx context.Context, key string) (*string, error) {
			return nil, nil // 先写 null marker
		}
	})
	if _, err := c.GetOrLoad(ctx, "nk"); err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	clock.Advance(2 * time.Minute) // null marker 过期
	boom := errors.New("down")
	c.cfg.Source = func(ctx context.Context, key string) (*string, error) {
		return nil, boom
	}
	got, err := c.GetOrLoad(ctx, "nk")
	if !errors.Is(err, boom) || got != nil {
		t.Fatalf("null marker must not serve stale = (%v, %v), want (nil, boom)", got, err)
	}
}
