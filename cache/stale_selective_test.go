package cache

// 选择性 stale 测试（#172）：StaleIfError 开启时 Source 仍可让「本次错误不吃
// stale 兜底」，不必让消费方发明毒标记绕行——
//
//   - 取消机制 A：Source 被调用期间对本 key 调 Del（兜底资格在回源之后
//     重新捕获，Del 即取消——修复前 stale 标志先于 Source 捕获，Source 内
//     Del 也取消不了，消费方只能写毒标记绕行）；
//   - 取消机制 B：返回 unwrap 命中 cache.ErrNoStale 的错误（原样回传）。
//
// 正反用例：取消生效 / 未取消仍兜底（原 StaleIfError 行为不回归）；
// SingleFlightCache 装饰层语义同步：单飞等待方拿到的是 leader 过滤后的结果。

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestLocalCache_StaleIfError_SourceDelCancelsFallback 正例（#172）：
// Source 内 Del(key) 取消本次 stale 兜底，回源错误原样返回、OnStale 不触发。
func TestLocalCache_StaleIfError_SourceDelCancelsFallback(t *testing.T) {
	boom := errors.New("upstream denied")
	ctx := context.Background()
	clock := &fakeNowClock{t: nowTestStart}
	var staleCalls atomic.Int64
	c := newNowTestCache(t, clock, func(cfg *LocalConfig[string]) {
		cfg.StaleIfError = true
		cfg.OnStale = func(ctx context.Context, key string, stale *string) { staleCalls.Add(1) }
	})
	if err := c.SetWithTTL(ctx, "k", ptr("stale-v"), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(31 * time.Second) // 真值过期（保留给 stale 兜底）

	c.cfg.Source = func(ctx context.Context, key string) (*string, error) {
		if err := c.Del(ctx, key); err != nil {
			return nil, err
		}
		return nil, boom
	}
	got, err := c.GetOrLoad(ctx, "k")
	if !errors.Is(err, boom) || got != nil {
		t.Fatalf("Source 内 Del 应取消 stale 兜底，got (%v, %v), want (nil, boom)", got, err)
	}
	if staleCalls.Load() != 0 {
		t.Fatalf("取消兜底时 OnStale 不应触发，got %d", staleCalls.Load())
	}
}

// TestLocalCache_StaleIfError_ErrNoStaleCancelsFallback 正例（#172）：
// 回源错误 unwrap 命中 cache.ErrNoStale 时原样回传、不吃 stale；
// 对照组：不带哨兵的普通错误仍走兜底（原行为不回归）。
func TestLocalCache_StaleIfError_ErrNoStaleCancelsFallback(t *testing.T) {
	ctx := context.Background()
	clock := &fakeNowClock{t: nowTestStart}
	c := newNowTestCache(t, clock, func(cfg *LocalConfig[string]) {
		cfg.StaleIfError = true
	})
	if err := c.SetWithTTL(ctx, "k", ptr("stale-v"), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(31 * time.Second)

	// 取消：ErrNoStale 包进错误链（消费方对业务错误的判定不受影响）
	bizErr := errors.New("permission revoked")
	c.cfg.Source = func(ctx context.Context, key string) (*string, error) {
		return nil, fmt.Errorf("%w: %w", bizErr, ErrNoStale)
	}
	got, err := c.GetOrLoad(ctx, "k")
	if !errors.Is(err, ErrNoStale) || !errors.Is(err, bizErr) || got != nil {
		t.Fatalf("ErrNoStale 应原样回传不吃 stale，got (%v, %v), want (nil, err[ErrNoStale])", got, err)
	}

	// 对照：普通错误仍兜底
	boom := errors.New("upstream down")
	c.cfg.Source = func(ctx context.Context, key string) (*string, error) {
		return nil, boom
	}
	got2, err2 := c.GetOrLoad(ctx, "k")
	if err2 != nil || got2 == nil || *got2 != "stale-v" {
		t.Fatalf("普通错误应仍走 stale 兜底，got (%v, %v), want (stale-v, nil)", got2, err2)
	}
}

// TestLocalCache_StaleIfError_UncancelledStillFallsBack 反例（#172）：
// Source 未取消时行为不变——回源失败回退过期真值（fail-open，不回归）。
func TestLocalCache_StaleIfError_UncancelledStillFallsBack(t *testing.T) {
	boom := errors.New("upstream down")
	ctx := context.Background()
	clock := &fakeNowClock{t: nowTestStart}
	c := newNowTestCache(t, clock, func(cfg *LocalConfig[string]) {
		cfg.StaleIfError = true
	})
	if err := c.SetWithTTL(ctx, "k", ptr("stale-v"), 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(31 * time.Second)
	c.cfg.Source = func(ctx context.Context, key string) (*string, error) {
		return nil, boom
	}
	got, err := c.GetOrLoad(ctx, "k")
	if err != nil || got == nil || *got != "stale-v" {
		t.Fatalf("未取消时应仍兜底，got (%v, %v), want (stale-v, nil)", got, err)
	}
}

// TestSingleFlightCache_StaleIfError_SelectiveWaiters 装饰层语义同步（#172）：
// singleflight 共享的是 leader 过滤后的结果——Source 取消兜底时，leader 与
// 全部等待方拿到的都是回源错误；未取消时全员拿到 stale 值（等待方不会拿到
// 与 leader 不一致的另一份结果）。
func TestSingleFlightCache_StaleIfError_SelectiveWaiters(t *testing.T) {
	boom := errors.New("upstream denied")

	for _, tc := range []struct {
		name   string
		source func(ctx context.Context, c *LocalCache[string], key string) (*string, error)
		want   string // 空串 = 期望回源错误；非空 = 期望 stale 兜底值
	}{
		{
			name: "Del 取消：leader 与等待方都拿错误",
			source: func(ctx context.Context, c *LocalCache[string], key string) (*string, error) {
				if err := c.Del(ctx, key); err != nil {
					return nil, err
				}
				return nil, boom
			},
		},
		{
			name: "ErrNoStale 取消：leader 与等待方都拿错误",
			source: func(ctx context.Context, c *LocalCache[string], key string) (*string, error) {
				return nil, fmt.Errorf("%w: %w", boom, ErrNoStale)
			},
		},
		{
			name: "未取消：leader 与等待方都拿 stale 值",
			source: func(ctx context.Context, c *LocalCache[string], key string) (*string, error) {
				return nil, boom
			},
			want: "stale-v",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			clock := &fakeNowClock{t: nowTestStart}
			c := newNowTestCache(t, clock, func(cfg *LocalConfig[string]) {
				cfg.StaleIfError = true
			})
			if err := c.SetWithTTL(ctx, "k", ptr("stale-v"), 30*time.Second); err != nil {
				t.Fatalf("SetWithTTL: %v", err)
			}
			clock.Advance(31 * time.Second)

			var calls atomic.Int64
			release := make(chan struct{})
			c.cfg.Source = func(ctx context.Context, key string) (*string, error) {
				calls.Add(1)
				<-release // 卡住源函数，逼并发请求全部堆到 singleflight 上
				return tc.source(ctx, c, key)
			}
			sf := NewSingleFlightCache(c)

			const n = 16
			var wg sync.WaitGroup
			var arrived sync.WaitGroup
			arrived.Add(n)
			gots := make([]*string, n)
			errs := make([]error, n)
			for i := range n {
				wg.Go(func() {
					arrived.Done() // 到位即报到，接下来进入 GetOrLoad
					gots[i], errs[i] = sf.GetOrLoad(ctx, "k")
				})
			}
			arrived.Wait()
			for calls.Load() == 0 {
				time.Sleep(time.Millisecond)
			}
			// 全员报到、leader 卡在源函数里之后再放行，让其余 15 方确定性地
			// 堆在同一次 flight 上（等待者入队口径对齐
			// TestChecker_RefreshPanic_WaiterGetsError 的「先入队、后放行」）。
			time.Sleep(50 * time.Millisecond)
			close(release)
			wg.Wait()

			if got := calls.Load(); got != 1 {
				t.Fatalf("单飞语义：source 只应被调 1 次，got %d", got)
			}
			for i := range n {
				if tc.want == "" {
					if gots[i] != nil || !errors.Is(errs[i], boom) {
						t.Fatalf("caller #%d: 取消兜底后 leader/等待方都应拿回源错误，got (%v, %v)", i, gots[i], errs[i])
					}
					continue
				}
				if errs[i] != nil || gots[i] == nil || *gots[i] != tc.want {
					t.Fatalf("caller #%d: 未取消时 leader/等待方都应拿 stale 值，got (%v, %v)", i, gots[i], errs[i])
				}
			}
		})
	}
}
