package cache

// SingleFlightCache 按调用注入回源的测试（pkg#77 + pkg#353）：
//   - GetOrLoadWithSource / GetOrLoadWithSourceTTL **不进 singleflight**：
//     source 函数不可比较，按 key 合并会让等待者拿走先到者的回源结果
//     （同 key 不同调用方注入不同回源时，source 串味 / TTL 被丢 / 强刷
//     退化为普通加载——pkg#353 三例）。并发未命中各自回源，注入的参数
//     必须原样生效；
//   - Refresh 与 load 分键：强刷不被进行中的普通加载吞掉；
//   - 内层不支持按调用注入（未实现 cache.SourceInjector）时退回内层的
//     构造期 Source——能力缺失是内层的事实，不是装饰器引入的偏差。
//
// 契约套件（cachetest）里的对应条目见 cachetest/source.go。

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// plainAside 只暴露 CacheAside 能力——刻意**不**实现 cache.SourceInjector，
// 用于验证装饰器在内层缺少该能力时的退化行为。
type plainAside[T any] struct {
	inner CacheAside[T]
}

func (p *plainAside[T]) Get(ctx context.Context, key string) (*T, error) {
	return p.inner.Get(ctx, key)
}

func (p *plainAside[T]) Set(ctx context.Context, key string, value *T) error {
	return p.inner.Set(ctx, key, value)
}

func (p *plainAside[T]) SetWithTTL(ctx context.Context, key string, value *T, ttl time.Duration) error {
	return p.inner.SetWithTTL(ctx, key, value, ttl)
}

func (p *plainAside[T]) Del(ctx context.Context, key string) error {
	return p.inner.Del(ctx, key)
}

func (p *plainAside[T]) DelBatch(ctx context.Context, keys ...string) error {
	return p.inner.DelBatch(ctx, keys...)
}

func (p *plainAside[T]) Exists(ctx context.Context, key string) (bool, error) {
	return p.inner.Exists(ctx, key)
}

func (p *plainAside[T]) GetOrLoad(ctx context.Context, key string) (*T, error) {
	return p.inner.GetOrLoad(ctx, key)
}

func (p *plainAside[T]) Refresh(ctx context.Context, key string) (*T, error) {
	return p.inner.Refresh(ctx, key)
}

// 编译期断言：plainAside 刻意不实现 SourceInjector。
var _ CacheAside[string] = (*plainAside[string])(nil)

// TestSingleFlightCache_GetOrLoadWithSource_NoCrossMerge 钉住 pkg#353 的
// 新契约：不同调用方注入不同 source 时**不得合并**——每个 source 必须真的
// 被执行，等待者拿到的只能是自己的 source 的结果（串味的旧形态是等待者
// 共享先到者的返回值，后到者的 source 从未被调用）。
func TestSingleFlightCache_GetOrLoadWithSource_NoCrossMerge(t *testing.T) {
	inner := NewLocalCache(LocalConfig[string]{TTL: time.Minute})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache(inner)
	ctx := context.Background()

	gate := make(chan struct{}) // 领先者卡住，逼后来者在其进行中到达
	var srcACalls, srcBCalls atomic.Int64
	started := make(chan struct{}, 1)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() { // G1：srcA 慢
		defer wg.Done()
		got, err := sf.GetOrLoadWithSource(ctx, "k", func(ctx context.Context, key string) (*string, error) {
			srcACalls.Add(1)
			started <- struct{}{}
			<-gate
			return ptr("from-A"), nil
		})
		if err != nil {
			t.Errorf("G1 GetOrLoadWithSource: %v", err)
			return
		}
		if got == nil || *got != "from-A" {
			t.Errorf("G1 应拿到 srcA 的结果，got %v", got)
		}
	}()
	go func() { // G2：srcB，与 G1 同 key 并发
		defer wg.Done()
		<-started // 保证 G1 已进入回源（旧形态下 G2 会合并进 G1）
		got, err := sf.GetOrLoadWithSource(ctx, "k", func(ctx context.Context, key string) (*string, error) {
			srcBCalls.Add(1)
			return ptr("from-B"), nil
		})
		if err != nil {
			t.Errorf("G2 GetOrLoadWithSource: %v", err)
			return
		}
		if got == nil || *got != "from-B" {
			t.Errorf("G2 必须拿到自己 source 的结果（旧形态串味为 from-A），got %v", got)
		}
	}()

	for srcBCalls.Load() == 0 {
		time.Sleep(time.Millisecond) // 等 G2 真正调用自己的 source
	}
	close(gate)
	wg.Wait()

	if got := srcACalls.Load(); got != 1 {
		t.Fatalf("srcA 调用计数 want 1 got %d", got)
	}
	if got := srcBCalls.Load(); got != 1 {
		t.Fatalf("srcB 必须被执行（旧形态被合并丢弃）want 1 got %d", got)
	}
}

// TestSingleFlightCache_GetOrLoadWithSourceTTL_TTLNotLost 钉住 pkg#353 第二例：
// 注入的 TTL 不被并发合并吞掉——普通 GetOrLoad 与 WithSourceTTL 同 key 并发时，
// 后者声明的长 TTL 必须真实生效（旧形态：合并进先到者的默认 TTL 加载，
// 声明 1h 实际 300ms 过期）。
func TestSingleFlightCache_GetOrLoadWithSourceTTL_TTLNotLost(t *testing.T) {
	now := time.Now()
	inner := NewLocalCache(LocalConfig[string]{TTL: 300 * time.Millisecond, Now: func() time.Time { return now }})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache(inner)
	ctx := context.Background()

	// 单发路径：WithSourceTTL 声明 1h——命中窗口必须按 1h 算
	if _, err := sf.GetOrLoadWithSourceTTL(ctx, "k",
		func(ctx context.Context, key string) (*string, time.Duration, error) {
			return ptr("v"), 0, nil // 回源不给 TTL，全靠注入的 1h
		}, time.Hour); err != nil {
		t.Fatalf("GetOrLoadWithSourceTTL: %v", err)
	}
	// 推进 450ms：若注入 TTL 生效应命中（不再回源）
	now = now.Add(450 * time.Millisecond)
	got, err := sf.GetOrLoadWithSourceTTL(ctx, "k",
		func(ctx context.Context, key string) (*string, time.Duration, error) {
			t.Errorf("1h 注入 TTL 内不应过期回源")
			return ptr("v2"), 0, nil
		}, time.Hour)
	if err != nil {
		t.Fatalf("GetOrLoadWithSourceTTL second: %v", err)
	}
	if got == nil || *got != "v" {
		t.Fatalf("注入 TTL 应生效（450ms < 1h），got %v", got)
	}
}

// TestSingleFlightCache_RefreshNotMergedIntoLoad 钉住 pkg#353 第三例：
// Refresh 与进行中的 GetOrLoad 分键——强刷是删除重建，不得退化成
// 「加入当前加载」等先到的加载并拿回它的结果。
func TestSingleFlightCache_RefreshNotMergedIntoLoad(t *testing.T) {
	var calls atomic.Int64
	loadStarted := make(chan struct{}) // 第一次（慢加载）进入 source 时触发
	release := make(chan struct{})     // 放行慢加载
	inner := NewLocalCache(LocalConfig[string]{
		TTL: time.Minute,
		Source: func(ctx context.Context, key string) (*string, error) {
			if calls.Add(1) == 1 {
				loadStarted <- struct{}{}
				<-release // G1 的加载卡在 source 里
				return ptr("old"), nil
			}
			return ptr("new"), nil // Refresh 的回源
		},
	})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache(inner)
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Go(func() { // G1：GetOrLoad 慢加载（进行中）
		got, err := sf.GetOrLoad(ctx, "k")
		if err != nil {
			t.Errorf("GetOrLoad: %v", err)
			return
		}
		if got == nil || *got != "old" {
			t.Errorf("G1 应拿到自己的加载结果 old，got %v", got)
		}
	})
	<-loadStarted // G1 已进入 flight 并卡在 source 里

	// Refresh 必须独立完成删除重建并拿到新值。
	// 旧形态（同键合并）：Refresh 等待 G1 的加载结果，阻塞到 release 之后
	// 才返回，且拿到 "old"。
	got, err := sf.Refresh(ctx, "k")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got == nil || *got != "new" {
		t.Fatalf("Refresh 不应被进行中的 GetOrLoad 合并（旧形态阻塞且拿回 old），got %v", got)
	}
	close(release)
	wg.Wait()
}

// TestSingleFlightCache_GetOrLoadWithSource_InnerUnsupported 钉住退化行为：
// 内层不实现 cache.SourceInjector 时，装饰器退回内层的构造期 Source
// （能力缺失是内层的事实，不是装饰器引入的偏差）。
func TestSingleFlightCache_GetOrLoadWithSource_InnerUnsupported(t *testing.T) {
	var ctorCalls atomic.Int64
	inner := NewLocalCache(LocalConfig[string]{
		TTL: time.Minute,
		Source: func(ctx context.Context, key string) (*string, error) {
			ctorCalls.Add(1)
			return ptr("ctor"), nil
		},
	})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache(&plainAside[string]{inner: inner})
	ctx := context.Background()

	got, err := sf.GetOrLoadWithSource(ctx, "k", func(ctx context.Context, key string) (*string, error) {
		t.Errorf("内层不支持按调用注入时不得调用注入的 source")
		return ptr("injected"), nil
	})
	if err != nil {
		t.Fatalf("GetOrLoadWithSource: %v", err)
	}
	if got == nil || *got != "ctor" {
		t.Fatalf("应退回内层构造期 source 的值，got %v", got)
	}
	if got := ctorCalls.Load(); got != 1 {
		t.Fatalf("构造期 source 调用计数应恰 1，got %d", got)
	}
}

// TestSingleFlightCache_GetOrLoadWithSourceTTL 钉住装饰器上的动态 TTL 注入
// 同样生效（并保持单飞）。
func TestSingleFlightCache_GetOrLoadWithSourceTTL(t *testing.T) {
	now := time.Now()
	inner := NewLocalCache(LocalConfig[string]{
		TTL: time.Minute,
		Now: func() time.Time { return now },
	})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache(inner)
	ctx := context.Background()

	var calls int
	if _, err := sf.GetOrLoadWithSourceTTL(ctx, "k",
		func(ctx context.Context, key string) (*string, time.Duration, error) {
			calls++
			return ptr("v"), 30 * time.Second, nil
		}, 0); err != nil {
		t.Fatalf("GetOrLoadWithSourceTTL: %v", err)
	}
	now = now.Add(31 * time.Second)
	if _, err := sf.GetOrLoadWithSourceTTL(ctx, "k",
		func(ctx context.Context, key string) (*string, time.Duration, error) {
			calls++
			return ptr("v"), 30 * time.Second, nil
		}, 0); err != nil {
		t.Fatalf("GetOrLoadWithSourceTTL after expiry: %v", err)
	}
	if calls != 2 {
		t.Fatalf("动态 TTL 到期后应重新回源，调用计数 want 2 got %d", calls)
	}
}
