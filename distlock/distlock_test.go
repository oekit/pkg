package distlock

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeLocker 是行为可编程的内存 Locker：只用于验证契约层（WithLock 的
// 获取/释放路径、ctx 取消场景、哨兵判定），不引任何真后端。
type fakeLocker struct {
	lockErr   error           // Lock 的返回错误
	unlockErr error           // Unlock 的返回错误
	unlockKey string          // Unlock 收到的 key
	unlockOwn string          // Unlock 收到的 ownerID
	unlockCtx context.Context // Unlock 收到的 ctx（验证 WithoutCancel 纪律）
	lockCalls int
	fnRan     bool
}

func (f *fakeLocker) TryLock(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if f.lockErr != nil {
		return "", f.lockErr
	}
	return "owner-1", nil
}

func (f *fakeLocker) Lock(ctx context.Context, key string, ttl, timeout time.Duration) (string, error) {
	f.lockCalls++
	if f.lockErr != nil {
		return "", f.lockErr
	}
	return "owner-1", nil
}

func (f *fakeLocker) Unlock(ctx context.Context, key, ownerID string) error {
	f.unlockCtx = ctx
	f.unlockKey = key
	f.unlockOwn = ownerID
	return f.unlockErr
}

// 编译期契约：fake 实现满足 Locker（接口形状钉住）。
var _ Locker = (*fakeLocker)(nil)

func TestWithLock_AcquireRunRelease(t *testing.T) {
	fl := &fakeLocker{}
	err := WithLock(context.Background(), fl, "job:nightly", 30*time.Second, 5*time.Second, func() error {
		fl.fnRan = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithLock 应成功, got %v", err)
	}
	if fl.lockCalls != 1 {
		t.Fatalf("Lock 应恰被调一次, got %d", fl.lockCalls)
	}
	if !fl.fnRan {
		t.Fatal("fn 应被执行")
	}
	if fl.unlockKey != "job:nightly" || fl.unlockOwn != "owner-1" {
		t.Fatalf("Unlock 应收到同 key 同 owner, got key=%q owner=%q", fl.unlockKey, fl.unlockOwn)
	}
}

func TestWithLock_AcquireFailure_FnNotRun_SentinelPreserved(t *testing.T) {
	fl := &fakeLocker{lockErr: ErrLockTimeout}
	err := WithLock(context.Background(), fl, "k", time.Second, 10*time.Millisecond, func() error {
		t.Fatal("获取失败 fn 不得执行")
		return nil
	})
	if err == nil {
		t.Fatal("获取失败应报错")
	}
	// 哨兵必须穿透 "lock %s:" 包装——调用方 errors.Is 可编程判定（429/重试分流）。
	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("错误链应保留 ErrLockTimeout, got %v", err)
	}
	if fl.fnRan {
		t.Fatal("fn 不应被执行")
	}
	if fl.unlockKey != "" {
		t.Fatal("未获取到锁不得释放")
	}
}

func TestWithLock_FnErrorPropagates(t *testing.T) {
	boom := errors.New("job failed")
	fl := &fakeLocker{}
	err := WithLock(context.Background(), fl, "k", time.Second, 10*time.Millisecond, func() error {
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("fn 的错误应原样上抛, got %v", err)
	}
	if fl.unlockKey != "k" {
		t.Fatal("fn 出错后锁仍必须释放")
	}
}

func TestWithLock_UnlockFailure_DoesNotOverrideFnError(t *testing.T) {
	boom := errors.New("job failed")
	fl := &fakeLocker{unlockErr: ErrLockNotHeld}
	err := WithLock(context.Background(), fl, "k", time.Second, 10*time.Millisecond, func() error {
		return boom
	})
	// 释放失败只记日志：fn 的结果是主结果，ErrLockNotHeld 不顶掉它。
	if !errors.Is(err, boom) {
		t.Fatalf("fn 错误应为主结果, got %v", err)
	}
}

func TestWithLock_ReleasedWithUncancelableContext(t *testing.T) {
	// 核心纪律（redisx 的 WithoutCancel 口径）：fn 执行期间 caller ctx 取消，
	// 释放动作仍必须拿到**未取消**的 ctx——取消的 ctx 会让后端发解锁命令前
	// 就返回 context.Canceled，锁变孤儿阻塞到 TTL 过期。
	ctx, cancel := context.WithCancel(context.Background())
	fl := &fakeLocker{}
	err := WithLock(ctx, fl, "k", time.Second, 10*time.Millisecond, func() error {
		cancel() // 模拟 caller ctx 在持锁期到期
		return nil
	})
	if err != nil {
		t.Fatalf("WithLock 应成功, got %v", err)
	}
	if fl.unlockCtx == nil {
		t.Fatal("Unlock 未被调用")
	}
	if err := fl.unlockCtx.Err(); err != nil {
		t.Fatalf("释放用的 ctx 必须与 caller 取消状态解耦, got %v", err)
	}
	// ctx 携带的取值保留（WithoutCancel 只剥离取消信号，不换 context 链）。
	type ctxKey struct{}
	ctx2, cancel2 := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "auth"))
	defer cancel2()
	fl2 := &fakeLocker{}
	_ = WithLock(ctx2, fl2, "k", time.Second, 10*time.Millisecond, func() error {
		return nil
	})
	if fl2.unlockCtx == nil {
		t.Fatal("Unlock 未被调用")
	}
	if gotVal, _ := fl2.unlockCtx.Value(ctxKey{}).(string); gotVal != "auth" {
		t.Fatalf("释放 ctx 应保留 ctx 取值, got %q", gotVal)
	}
}

func TestWithLock_ReleasedWithUncancelableContext_NoCallerCancel(t *testing.T) {
	// caller ctx 未取消时释放照常工作（WithoutCancel 的常规路径）。
	fl := &fakeLocker{}
	if err := WithLock(context.Background(), fl, "k", time.Second, 10*time.Millisecond, func() error { return nil }); err != nil {
		t.Fatalf("WithLock 应成功, got %v", err)
	}
	if fl.unlockCtx.Err() != nil {
		t.Fatalf("未取消场景释放 ctx 不应带错误, got %v", fl.unlockCtx.Err())
	}
}

func TestSentinels_DistinctAndJudgable(t *testing.T) {
	// 三个哨兵是不同的值（同一进程只此一套），errors.Is 按值身份判定；
	// 包装链（后端包 %w 包装后）判定不丢。
	all := []error{ErrLockNotAcquired, ErrLockTimeout, ErrLockNotHeld}
	for i, a := range all {
		if a == nil {
			t.Fatalf("哨兵 %d 不应为 nil", i)
		}
		for j, b := range all {
			if i != j && errors.Is(a, b) {
				t.Fatalf("哨兵 %d 与 %d 不应等价", i, j)
			}
		}
	}
	// 包装/聚合链上的哨兵判定不丢（后端包 %w 包装后的形态）。
	chain := errors.Join(errors.New("outer"), ErrLockNotHeld)
	if !errors.Is(chain, ErrLockNotHeld) {
		t.Fatal("包装/聚合链上的哨兵应可判定")
	}
}
