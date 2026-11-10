package cache

// Checker 构造期校验与刷新取消语义的回归测试（pkg#356 + pkg#352）：
//   - 构造期：nil refresh fail-fast panic（总则①）；WithClock(nil) 显式回退
//     默认时钟（总则②，与同包 LocalConfig.Now 口径一致）——nil 不再一路存进
//     实例到 Check 的远端调用点炸 SIGSEGV；
//   - 刷新取消语义：刷新是共享回填，跑在 WithoutCancel 上——leader 断开不
//     中止刷新、不把取消缓存成 failTTL 传染等待者；刷新自身返回的 Canceled
//     视为瞬态不进缓存；入口对已取消 ctx 快速返回、不触发共享刷新。
import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// TestNewCheckerNilRefreshPanics 钉住 pkg#356①：必需回调 nil 在构造期 fail-fast。
func TestNewCheckerNilRefreshPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("NewChecker(nil refresh) 必须在构造期 panic（旧形态 nil 存进实例，Check 远端炸）")
		}
	}()
	NewChecker(nil, time.Minute, 0)
}

// TestWithClockNilFallsBackToDefault 钉住 pkg#356②：WithClock(nil) 显式回退
// 默认时钟，不把 nil 存进实例。
func TestWithClockNilFallsBackToDefault(t *testing.T) {
	c := NewChecker(func(ctx context.Context) error { return nil }, time.Minute, 0, WithClock(nil))
	if err := c.Check(context.Background()); err != nil {
		t.Fatalf("WithClock(nil) 应回退默认时钟正常工作，得到 %v（旧形态 nil 时钟 SIGSEGV）", err)
	}
}

// TestCheckCanceledCtxAtEntry 钉住 pkg#352 入口语义：已取消的调用方快速返回，
// 不触发共享刷新。
func TestCheckCanceledCtxAtEntry(t *testing.T) {
	var calls atomic.Int64
	c := NewChecker(func(ctx context.Context) error {
		calls.Add(1)
		return nil
	}, time.Minute, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Check(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消 ctx 应在入口返回 ctx.Err()，得到 %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("已取消的调用方不应触发共享刷新，refresh 调用计数 want 0 got %d", got)
	}
}

// TestCheckLeaderCancelDoesNotAbortSharedRefresh 钉住 pkg#352 核心场景：
// leader 断开不把共享刷新整个拖死——刷新收到的 ctx 在 leader 取消后仍
// err()==nil（WithoutCancel），刷新完成且成功结果正常入缓存。
func TestCheckLeaderCancelDoesNotAbortSharedRefresh(t *testing.T) {
	started := make(chan struct{})
	gate := make(chan struct{})
	var leaderCanceled atomic.Bool
	c := NewChecker(func(ctx context.Context) error {
		close(started)
		<-gate // 刷新进行中，主 goroutine 此刻取消 leader 的 ctx
		if err := ctx.Err(); err != nil {
			// 旧形态：刷新跑在 leader 的 ctx 上，取消直接传染进刷新
			leaderCanceled.Store(true)
			return fmt.Errorf("refresh aborted: %w", err)
		}
		return nil
	}, time.Hour, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- c.Check(ctx) }()
	<-started
	cancel()    // leader 断开（客户端断连/超时——正是线上最常见形态）
	close(gate) // 放行刷新

	if err := <-errCh; err != nil {
		t.Fatalf("leader 断开不应中止共享刷新也不应把取消当结果返回，得到 %v", err)
	}
	if leaderCanceled.Load() {
		t.Fatalf("刷新不得跑在 leader 可取消的 ctx 上（pkg#352 旧形态）")
	}
	// 成功结果已入缓存：新调用方在 ttl 内直接命中
	if err := c.Check(context.Background()); err != nil {
		t.Fatalf("刷新成功结果应入缓存，得到 %v", err)
	}
}

// TestCheckTransientCancelNotCached 钉住 pkg#352 缓存面：刷新自身返回的
// Canceled 是瞬态——如实返回给本次调用方，但不坐 failTTL，下次 Check
// 立即重试（旧形态整个 failTTL 内所有调用方都被缓存住的取消顶回来）。
func TestCheckTransientCancelNotCached(t *testing.T) {
	var calls atomic.Int64
	c := NewChecker(func(ctx context.Context) error {
		if calls.Add(1) == 1 {
			return fmt.Errorf("inner op: %w", context.Canceled)
		}
		return nil
	}, time.Hour, time.Hour) // failTTL 拉满——若取消被缓存，第二次必然命中缓存
	if err := c.Check(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("首次应返回刷新的取消错误，得到 %v", err)
	}
	if err := c.Check(context.Background()); err != nil {
		t.Fatalf("瞬态取消不得入缓存（failTTL=%v），第二次得到 %v", time.Hour, err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("取消未入缓存应立即重试刷新，refresh 调用计数 want 2 got %d", got)
	}
}
