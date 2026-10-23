package cachetest

import (
	"context"
	"errors"
	"testing"
	"time"
)

// 本文件是 pkg#72 的「stale-if-error」契约套件：回源失败时回退已过期的
// 历史真值（fail-open）。
//
// 与 Run/RunWithClock 的区别：那些断言所有后端都必须满足的通用契约；
// 本文件断言的是**实现显式开启的能力**（LocalConfig.StaleIfError）——
// 套件入口硬失败而不是静默跳过：显式调用即视为声明该能力，能力缺失或
// 行为不符都必须红，避免"假绿"。
//
// 这条契约真正钉住的是「**装饰器不得改变内层语义**」：SingleFlightCache
// 包装 LocalCache 时，其 GetOrLoad 的前置检查若走 Get（惰性删除路径），
// 就会把 stale 兜底需要的那份过期真值提前删掉 ⇒ 兜底失效 ⇒ 回源错误
// 被原样抛出。这正是 pkg#72 的形态。

// RunStaleIfErrorWithClock 跑「stale-if-error」契约（pkg#72）。
//
// factory 须构造**开启 stale-if-error** 的缓存（LocalCache：
// LocalConfig.StaleIfError=true），并把传入的 source 作为回源路径。
//
// 场景：写入一条 30s TTL 的真值 → 推进时钟越过 TTL（真值过期但仍在表内）
// → 回源失败（source 恒返回错误）→ GetOrLoad 必须回退过期真值返回
// （fail-open，err=nil），而不是把回源错误原样抛出。
//
// 前提：实现须能在回源失败时保留并读到过期真值（LocalCache 的 GetOrLoad
// 刻意绕开惰性删除，把回收交给后台清扫与其他读路径）。对未声明该能力的
// 实现本场景不适用——显式调用即视为声明该能力，能力缺失即判失败。
func RunStaleIfErrorWithClock[T any](t *testing.T, c Contract[T], factory ClockFactory[T]) {
	t.Helper()

	clock := NewFakeClock()
	boom := errors.New("upstream down")
	// 回源恒失败——回源失败是 stale 兜底的触发前提
	cc := factory(t, func(ctx context.Context, key string) (*T, error) {
		return nil, boom
	}, clock)
	ctx := context.Background()

	// 写入一条 30s TTL 的真值
	want := c.NewValue(42)
	if err := cc.SetWithTTL(ctx, "stale-key", want, 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}

	// 推进时钟越过 TTL——真值过期但仍留在表内
	clock.Advance(31 * time.Second)

	// 回源失败 → 应回退过期真值（fail-open），而不是把回源错误原样抛出
	got, err := cc.GetOrLoad(ctx, "stale-key")
	if err != nil {
		t.Fatalf("回源失败时应回退过期真值（stale-if-error），却返回错误: %v", err)
	}
	if got == nil || !c.Equal(got, want) {
		t.Fatalf("stale 兜底应返回历史真值，got %v", got)
	}
}

// RunStaleIfErrorRepeatedWithClock 跑「stale 兜底可反复生效」契约（pkg#72）。
//
// stale-if-error 的兜底值必须**留在表内**——否则第一次兜底后过期真值被
// 回收，第二次回源失败时就没有可退的值了（fail-open 只在第一次生效，
// 形同虚设）。
//
// 场景：写入 30s TTL 真值 → 推进时钟越过 TTL → 连续三次回源失败 →
// 三次都应回退同一份历史真值。
func RunStaleIfErrorRepeatedWithClock[T any](t *testing.T, c Contract[T], factory ClockFactory[T]) {
	t.Helper()

	clock := NewFakeClock()
	boom := errors.New("upstream down")
	cc := factory(t, func(ctx context.Context, key string) (*T, error) {
		return nil, boom
	}, clock)
	ctx := context.Background()

	want := c.NewValue(7)
	if err := cc.SetWithTTL(ctx, "stale-repeat", want, 30*time.Second); err != nil {
		t.Fatalf("SetWithTTL: %v", err)
	}
	clock.Advance(31 * time.Second)

	for i := 1; i <= 3; i++ {
		got, err := cc.GetOrLoad(ctx, "stale-repeat")
		if err != nil {
			t.Fatalf("第 %d 次回源失败时应回退过期真值（stale-if-error），却返回错误: %v", i, err)
		}
		if got == nil || !c.Equal(got, want) {
			t.Fatalf("第 %d 次 stale 兜底应返回历史真值，got %v", i, got)
		}
	}
}
