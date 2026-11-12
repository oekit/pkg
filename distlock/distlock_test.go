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
	lockErr            error           // Lock 的返回错误
	unlockErr          error           // Unlock 的返回错误
	unlockKey          string          // Unlock 收到的 key
	unlockOwn          string          // Unlock 收到的 ownerID
	unlockCtx          context.Context // Unlock 收到的 ctx（验证 WithoutCancel 纪律）
	unlockCtxErrAtCall error           // Unlock **调用时**的 ctx 状态（#428：ReleaseUncancelable
	// defer cancel 会取消 timeout ctx，事后查 Err() 恒为 Canceled——解耦纪律
	// 的真值只在调用点成立，fake 在调用点采样）
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
	f.unlockCtxErrAtCall = ctx.Err()
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
	// #428：解耦纪律的判定点在 Unlock **调用时**（ReleaseUncancelable 的
	// defer cancel 会让事后 Err() 恒为 Canceled——那是 timeout ctx 的正常
	// 收尾，不是 caller 取消传染）。调用点状态必须干净，且必须带 deadline
	// （释放有上界，裸 WithoutCancel 的「永不超时」已废除）。
	if fl.unlockCtxErrAtCall != nil {
		t.Fatalf("释放用的 ctx 在调用点必须与 caller 取消状态解耦, got %v", fl.unlockCtxErrAtCall)
	}
	if _, ok := fl.unlockCtx.Deadline(); !ok {
		t.Fatal("释放 ctx 必须带 deadline（#428：释放动作有上界）")
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
	if fl.unlockCtxErrAtCall != nil {
		t.Fatalf("未取消场景释放 ctx 调用点不应带错误, got %v", fl.unlockCtxErrAtCall)
	}
	if _, ok := fl.unlockCtx.Deadline(); !ok {
		t.Fatal("释放 ctx 必须带 deadline（#428：释放动作有上界）")
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

// hangingUnlocker 的 Unlock 挂到 ctx 完成为止——模拟 etcd 半开连接/慢盘
// 场景（#428）：裸 WithoutCancel 时代 ctx 永不完成 ⇒ 调用方永久阻塞。
type hangingUnlocker struct {
	Locker
}

func (h hangingUnlocker) Unlock(ctx context.Context, key, ownerID string) error {
	<-ctx.Done()
	return ctx.Err()
}

// TestReleaseUncancelable_Bounded 钉住 #428：释放动作必须有上界——裸
// WithoutCancel 把 deadline 一并剥了，Unlock 挂起时调用方 goroutine 永久
// 阻塞（etcdx v0.2.12 的 3s 短超时在迁单源时丢失）。修后 WithoutCancel
// 之上叠 WithTimeout(releaseTimeout)，超时返回可
// errors.Is(err, context.DeadlineExceeded) 判定的错误。
func TestReleaseUncancelable_Bounded(t *testing.T) {
	old := releaseTimeout
	releaseTimeout = 100 * time.Millisecond
	defer func() { releaseTimeout = old }()

	ctx := context.Background()
	start := time.Now()
	err := ReleaseUncancelable(ctx, hangingUnlocker{}, "k", "owner")
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("挂起 Unlock 应以 DeadlineExceeded 收场, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("释放耗时 %v：上界未生效（应 ~releaseTimeout）", elapsed)
	}
}

// funcUnlocker 只重写 Unlock 的可编程桩（其余方法不注入——
// ReleaseUncancelable 只调 Unlock）。
type funcUnlocker struct {
	Locker
	unlock func(ctx context.Context, key, ownerID string) error
}

func (f funcUnlocker) Unlock(ctx context.Context, key, ownerID string) error {
	return f.unlock(ctx, key, ownerID)
}

// TestReleaseUncancelable_NormalPath 契约基线：正常释放不受上界改动影响。
func TestReleaseUncancelable_NormalPath(t *testing.T) {
	calls := 0
	normal := funcUnlocker{unlock: func(ctx context.Context, key, ownerID string) error {
		calls++
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}}
	if err := ReleaseUncancelable(context.Background(), normal, "k", "owner"); err != nil {
		t.Fatalf("正常释放不应报错: %v", err)
	}
	if calls != 1 {
		t.Fatalf("Unlock 调用次数 = %d, want 1", calls)
	}
}
