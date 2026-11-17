package timex

import (
	"context"
	"time"
)

// SleepContext 可被 ctx 取消/超时打断的 sleep（#530）。
//
// 裸 time.Sleep 不看 ctx：重试退避、重拨间隔这类等待发生在持有请求 ctx
// 的 goroutine 上时，取消与超时要等睡满整段才被感知（httpx#236 的病灶
// ——退避第 7 档 64s，单请求总时长被放大到分钟级，与「总时长预算由调用
// 方经 ctx 控制」直接冲突）。本函数用 select{timer.C, ctx.Done()} 实现：
// ctx 到期立即返回 ctx.Err()，等待不再吞取消信号。
//
// 返回值语义：
//   - nil：睡满 d（或 d<=0 无需等待），未被取消；
//   - ctx.Err()（context.Canceled / context.DeadlineExceeded）：等待期间
//     ctx 被取消/超时，等待被打断。
//
// d <= 0 视为无需等待，直接返回 nil、不检查 ctx 取消状态——对齐两个消费
// 方的 d<=0 直通分支（httpx waitRetry：d<=0 timer 立即到期，由重试循环顶
// 部自查取消；sshx sleepRedial：d<=0 视为无需等待、「不算打断」，返回
// nil）。需要零等待时也可见取消的调用方自行先查 ctx.Err()。
//
// ctx 为 nil 按 context.Background() 处理（矩阵 #159 的
// contextx.OrBackground 同口径内联，保持 timex 零包内依赖）。
//
// 用法（可中断 sleep 典型形态）:
//
//	if err := timex.SleepContext(ctx, backoff); err != nil {
//	    return err // ctx 已取消/超时，等待被打断
//	}
func SleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
