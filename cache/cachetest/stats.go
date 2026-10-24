package cachetest

// 本文件是 pkg#76 的观测计数契约套件：StatsProvider 能力
// （Stats() cache.Stats）——读命中/未命中、容量淘汰、TTL 过期回收的计数口径。
//
// 与 Run/RunWithClock 的区别：那些是所有后端都必须满足的通用契约；
// 本文件里的场景需要实现**显式提供**对应能力，因此套件入口一律**硬失败**
// 而不是静默跳过——显式调用即视为声明该能力，能力缺失或行为不符都必须红，
// 避免"假绿"。

import (
	"context"
	"testing"
	"time"

	"github.com/oekit/pkg/cache"
)

// RunStatsWithClock 跑「观测计数」契约（pkg#76）。
//
// 实现须提供 cache.StatsProvider（Stats() cache.Stats）——未提供即判失败。
//
// 覆盖五点（计数口径自洽 + 单调不减）：
//
//  1. 初始命中/未命中计数全 0；
//  2. 读不存在的 key → Misses+1、Hits 不变；
//  3. Set 后读 → Hits+1、Misses 不变；
//  4. null marker 读 → Misses+1（null marker 无值可返，按未命中口径）；
//  5. TTL 过期后读 → Misses+1。
//
// 关键断言：**每次读恰好计入命中或未命中之一**（互斥），且两个计数各自
// 单调不减。Size 只断言非负（条目数口径由实现定义）。
//
// 刻意不断言 Evictions/Expirations：两者的口径与实现的容量策略、回收
// 路径相关（契约层不绑死），由实现自带的单元测试钉住。
func RunStatsWithClock[T any](t *testing.T, c Contract[T], factory ClockFactory[T]) {
	t.Helper()

	clock := NewFakeClock()
	cc := factory(t, func(ctx context.Context, key string) (*T, error) {
		return nil, nil // null marker
	}, clock)
	sp, ok := cc.(cache.StatsProvider)
	if !ok {
		t.Fatalf("实现须提供 cache.StatsProvider 能力（Stats() cache.Stats）")
	}
	ctx := context.Background()

	base := sp.Stats()
	if base.Hits != 0 || base.Misses != 0 {
		t.Fatalf("初始命中/未命中计数应全 0，got %+v", base)
	}

	// 1. 读不存在的 key → 未命中
	if _, err := cc.Get(ctx, "stats-absent"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	s := sp.Stats()
	if s.Misses != base.Misses+1 {
		t.Fatalf("读不存在的 key 应计未命中，Misses want %d got %d", base.Misses+1, s.Misses)
	}
	if s.Hits != base.Hits {
		t.Fatalf("未命中不得计入命中，Hits want %d got %d", base.Hits, s.Hits)
	}

	// 2. Set 后读 → 命中
	v0 := c.NewValue(0)
	if err := cc.Set(ctx, "stats-hit", v0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	prev := sp.Stats()
	if got, err := cc.Get(ctx, "stats-hit"); err != nil || !c.Equal(got, v0) {
		t.Fatalf("Set 后应命中，got (%v, %v)", got, err)
	}
	s = sp.Stats()
	if s.Hits != prev.Hits+1 {
		t.Fatalf("Set 后读应计命中，Hits want %d got %d", prev.Hits+1, s.Hits)
	}
	if s.Misses != prev.Misses {
		t.Fatalf("命中不得计入未命中，Misses want %d got %d", prev.Misses, s.Misses)
	}

	// 3. null marker 读 → 未命中（null marker 表示数据不存在、无值可返）
	if _, err := cc.GetOrLoad(ctx, "stats-null"); err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	prev = sp.Stats()
	if _, err := cc.Get(ctx, "stats-null"); err != nil {
		t.Fatalf("Get null marker: %v", err)
	}
	s = sp.Stats()
	if s.Misses != prev.Misses+1 {
		t.Fatalf("null marker 读应计未命中，Misses want %d got %d", prev.Misses+1, s.Misses)
	}

	// 4. TTL 过期后读 → 未命中
	if err := cc.SetWithTTL(ctx, "stats-ttl", v0, 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(31 * time.Second)
	prev = sp.Stats()
	if _, err := cc.Get(ctx, "stats-ttl"); err != nil {
		t.Fatalf("Get expired: %v", err)
	}
	s = sp.Stats()
	if s.Misses != prev.Misses+1 {
		t.Fatalf("过期后读应计未命中，Misses want %d got %d", prev.Misses+1, s.Misses)
	}

	// 5. 整体口径自洽：计数单调不减、Size 非负
	final := sp.Stats()
	if final.Hits < base.Hits || final.Misses < base.Misses {
		t.Fatalf("计数应单调不减，base %+v final %+v", base, final)
	}
	if final.Size < 0 {
		t.Fatalf("Size 不应为负，got %d", final.Size)
	}
}
