package cache

import (
	"context"
	"fmt"
	"testing"
)

// BenchmarkLocalCacheGetLRU_LibraryRead 量化 LRU 命中读路径的锁开销
// （#性能优化基线：每次读命中 promoteOne 取写锁 + 二次 map 查找）。
// 热键形态：64 个键轮转读，MaxSize 充足不触发淘汰。
func BenchmarkLocalCacheGetLRU_LibraryRead(b *testing.B) {
	c := NewLocalCache[int](LocalConfig[int]{
		MaxSize:           1024,
		EvictPolicy:       EvictLRU,
		NoBackgroundSweep: true,
	})
	defer c.Close(context.Background())
	keys := make([]string, 64)
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d", i)
		v := i
		if err := c.Set(context.Background(), keys[i], &v); err != nil {
			b.Fatal(err)
		}
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Get(ctx, keys[i&63]); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLocalCacheGetFIFO_LibraryRead 对照组：FIFO 策略读路径
// （无 promoteOne，只有 RLock 读 + 计数）。
func BenchmarkLocalCacheGetFIFO_LibraryRead(b *testing.B) {
	c := NewLocalCache[int](LocalConfig[int]{
		MaxSize:           1024,
		EvictPolicy:       EvictFIFO,
		NoBackgroundSweep: true,
	})
	defer c.Close(context.Background())
	keys := make([]string, 64)
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d", i)
		v := i
		if err := c.Set(context.Background(), keys[i], &v); err != nil {
			b.Fatal(err)
		}
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Get(ctx, keys[i&63]); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLocalCacheGetLRU_Parallel 并发命中读——LRU 的每读写锁在多读
// goroutine 下的串行化代价（单线程 12ns 的差距会放大成锁竞争）。
func BenchmarkLocalCacheGetLRU_Parallel(b *testing.B) {
	c := NewLocalCache[int](LocalConfig[int]{
		MaxSize:           1024,
		EvictPolicy:       EvictLRU,
		NoBackgroundSweep: true,
	})
	defer c.Close(context.Background())
	keys := make([]string, 64)
	for i := range keys {
		keys[i] = fmt.Sprintf("k%d", i)
		v := i
		if err := c.Set(context.Background(), keys[i], &v); err != nil {
			b.Fatal(err)
		}
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if _, err := c.Get(ctx, keys[i&63]); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}
