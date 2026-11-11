package cache

import (
	"context"
	"time"
)

// 本文件是 LocalCache 的过期清扫路径（#374 拆分）：后台清扫 goroutine 与
// 手动 Sweep。读写热路径见 local.go，回源与世代写入见 local_load.go。

// sweepNeeded 报告是否存在会过期的条目来源：真值 TTL 或 null marker TTL。
//
// 判据是「有没有会过期的条目」，不是「TTL 是否为正」——null marker 按
// NullTTL 过期（缺省 1 分钟），TTL=0（真值永不过期）时 null marker 照样会
// 过期。此前后台清扫只看 TTL>0 ⇒ 这类配置下过期 null marker 永远没人回收
// （惰性删除只覆盖 Get/GetEx/Exists，回源路径是非删除式的）⇒ 无界增长。
func (c *LocalCache[T]) sweepNeeded() bool {
	return c.cfg.TTL > 0 || c.effectiveNullTTL() > 0
}

// sweepInterval 是后台清扫的节奏：最短过期窗口的一半（TTL 与 NullTTL 取小），
// 下限 1s（time.NewTicker(0) 会 panic；TTL=1ns 之类整除为 0 的同型问题）。
func (c *LocalCache[T]) sweepInterval() time.Duration {
	horizon := c.cfg.TTL
	if nullTTL := c.effectiveNullTTL(); nullTTL > 0 && (horizon <= 0 || nullTTL < horizon) {
		horizon = nullTTL
	}
	interval := horizon / 2
	if interval <= 0 {
		interval = time.Second
	}
	return interval
}

// Sweep 手动清扫过期条目，返回本次清除的条目数（pkg#25）。
//
// 供「不起后台清扫协程」模式（LocalConfig.NoBackgroundSweep）下由调用方
// 在合适时机回收过期条目——短命 CLI/宿主内嵌场景不必常驻清扫协程。
// 过期判定走注入时钟（LocalConfig.Now），与后台清扫同一口径。
//
// ctx 已取消时直接返回 0（不执行清扫）。可与后台清扫并存——两者各自
// 独立判定，重复清扫不会重复计数（已被回收的条目不在表内）。
func (c *LocalCache[T]) Sweep(ctx context.Context) int {
	if ctx != nil && ctx.Err() != nil {
		return 0
	}
	return c.sweepExpired(c.now())
}

// cleanup 定期清理过期条目。
// ticker 只定节奏，过期判定一律走注入时钟——假时钟下真实 ticker 的时间戳
// 不可比（直接比较会把全部条目误判为过期而清空）。
func (c *LocalCache[T]) cleanup() {
	// 节奏取最短过期窗口的一半（TTL 与 NullTTL）——只按 TTL 算会漏掉
	// 「TTL=0、靠 NullTTL 过期」这一类条目的及时回收；除以 2 可能整除为 0
	// （如 TTL=1ns），time.NewTicker(0) 会 panic，下限 1s 保底。
	interval := c.sweepInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.sweepExpired(c.now())
		}
	}
}

// sweepExpired 清扫过期条目，返回实际清除的条目数。
// 分两阶段：读锁收集过期 key，写锁批量删除，减少写锁持有时间。
//
// StaleIfError 开启时，过期**真值**条目被跳过（保留给 stale 兜底）——
// 见 retainForStale。
func (c *LocalCache[T]) sweepExpired(now time.Time) int {
	c.mu.RLock()
	expired := make([]string, 0, 8)
	for key, entry := range c.data {
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			if c.retainForStale(entry) {
				continue // 保留给 stale-if-error 兜底
			}
			expired = append(expired, key)
		}
	}
	c.mu.RUnlock()

	if len(expired) == 0 {
		return 0
	}

	c.mu.Lock()
	removed := 0
	for _, key := range expired {
		// 二次检查：可能已被其他 goroutine 更新
		if entry, ok := c.data[key]; ok && entry.isExpired(now) {
			if c.retainForStale(entry) {
				continue
			}
			delete(c.data, key)
			c.removeFromOrder(key)
			removed++
		}
	}
	c.mu.Unlock()
	c.expirations.Add(int64(removed))
	return removed
}
