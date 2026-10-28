package cache

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestLocalCache_StaleIfError_SurvivesBackgroundSweep 回归测试：
// StaleIfError 开启时，过期真值必须活过后台清扫，兜底才能「反复可用」。
//
// 缺陷成因：后台清扫（缺省开启，TTL/2 一跳）把**所有**过期条目删掉，包括
// 要留作 stale 兜底的真值 ⇒ 兜底只在「[过期, 下次清扫)」窗口里有效，
// 而 LocalConfig.StaleIfError 承诺的是「过期条目在本路径下保留于表内……
// 可反复兜底直至上游恢复」。
func TestLocalCache_StaleIfError_SurvivesBackgroundSweep(t *testing.T) {
	ctx := context.Background()
	c := NewLocalCache(LocalConfig[string]{
		TTL:          100 * time.Millisecond, // 清扫间隔 = TTL/2 = 50ms
		StaleIfError: true,
		Source:       func(ctx context.Context, key string) (*string, error) { return nil, errors.New("upstream down") },
	})
	t.Cleanup(func() { _ = c.Close(ctx) })

	if err := c.Set(ctx, "k", ptr("cached")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// 等到过期之后、且后台清扫至少跑过好几轮。
	time.Sleep(400 * time.Millisecond)

	got, err := c.GetOrLoad(ctx, "k")
	if err != nil {
		t.Fatalf("stale 兜底失效（清扫把兜底值删了）：%v", err)
	}
	if got == nil || *got != "cached" {
		t.Fatalf("stale 兜底值不对：%v", got)
	}

	// 反复兜底：上游没恢复前，每次都该拿到 stale 值。
	for i := 0; i < 3; i++ {
		time.Sleep(120 * time.Millisecond)
		got, err := c.GetOrLoad(ctx, "k")
		if err != nil {
			t.Fatalf("第 %d 次 stale 兜底失效：%v", i+1, err)
		}
		if got == nil || *got != "cached" {
			t.Fatalf("第 %d 次 stale 兜底值不对：%v", i+1, got)
		}
	}
}

// TestLocalCache_StaleIfError_Disabled_StillSweeps 未开启 StaleIfError 时
// 过期条目的回收行为不变（后台清扫照常删）。
func TestLocalCache_StaleIfError_Disabled_StillSweeps(t *testing.T) {
	ctx := context.Background()
	c := NewLocalCache(LocalConfig[string]{
		TTL:          100 * time.Millisecond,
		StaleIfError: false,
		Source:       func(ctx context.Context, key string) (*string, error) { return nil, errors.New("down") },
	})
	t.Cleanup(func() { _ = c.Close(ctx) })

	if err := c.Set(ctx, "k", ptr("cached")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	time.Sleep(400 * time.Millisecond)

	if _, err := c.GetOrLoad(ctx, "k"); err == nil {
		t.Fatal("未开启 StaleIfError 时应原样返回回源错误")
	}
}

// TestLocalCache_StaleIfError_NullMarkerNotRetained null marker 表示数据不
// 存在，没有可兜底的历史真值 ⇒ 不参与 stale 保留，照常被回收。
func TestLocalCache_StaleIfError_NullMarkerNotRetained(t *testing.T) {
	ctx := context.Background()
	c := NewLocalCache(LocalConfig[string]{
		TTL:          time.Minute,
		NullTTL:      100 * time.Millisecond,
		StaleIfError: true,
		Source:       func(ctx context.Context, key string) (*string, error) { return nil, nil },
	})
	t.Cleanup(func() { _ = c.Close(ctx) })

	if _, err := c.GetOrLoad(ctx, "k"); err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	time.Sleep(400 * time.Millisecond)

	if c.Size() != 0 {
		t.Fatalf("过期的 null marker 未被回收（size=%d）", c.Size())
	}
}

// TestLocalCache_SweepNeeded_NullTTLWithoutTTL 回归测试：TTL=0（真值永不过期）
// 但 NullTTL>0 时，后台清扫必须仍然启动——否则过期的 null marker 永远没人
// 回收（惰性删除只覆盖 Get/GetEx/Exists，回源路径是非删除式的）⇒ 无界增长。
func TestLocalCache_SweepNeeded_NullTTLWithoutTTL(t *testing.T) {
	ctx := context.Background()
	c := NewLocalCache(LocalConfig[string]{
		TTL:     0, // 真值永不过期
		NullTTL: 100 * time.Millisecond,
		Source:  func(ctx context.Context, key string) (*string, error) { return nil, nil },
	})
	t.Cleanup(func() { _ = c.Close(ctx) })

	for i := 0; i < 20; i++ {
		if _, err := c.GetOrLoad(ctx, "miss-"+string(rune('a'+i))); err != nil {
			t.Fatalf("GetOrLoad: %v", err)
		}
	}
	if c.Size() != 20 {
		t.Fatalf("null marker 未写入：size=%d", c.Size())
	}

	// 等 null marker 过期 + 后台清扫跑过。
	time.Sleep(500 * time.Millisecond)

	if c.Size() != 0 {
		t.Fatalf("TTL=0 时过期的 null marker 未被回收（size=%d）——后台清扫没启动", c.Size())
	}
}

// TestLocalCache_SweepInterval_UsesShortestHorizon 清扫节奏取最短过期窗口的
// 一半：TTL=0、NullTTL=100ms ⇒ 间隔 50ms（不是缺省 1s）。
func TestLocalCache_SweepInterval_UsesShortestHorizon(t *testing.T) {
	c := NewLocalCache(LocalConfig[string]{TTL: time.Hour, NullTTL: 100 * time.Millisecond})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	if got := c.sweepInterval(); got != 50*time.Millisecond {
		t.Fatalf("sweepInterval = %v，期望 50ms", got)
	}

	c2 := NewLocalCache(LocalConfig[string]{TTL: 100 * time.Millisecond, NullTTL: time.Hour})
	t.Cleanup(func() { _ = c2.Close(context.Background()) })
	if got := c2.sweepInterval(); got != 50*time.Millisecond {
		t.Fatalf("sweepInterval = %v，期望 50ms", got)
	}
}

// TestLocalCache_CloneOnRead_BackfillDoesNotSharePointer 回归测试：回源回填
// 之后，返回给调用方的值不得再是缓存条目本身——调用方改写它不应污染缓存
// （CloneOnRead 开启时）。
//
// 缺陷成因：回填直接 Set(val) 再把同一个指针返回给调用方。回填之后该指针
// 已是缓存的条目，调用方一改写就污染缓存；CloneOnRead 挡不住（下次读命中
// 克隆的是**已被改写**的那份）。
func TestLocalCache_CloneOnRead_BackfillDoesNotSharePointer(t *testing.T) {
	ctx := context.Background()
	c := NewLocalCache(LocalConfig[string]{
		CloneOnRead: func(s *string) *string { v := *s; return &v },
		Source:      func(ctx context.Context, key string) (*string, error) { return ptr("orig"), nil },
	})
	t.Cleanup(func() { _ = c.Close(ctx) })

	got, err := c.GetOrLoad(ctx, "k")
	if err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	*got = "MUTATED"

	second, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if second == nil || *second != "orig" {
		t.Fatalf("缓存被调用方改写污染：%v（期望 orig）", second)
	}
}

// TestLocalCache_CloneOnRead_Unset_KeepsZeroCopy 未配置 CloneOnRead 时保持
// 零拷贝既有口径（回填值与调用方共享引用）。
func TestLocalCache_CloneOnRead_Unset_KeepsZeroCopy(t *testing.T) {
	ctx := context.Background()
	c := NewLocalCache(LocalConfig[string]{
		Source: func(ctx context.Context, key string) (*string, error) { return ptr("orig"), nil },
	})
	t.Cleanup(func() { _ = c.Close(ctx) })

	got, err := c.GetOrLoad(ctx, "k")
	if err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	*got = "MUTATED"

	second, _ := c.Get(ctx, "k")
	if second == nil || *second != "MUTATED" {
		t.Fatalf("未配置 CloneOnRead 时应保持零拷贝共享引用，实得 %v", second)
	}
}

// TestLocalCache_Stats_StaleCountsAsMissOnly 回归测试：一次 stale 读取恰好
// 计入一次未命中，**不**计入命中。
//
// 缺陷成因：stale 路径既计未命中（回源前）又计命中（兜底后）⇒ 一次读被计成
// 两次 ⇒ 命中率虚高。这与 interface.go「一次读恰好计入命中或未命中之一」
// 的承诺矛盾，也与 Stats 的 Hits 口径（「读到**未过期的**真值条目」）矛盾。
func TestLocalCache_Stats_StaleCountsAsMissOnly(t *testing.T) {
	ctx := context.Background()
	c := NewLocalCache(LocalConfig[string]{
		TTL:          50 * time.Millisecond,
		StaleIfError: true,
		Source:       func(ctx context.Context, key string) (*string, error) { return nil, errors.New("down") },
	})
	t.Cleanup(func() { _ = c.Close(ctx) })

	if err := c.Set(ctx, "k", ptr("cached")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	before := c.Stats()
	if _, err := c.GetOrLoad(ctx, "k"); err != nil {
		t.Fatalf("stale 兜底失败：%v", err)
	}
	after := c.Stats()

	if dMiss := after.Misses - before.Misses; dMiss != 1 {
		t.Fatalf("一次 stale 读取应计 1 次未命中，实得 %d（before=%+v after=%+v）", dMiss, before, after)
	}
	if dHit := after.Hits - before.Hits; dHit != 0 {
		t.Fatalf("一次 stale 读取不应计命中（过期真值不是「未过期的真值条目」），实得 %d", dHit)
	}
}

// TestCacheLogger_FollowsSetDefault 回归测试：缓存的日志必须跟随
// slog.SetDefault——消费方服务靠它把日志汇流到自己的 logger。
//
// 缺陷成因：cacheLog 是包级 var，在 init 阶段求值，早于任何 main() 里的
// slog.SetDefault ⇒ 缓存日志始终走进程启动时的默认 handler，汇流不到。
func TestCacheLogger_FollowsSetDefault(t *testing.T) {
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })

	before := cacheLogger()
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	after := cacheLogger()

	if before.Handler() == after.Handler() {
		t.Fatal("slog.SetDefault 之后 cacheLogger 仍返回同一个 handler：缓存日志汇流不到消费方的 logger")
	}
}
