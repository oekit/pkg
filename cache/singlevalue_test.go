package cache

// SingleValue 单值 TTL 缓存测试（#213）：无 key 参数的取值/回源/失效语义，
// 内部复用 LocalCache + SingleFlightCache（不另造实现）。

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSingleValue_GetLoadsViaSource(t *testing.T) {
	var calls atomic.Int64
	sv := NewSingleValue[bool](SingleValueConfig[bool]{
		TTL: time.Minute,
		Source: func(context.Context) (*bool, error) {
			calls.Add(1)
			v := true
			return &v, nil
		},
	})
	t.Cleanup(func() { _ = sv.Close(context.Background()) })
	ctx := context.Background()

	v, err := sv.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v == nil || !*v {
		t.Fatalf("Get 应返回回源值 true, got %v", v)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("应回源 1 次, got %d", got)
	}
}

func TestSingleValue_ConcurrentGetSingleFlight(t *testing.T) {
	var calls atomic.Int64
	release := make(chan struct{})
	sv := NewSingleValue[string](SingleValueConfig[string]{
		TTL: time.Minute,
		Source: func(context.Context) (*string, error) {
			if calls.Add(1) == 1 {
				close(release) // 首调放行后其余等待者落在同一单飞里
			}
			v := "flag"
			return &v, nil
		},
	})
	t.Cleanup(func() { _ = sv.Close(context.Background()) })

	const n = 16
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			v, err := sv.Get(context.Background())
			if err != nil {
				t.Errorf("Get: %v", err)
				return
			}
			if v == nil || *v != "flag" {
				t.Errorf("Get 应返回 flag, got %v", v)
			}
		})
	}
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("并发 Get 应单飞回源 1 次, got %d", got)
	}
}

func TestSingleValue_TTLExpiryReloads(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	var calls atomic.Int64
	sv := NewSingleValue[int](SingleValueConfig[int]{
		TTL: time.Minute,
		Now: clock,
		Source: func(context.Context) (*int, error) {
			calls.Add(1)
			v := 42
			return &v, nil
		},
	})
	t.Cleanup(func() { _ = sv.Close(context.Background()) })
	ctx := context.Background()

	if _, err := sv.Get(ctx); err != nil {
		t.Fatalf("Get: %v", err)
	}
	now = now.Add(2 * time.Minute) // 推过 TTL
	if _, err := sv.Get(ctx); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("TTL 过期后应重新回源, got %d 次", got)
	}
}

func TestSingleValue_NoValueReturnsSentinel(t *testing.T) {
	var calls atomic.Int64
	sv := NewSingleValue[string](SingleValueConfig[string]{
		TTL: time.Minute,
		Source: func(context.Context) (*string, error) {
			calls.Add(1)
			return nil, nil // 无真值
		},
	})
	t.Cleanup(func() { _ = sv.Close(context.Background()) })
	ctx := context.Background()

	v, err := sv.Get(ctx)
	if v != nil {
		t.Fatalf("无真值应返回 nil 值, got %v", *v)
	}
	if !errors.Is(err, ErrNoValue) {
		t.Fatalf("无真值应返回 ErrNoValue, got %v", err)
	}

	// null marker 防穿透：NullTTL 窗口内不再回源
	if _, err := sv.Get(ctx); !errors.Is(err, ErrNoValue) {
		t.Fatalf("null marker 窗口内应仍返回 ErrNoValue, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("null marker 防穿透应只回源 1 次, got %d", got)
	}
}

func TestSingleValue_NoSourceEmptyCache(t *testing.T) {
	sv := NewSingleValue[bool](SingleValueConfig[bool]{TTL: time.Minute})
	t.Cleanup(func() { _ = sv.Close(context.Background()) })

	v, err := sv.Get(context.Background())
	if v != nil || !errors.Is(err, ErrNoValue) {
		t.Fatalf("未配置 Source 且缓存为空应返回 ErrNoValue, got (%v, %v)", v, err)
	}
}

func TestSingleValue_SourceErrorPropagates(t *testing.T) {
	boom := errors.New("boom")
	sv := NewSingleValue[string](SingleValueConfig[string]{
		TTL:    time.Minute,
		Source: func(context.Context) (*string, error) { return nil, boom },
	})
	t.Cleanup(func() { _ = sv.Close(context.Background()) })

	if _, err := sv.Get(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("回源错误应原样返回, got %v", err)
	}
}

func TestSingleValue_SetAndDel(t *testing.T) {
	var calls atomic.Int64
	sv := NewSingleValue[string](SingleValueConfig[string]{
		TTL: time.Minute,
		Source: func(context.Context) (*string, error) {
			calls.Add(1)
			v := "from-source"
			return &v, nil
		},
	})
	t.Cleanup(func() { _ = sv.Close(context.Background()) })
	ctx := context.Background()

	manual := "manual"
	if err := sv.Set(ctx, &manual); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err := sv.Get(ctx)
	if err != nil || v == nil || *v != manual {
		t.Fatalf("Set 后 Get 应命中显式写入, got (%v, %v)", v, err)
	}

	// Del 失效 → 下一次 Get 重新回源
	if delErr := sv.Del(ctx); delErr != nil {
		t.Fatalf("Del: %v", delErr)
	}
	v, err = sv.Get(ctx)
	if err != nil || v == nil || *v != "from-source" {
		t.Fatalf("Del 后应重新回源, got (%v, %v)", v, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("应恰好回源 1 次, got %d", got)
	}
}

func TestSingleValue_Refresh(t *testing.T) {
	var calls atomic.Int64
	sv := NewSingleValue[int](SingleValueConfig[int]{
		TTL: time.Minute,
		Source: func(context.Context) (*int, error) {
			v := int(calls.Add(1)) // 每次回源返回不同值
			return &v, nil
		},
	})
	t.Cleanup(func() { _ = sv.Close(context.Background()) })
	ctx := context.Background()

	if _, err := sv.Get(ctx); err != nil {
		t.Fatalf("Get: %v", err)
	}
	v, err := sv.Refresh(ctx)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if v == nil || *v != 2 {
		t.Fatalf("Refresh 应强制重新回源, got %v", v)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("Refresh 应回源, 共 %d 次", got)
	}
}

func TestSingleValue_WithTTLSource(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	var calls atomic.Int64
	sv := NewSingleValue[string](SingleValueConfig[string]{
		Now: clock,
		SourceWithTTL: func(context.Context) (*string, time.Duration, error) {
			calls.Add(1)
			v := "token"
			return &v, 30 * time.Second, nil
		},
	})
	t.Cleanup(func() { _ = sv.Close(context.Background()) })
	ctx := context.Background()

	if _, err := sv.Get(ctx); err != nil {
		t.Fatalf("Get: %v", err)
	}
	now = now.Add(31 * time.Second) // 推过动态 TTL
	if _, err := sv.Get(ctx); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("动态 TTL 过期后应重新回源, got %d 次", got)
	}
}
