package cache

// LocalCache 观测计数测试（pkg#76）：
//   - 读命中/未命中的计数口径（Get/GetEx/Peek/MGet/GetOrLoad）；
//   - 写路径（Set/Del）与 Exists（存在性探测）不计入；
//   - 容量淘汰（Evictions）与 TTL 过期回收（Expirations）分别计数；
//   - Stats().Size 与缓存条目数一致。
//
// 契约套件（cachetest）里的对应条目见 cachetest/stats.go——本文件覆盖
// LocalCache 专属的口径细节（哪些路径计入、哪些不计入）。

import (
	"context"
	"testing"
	"time"
)

// TestLocalCache_Stats_ReadCounting 钉住读路径的计数口径：
// 读命中计 Hits、读未命中计 Misses，二者互斥；写路径（Set/Del）与
// Exists（存在性探测）不计入。
func TestLocalCache_Stats_ReadCounting(t *testing.T) {
	c := NewLocalCache(LocalConfig[string]{TTL: time.Minute})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	ctx := context.Background()

	if s := c.Stats(); s.Hits != 0 || s.Misses != 0 {
		t.Fatalf("初始计数应全 0，got %+v", s)
	}

	// 读不存在的 key → 未命中
	if _, err := c.Get(ctx, "absent"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if s := c.Stats(); s.Misses != 1 || s.Hits != 0 {
		t.Fatalf("读不存在的 key 应计 1 次未命中，got %+v", s)
	}

	// 写路径不计入
	if err := c.Set(ctx, "k", ptr("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if s := c.Stats(); s.Misses != 1 || s.Hits != 0 {
		t.Fatalf("Set 不得计入命中/未命中，got %+v", s)
	}

	// Exists 不计入（存在性探测不是取值读路径）
	if ok, err := c.Exists(ctx, "k"); err != nil || !ok {
		t.Fatalf("Exists: exists=%v err=%v", ok, err)
	}
	if s := c.Stats(); s.Misses != 1 || s.Hits != 0 {
		t.Fatalf("Exists 不得计入命中/未命中，got %+v", s)
	}

	// 读命中
	if got, err := c.Get(ctx, "k"); err != nil || got == nil || *got != "v" {
		t.Fatalf("Get 应命中，got (%v, %v)", got, err)
	}
	if s := c.Stats(); s.Hits != 1 || s.Misses != 1 {
		t.Fatalf("读命中应计 1 次命中，got %+v", s)
	}

	// GetEx 同样计入
	if _, hit, err := c.GetEx(ctx, "k"); err != nil || !hit {
		t.Fatalf("GetEx: hit=%v err=%v", hit, err)
	}
	if s := c.Stats(); s.Hits != 2 {
		t.Fatalf("GetEx 命中应计入，got %+v", s)
	}

	// Peek 同样计入
	if _, hit, err := c.Peek(ctx, "k"); err != nil || !hit {
		t.Fatalf("Peek: hit=%v err=%v", hit, err)
	}
	if s := c.Stats(); s.Hits != 3 {
		t.Fatalf("Peek 命中应计入，got %+v", s)
	}

	// MGet 逐键计入（1 命中 + 1 未命中）
	if _, err := c.MGet(ctx, "k", "absent-2"); err != nil {
		t.Fatalf("MGet: %v", err)
	}
	if s := c.Stats(); s.Hits != 4 || s.Misses != 2 {
		t.Fatalf("MGet 应逐键计入（1 命中 + 1 未命中），got %+v", s)
	}

	// Del 不得计入
	if err := c.Del(ctx, "k"); err != nil {
		t.Fatalf("Del: %v", err)
	}
	if s := c.Stats(); s.Hits != 4 || s.Misses != 2 {
		t.Fatalf("Del 不得计入命中/未命中，got %+v", s)
	}
}

// TestLocalCache_Stats_GetOrLoadCounting 钉住 GetOrLoad 的计数口径：
// 未命中回源记 1 次未命中、命中记 1 次命中；stale 兜底（回源失败回退过期
// 真值）算**未命中**（拿到的是已过期的条目，按 Stats 的 Hits 口径
// 「读到未过期的真值条目」不属于命中）——一次读恰好计入命中或未命中之一。
func TestLocalCache_Stats_GetOrLoadCounting(t *testing.T) {
	c := NewLocalCache(LocalConfig[string]{
		TTL:          time.Minute,
		Source:       func(ctx context.Context, key string) (*string, error) { return ptr("v"), nil },
		StaleIfError: true,
	})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	ctx := context.Background()

	// 未命中 → 回源 → 计 1 次未命中
	if _, err := c.GetOrLoad(ctx, "k"); err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	if s := c.Stats(); s.Misses != 1 || s.Hits != 0 {
		t.Fatalf("GetOrLoad 未命中应计 1 次未命中，got %+v", s)
	}

	// 命中 → 计 1 次命中
	if _, err := c.GetOrLoad(ctx, "k"); err != nil {
		t.Fatalf("GetOrLoad second: %v", err)
	}
	if s := c.Stats(); s.Hits != 1 || s.Misses != 1 {
		t.Fatalf("GetOrLoad 命中应计 1 次命中，got %+v", s)
	}
}

// TestLocalCache_Stats_NullMarkerCountsAsMiss 钉住 null marker 的口径：
// null marker 表示「数据不存在」、无值可返 ⇒ 计入未命中（与 GetEx/Peek 的
// hit=false 一致）。
func TestLocalCache_Stats_NullMarkerCountsAsMiss(t *testing.T) {
	c := NewLocalCache(LocalConfig[string]{
		TTL:     time.Minute,
		NullTTL: time.Minute,
		Source:  func(ctx context.Context, key string) (*string, error) { return nil, nil },
	})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	ctx := context.Background()

	// 回源返回 (nil,nil) → 写 null marker（这次读本身计 1 次未命中）
	if _, err := c.GetOrLoad(ctx, "k"); err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	if s := c.Stats(); s.Misses != 1 {
		t.Fatalf("回源未命中应计 1 次未命中，got %+v", s)
	}

	// 命中 null marker → 仍是未命中口径
	if got, err := c.Get(ctx, "k"); err != nil || got != nil {
		t.Fatalf("null marker 应返回 (nil, nil)，got (%v, %v)", got, err)
	}
	if s := c.Stats(); s.Misses != 2 || s.Hits != 0 {
		t.Fatalf("null marker 命中应计未命中，got %+v", s)
	}
}

// TestLocalCache_Stats_EvictionVsExpiration 钉住两类回收分开计数：
// 容量淘汰（MaxSize 触发）计 Evictions；TTL 过期回收计 Expirations。
func TestLocalCache_Stats_EvictionVsExpiration(t *testing.T) {
	// 容量淘汰
	c := NewLocalCache(LocalConfig[string]{TTL: time.Minute, MaxSize: 2})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	ctx := context.Background()

	for i, k := range []string{"a", "b", "c"} {
		if err := c.Set(ctx, k, ptr(k)); err != nil {
			t.Fatalf("Set %s: %v", k, err)
		}
		_ = i
	}
	s := c.Stats()
	if s.Evictions != 1 {
		t.Fatalf("容量淘汰应计 1 次，got %+v", s)
	}
	if s.Expirations != 0 {
		t.Fatalf("容量淘汰不得计入过期回收，got %+v", s)
	}
	if s.Size != 2 {
		t.Fatalf("Size 应为 2，got %d", s.Size)
	}

	// TTL 过期回收（惰性删除：Get 走双检路径顺带回收）
	now := time.Now()
	c2 := NewLocalCache(LocalConfig[string]{
		TTL: time.Minute,
		Now: func() time.Time { return now },
	})
	t.Cleanup(func() { _ = c2.Close(context.Background()) })
	if err := c2.Set(ctx, "k", ptr("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := c2.Get(ctx, "k"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	s2 := c2.Stats()
	if s2.Expirations != 1 {
		t.Fatalf("过期条目被回收应计 1 次过期回收，got %+v", s2)
	}
	if s2.Evictions != 0 {
		t.Fatalf("过期回收不得计入容量淘汰，got %+v", s2)
	}
	if s2.Misses != 1 {
		t.Fatalf("读过期条目应计 1 次未命中，got %+v", s2)
	}
}

// TestLocalCache_Stats_Size 钉住 Size 与缓存条目数一致。
func TestLocalCache_Stats_Size(t *testing.T) {
	c := NewLocalCache(LocalConfig[string]{TTL: time.Minute})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := c.Set(ctx, string(rune('a'+i)), ptr("v")); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	if s := c.Stats(); s.Size != int64(c.Size()) || s.Size != 3 {
		t.Fatalf("Size 应为 3，got %+v", s)
	}
	if err := c.Del(ctx, "a"); err != nil {
		t.Fatalf("Del: %v", err)
	}
	if s := c.Stats(); s.Size != 2 {
		t.Fatalf("Del 后 Size 应为 2，got %d", s.Size)
	}
}
