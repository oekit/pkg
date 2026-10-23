package cache

// SingleFlightCache 装饰器行为测试（pkg#72）：
//   - 包装 LocalCache 时必须保持 CacheAside 契约，包括 stale-if-error：
//     回源失败且存在已过期历史真值时返回陈旧值（fail-open）；
//   - 前置检查走非删除式读路径（Peek）：内层不支持时退回 Get（历史行为）。
//
// 场景来源：#72 —— 旧实现的前置检查走 inner.Get（惰性删除路径），把 stale
// 兜底需要的过期真值提前删掉 ⇒ 包装后 StaleIfError 形同虚设。

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestSingleFlightCache_StaleIfErrorPreserved 是 #72 的回归用例：同一份注入
// 时钟、同一份过期数据，只差一层装饰器——
//
//	A) LocalCache.GetOrLoad          → 返回陈旧值（stale 生效）
//	B) SingleFlightCache.GetOrLoad  → 同样返回陈旧值（修复后与 A 一致）
//
// 修复前 B 会把回源错误原样抛出（stale 失效）。
func TestSingleFlightCache_StaleIfErrorPreserved(t *testing.T) {
	boom := errors.New("upstream down")
	ctx := context.Background()

	// A) LocalCache 直连：stale 生效（对照组）
	clockA := &fakeNowClock{t: nowTestStart}
	cacheA := newNowTestCache(t, clockA, func(cfg *LocalConfig[string]) {
		cfg.StaleIfError = true
	})
	if err := cacheA.SetWithTTL(ctx, "k", ptr("v42"), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clockA.Advance(31 * time.Second) // 真值过期
	cacheA.cfg.Source = func(ctx context.Context, key string) (*string, error) {
		return nil, boom
	}
	gotA, errA := cacheA.GetOrLoad(ctx, "k")
	if errA != nil || gotA == nil || *gotA != "v42" {
		t.Fatalf("A) LocalCache 直连 stale 应生效，got (%v, %v), want v42/nil", gotA, errA)
	}

	// B) SingleFlightCache 包装 LocalCache：stale 仍生效（修复点）
	clockB := &fakeNowClock{t: nowTestStart}
	cacheB := newNowTestCache(t, clockB, func(cfg *LocalConfig[string]) {
		cfg.StaleIfError = true
	})
	if err := cacheB.SetWithTTL(ctx, "k", ptr("v42"), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clockB.Advance(31 * time.Second) // 真值过期
	cacheB.cfg.Source = func(ctx context.Context, key string) (*string, error) {
		return nil, boom
	}
	sf := NewSingleFlightCache(cacheB)
	gotB, errB := sf.GetOrLoad(ctx, "k")
	if errB != nil || gotB == nil || *gotB != "v42" {
		t.Fatalf("B) SingleFlightCache 包装后 stale 应仍生效，got (%v, %v), want v42/nil", gotB, errB)
	}
}

// TestSingleFlightCache_StaleIfError_RepeatedFallback stale 可反复兜底：
// 包装后过期真值仍留在表内，回源持续失败时每次都能兜底（不是一次性）。
func TestSingleFlightCache_StaleIfError_RepeatedFallback(t *testing.T) {
	boom := errors.New("upstream down")
	ctx := context.Background()

	clock := &fakeNowClock{t: nowTestStart}
	c := newNowTestCache(t, clock, func(cfg *LocalConfig[string]) {
		cfg.StaleIfError = true
	})
	if err := c.SetWithTTL(ctx, "k", ptr("v"), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(31 * time.Second)
	c.cfg.Source = func(ctx context.Context, key string) (*string, error) {
		return nil, boom
	}

	sf := NewSingleFlightCache(c)
	for i := 0; i < 3; i++ {
		got, err := sf.GetOrLoad(ctx, "k")
		if err != nil || got == nil || *got != "v" {
			t.Fatalf("repeat stale #%d = (%v, %v), want v/nil", i+1, got, err)
		}
	}
}

// TestSingleFlightCache_NoStaleWhenDisabled 未开启 StaleIfError 时行为不变：
// 回源失败原样抛错（不兜底）。
func TestSingleFlightCache_NoStaleWhenDisabled(t *testing.T) {
	boom := errors.New("upstream down")
	ctx := context.Background()

	clock := &fakeNowClock{t: nowTestStart}
	c := newNowTestCache(t, clock, nil)
	if err := c.SetWithTTL(ctx, "k", ptr("old"), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(31 * time.Second)
	c.cfg.Source = func(ctx context.Context, key string) (*string, error) {
		return nil, boom
	}

	sf := NewSingleFlightCache(c)
	got, err := sf.GetOrLoad(ctx, "k")
	if !errors.Is(err, boom) || got != nil {
		t.Fatalf("未开启 stale 时错误应原样返回，got (%v, %v), want (nil, boom)", got, err)
	}
}

// noPeekInner 是**不实现** PeekGetter 的 CacheAside 实现（模拟第三方后端：
// Redis 等）。用于钉住装饰器的前置检查回退路径——内层不支持非删除式读时
// 退回 Get（历史行为：由内层自行决定过期条目的回收口径）。
type noPeekInner struct {
	values map[string]*string
	gets   int
}

func (n *noPeekInner) Get(_ context.Context, key string) (*string, error) {
	n.gets++
	return n.values[key], nil
}

func (n *noPeekInner) Set(_ context.Context, key string, value *string) error {
	n.values[key] = value
	return nil
}

func (n *noPeekInner) SetWithTTL(ctx context.Context, key string, value *string, _ time.Duration) error {
	return n.Set(ctx, key, value)
}

func (n *noPeekInner) Del(_ context.Context, key string) error {
	delete(n.values, key)
	return nil
}

func (n *noPeekInner) DelBatch(_ context.Context, keys ...string) error {
	for _, k := range keys {
		delete(n.values, k)
	}
	return nil
}

func (n *noPeekInner) Exists(_ context.Context, key string) (bool, error) {
	_, ok := n.values[key]
	return ok, nil
}

func (n *noPeekInner) GetOrLoad(_ context.Context, key string) (*string, error) {
	return n.values[key], nil
}

func (n *noPeekInner) Refresh(ctx context.Context, key string) (*string, error) {
	return n.GetOrLoad(ctx, key)
}

// TestSingleFlightCache_FallbackToGet 内层不支持 PeekGetter 时前置检查退回
// Get：命中路径行为不变（直接返回内层值），且不会因类型断言失败而报错。
func TestSingleFlightCache_FallbackToGet(t *testing.T) {
	ctx := context.Background()
	inner := &noPeekInner{values: map[string]*string{"k": ptr("v")}}

	sf := NewSingleFlightCache(inner)
	got, err := sf.GetOrLoad(ctx, "k")
	if err != nil || got == nil || *got != "v" {
		t.Fatalf("回退路径命中应直接返回值，got (%v, %v)", got, err)
	}
	if inner.gets == 0 {
		t.Fatal("回退路径应走 inner.Get")
	}
}
