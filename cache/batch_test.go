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
