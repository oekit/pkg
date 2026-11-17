package timex

import (
	"context"
	"testing"
	"time"
)

// #530：SleepContext 可中断 sleep 的行为契约。
// 计时断言全部只卡「下界/上限差一个数量级」，避免 CI 慢机抖动误报。

func TestSleepContext_SleepsFullDuration(t *testing.T) {
	const d = 50 * time.Millisecond
	start := time.Now()
	if err := SleepContext(context.Background(), d); err != nil {
		t.Fatalf("睡满整段应返回 nil, got %v", err)
	}
	if elapsed := time.Since(start); elapsed < d {
		t.Fatalf("应睡满至少 %v, 实际 %v", d, elapsed)
	}
}

func TestSleepContext_NonPositive_NoWait(t *testing.T) {
	// d<=0 无需等待直通返回 nil（对齐 httpx waitRetry / sshx sleepRedial
	// 的 d<=0 分支语义）——既不睡也不把 ctx 取消当成打断。
	cases := []struct {
		name string
		d    time.Duration
	}{
		{"零", 0},
		{"负数", -time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			if err := SleepContext(context.Background(), tc.d); err != nil {
				t.Fatalf("d=%v 应直接返回 nil, got %v", tc.d, err)
			}
			if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
				t.Fatalf("d=%v 不应等待, 实际耗时 %v", tc.d, elapsed)
			}
		})
	}
}

func TestSleepContext_NonPositive_DeadCtxStillNil(t *testing.T) {
	// d<=0 直通不检查 ctx：已取消 ctx + 零等待也返回 nil（文档写明，
	// 需要可见取消的调用方自查 ctx.Err()）。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SleepContext(ctx, 0); err != nil {
		t.Fatalf("d<=0 直通应返回 nil, got %v", err)
	}
}

func TestSleepContext_CancelInterrupts(t *testing.T) {
	const d = 2 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := SleepContext(ctx, d)
	if err != context.Canceled {
		t.Fatalf("等待期取消应返回 context.Canceled, got %v", err)
	}
	if elapsed := time.Since(start); elapsed >= d {
		t.Fatalf("取消应打断等待, 实际耗时 %v（应远小于 %v）", elapsed, d)
	}
}

func TestSleepContext_AlreadyCancelled_ReturnsImmediately(t *testing.T) {
	const d = 2 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	if err := SleepContext(ctx, d); err != context.Canceled {
		t.Fatalf("已取消 ctx 应立即返回 context.Canceled, got %v", err)
	}
	if elapsed := time.Since(start); elapsed >= d {
		t.Fatalf("已取消 ctx 不应等待, 实际耗时 %v", elapsed)
	}
}

func TestSleepContext_DeadlineExceeded(t *testing.T) {
	const d = 2 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	if err := SleepContext(ctx, d); err != context.DeadlineExceeded {
		t.Fatalf("等待期超时应返回 context.DeadlineExceeded, got %v", err)
	}
}

func TestSleepContext_NilCtx_Background(t *testing.T) {
	// ctx 为 nil 按 Background 处理：正常睡满，不 panic。
	const d = 20 * time.Millisecond
	start := time.Now()
	if err := SleepContext(nil, d); err != nil {
		t.Fatalf("nil ctx 应按 Background 处理, got %v", err)
	}
	if elapsed := time.Since(start); elapsed < d {
		t.Fatalf("应睡满至少 %v, 实际 %v", d, elapsed)
	}
}
