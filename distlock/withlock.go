package distlock

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/oekit/pkg/internal/logkv"
)

// distlockLogger 取当前默认 slog logger（带 component 标签）。必须在调用
// 时取，不能是包级 var（包级 var 在 init 阶段求值，早于 main 里的
// slog.SetDefault——cache 包同款教训）。pkg 零内部依赖，不用 logx 门面。
func distlockLogger() *slog.Logger {
	return slog.Default().With("component", "distlock")
}

// WithLock 是便捷包装：加锁 → 执行 fn → 自动释放锁（etcdx#123 的单源实现，
// redisx/etcdx 两仓的 WithLock wave 2 改为薄委托本函数）。
//
// 使用 Lock（带超时重试）获取锁；获取失败（含 ErrLockTimeout）直接返回
// 错误、fn 不执行。释放走 [ReleaseUncancelable]：**持锁期 caller ctx 取消
// 不吞释放动作**（WithoutCancel 语义与历史背景见包文档）。释放失败只记
// 日志不覆盖 fn 的返回值——分布式锁释放失败可能导致孤儿锁直到 TTL 过期，
// 记录日志便于运维定位，但业务结果不该被释放故障顶掉（租约/TTL 到期是
// 后端安全网）。
func WithLock(ctx context.Context, locker Locker, key string, ttl, timeout time.Duration, fn func() error) error {
	ownerID, err := locker.Lock(ctx, key, ttl, timeout)
	if err != nil {
		return fmt.Errorf("lock %s: %w", key, err)
	}
	defer func() {
		// 返回值刻意丢弃（ReleaseUncancelable 内部已记 Warn）：释放失败不
		// 覆盖 fn 的结果——孤儿锁由 TTL/租约到期兜底，业务结果优先。
		_ = ReleaseUncancelable(ctx, locker, key, ownerID)
	}()
	return fn()
}

// ReleaseUncancelable 用与 caller ctx 取消状态解耦的 context 释放锁并记录
// 失败（WithLock 与后端包自组 WithLock 共用的释放纪律单源）。
//
// 为什么要 WithoutCancel：fn 执行期间 ctx 到期（timeout 参数存在的意义）时，
// 取消的 ctx 会让后端在发解锁命令前就返回 context.Canceled，DEL/租约撤销
// 永不执行——锁变孤儿，其他实例阻塞到 TTL 过期。ctx 的取值（鉴权等）原样
// 保留，只剥离取消信号（context.WithoutCancel，Go 1.21+）。
//
// 释放失败记 Warn（component=distlock）并返回错误；调用方（WithLock）只记
// 日志不覆盖 fn 结果，需要区分处理的场景可直接消费返回值。
func ReleaseUncancelable(ctx context.Context, locker Locker, key, ownerID string) error {
	err := locker.Unlock(context.WithoutCancel(ctx), key, ownerID)
	if err != nil {
		distlockLogger().Warn("distlock unlock failed", "key", key, "owner", ownerID, logkv.ErrKey, err)
	}
	return err
}
