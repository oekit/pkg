package cache

// SingleFlightCache 按调用注入回源的测试（pkg#77）：
//   - GetOrLoadWithSource / GetOrLoadWithSourceTTL 保持单飞语义
//     （同一 key 的并发请求只会有一个实际调用 source）；
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

// TestSingleFlightCache_GetOrLoadWithSource_SingleFlight 钉住「按调用注入
// 回源」仍保持单飞语义：同一 key 的并发请求只会有一个实际调用 source。
//
// 这是 issue 标题里「回源依赖 key 的缓存拿不到单飞」的核心诉求。
func TestSingleFlightCache_GetOrLoadWithSource_SingleFlight(t *testing.T) {
	inner := NewLocalCache(LocalConfig[string]{TTL: time.Minute})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache(inner)
	ctx := context.Background()

	var calls atomic.Int64
	release := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := sf.GetOrLoadWithSource(ctx, "k", func(ctx context.Context, key string) (*string, error) {
				calls.Add(1)
				<-release // 卡住源函数，逼并发请求全部堆到 singleflight 上
				return ptr("v"), nil
			})
			if err != nil {
				t.Errorf("GetOrLoadWithSource: %v", err)
				return
			}
			if got == nil || *got != "v" {
				t.Errorf("应返回注入 source 的值，got %v", got)
			}
		}()
	}
	// 等源函数确实被调用，再放行
	for calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("单飞语义：同一 key 的并发请求只应调用 source 1 次，got %d", got)
	}
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
