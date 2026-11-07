package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/oekit/pkg/cache"
)

// MGet 批量读取：命中的键进 map；不存在与 null marker 一律缺席（与
// (nil, nil) 双关一致的键缺席语义）。
func TestLocalCacheMGet(t *testing.T) {
	ctx := context.Background()
	sourceCalls := 0
	c := cache.NewLocalCache(cache.LocalConfig[int]{
		TTL: time.Minute,
		Source: func(ctx context.Context, key string) (*int, error) {
			sourceCalls++
			if key == "ghost" {
				return nil, nil // null marker 防穿透
			}
			v := len(key)
			return &v, nil
		},
	})

	// 预置：hit 直写；marker 经 GetOrLoad 生成
	v := 3
	if err := c.Set(ctx, "hit", &v); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := c.GetOrLoad(ctx, "ghost"); err != nil {
		t.Fatalf("GetOrLoad marker: %v", err)
	}

	got, err := c.MGet(ctx, "hit", "ghost", "absent")
	if err != nil {
		t.Fatalf("MGet: %v", err)
	}
	if len(got) != 1 || got["hit"] == nil || *got["hit"] != 3 {
		t.Fatalf("MGet = %v, want 只含 hit", got)
	}
	if sourceCalls != 1 {
		t.Fatalf("source calls = %d, want 1", sourceCalls)
	}

	// 批量读不产生回填/穿透副作用
	got2, err := c.MGet(ctx, "miss1", "miss2")
	if err != nil {
		t.Fatalf("MGet second: %v", err)
	}
	if len(got2) != 0 {
		t.Fatalf("纯 miss 的 MGet 必须返回空 map: %v", got2)
	}
	if sourceCalls != 1 {
		t.Fatalf("MGet 不得触发回源: source calls = %d", sourceCalls)
	}

	// 空键集
	empty, err := c.MGet(ctx)
	if err != nil || len(empty) != 0 {
		t.Fatalf("MGet() = %v, %v", empty, err)
	}
}

// SingleFlightCache 的 MGet 委托内层批量能力；内层无批量能力时退化为逐键。
func TestSingleFlightMGetDelegates(t *testing.T) {
	ctx := context.Background()
	inner := cache.NewLocalCache(cache.LocalConfig[int]{TTL: time.Minute})
	sf := cache.NewSingleFlightCache(inner)

	v := 9
	if err := inner.Set(ctx, "k", &v); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := sf.MGet(ctx, "k", "absent")
	if err != nil {
		t.Fatalf("MGet: %v", err)
	}
	if len(got) != 1 || got["k"] == nil || *got["k"] != 9 {
		t.Fatalf("MGet = %v, want 只含 k", got)
	}
}

// TestLocalCacheMGetEx MGetEx 区分「nil 真值命中」与「缺席」（pkg#238）：
// MGet 的 map 里 v, ok := m[k] 对 nil 值命中 ok 也为 true，与缺席只能靠键
// 存在性区分，消费方极易误判；MGetEx 对每个请求键给出 Hit 标志
// （GetEx 的批量形态，口径一致：不存在/已过期/null marker 一律 Hit=false）。
func TestLocalCacheMGetEx(t *testing.T) {
	ctx := context.Background()
	sourceCalls := 0
	c := cache.NewLocalCache(cache.LocalConfig[int]{
		TTL: time.Minute,
		Source: func(ctx context.Context, key string) (*int, error) {
			sourceCalls++
			return nil, nil // null marker 防穿透
		},
	})
	t.Cleanup(func() { _ = c.Close(ctx) })

	// 预置：真值 / 显式 nil 真值 / null marker（经 GetOrLoad 生成）
	v := 3
	if err := c.Set(ctx, "hit", &v); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.Set(ctx, "nil-value", nil); err != nil {
		t.Fatalf("Set(nil): %v", err)
	}
	if _, err := c.GetOrLoad(ctx, "ghost"); err != nil {
		t.Fatalf("GetOrLoad marker: %v", err)
	}

	got, err := c.MGetEx(ctx, "hit", "nil-value", "ghost", "absent")
	if err != nil {
		t.Fatalf("MGetEx: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("MGetEx 应对每个请求键各给一条结果，got %v", got)
	}
	// 1. 普通真值命中
	if e := got["hit"]; !e.Hit || e.Value == nil || *e.Value != 3 {
		t.Fatalf("hit 应为 {Value=3, Hit=true}，got %+v", e)
	}
	// 2. nil 真值命中：Hit=true 且 Value=nil——这是 MGetEx 存在的意义
	if e := got["nil-value"]; !e.Hit || e.Value != nil {
		t.Fatalf("nil 真值应为 {Value=nil, Hit=true}，got %+v", e)
	}
	// 3. null marker：按缺席处理（数据不存在，与 GetEx 口径一致）
	if e := got["ghost"]; e.Hit || e.Value != nil {
		t.Fatalf("null marker 应为 {Value=nil, Hit=false}，got %+v", e)
	}
	// 4. 缺席键：Hit=false
	if e := got["absent"]; e.Hit || e.Value != nil {
		t.Fatalf("缺席键应为 {Value=nil, Hit=false}，got %+v", e)
	}

	// 纯读不回源（与 MGet 同口径）
	if sourceCalls != 1 {
		t.Fatalf("MGetEx 不得触发回源: source calls = %d", sourceCalls)
	}

	// 空键集
	empty, err := c.MGetEx(ctx)
	if err != nil || len(empty) != 0 {
		t.Fatalf("MGetEx() = %v, %v", empty, err)
	}

	// MGet 既有行为不回归：仍只返回命中键的值 map
	m, err := c.MGet(ctx, "hit", "nil-value", "absent")
	if err != nil {
		t.Fatalf("MGet: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("MGet = %v, want 只含 hit 与 nil-value", m)
	}
}
