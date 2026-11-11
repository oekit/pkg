package distlock

import (
	"context"
	"errors"
	"time"
)

// Locker 是分布式锁的后端中立接口（etcdx#123 契约单源；redisx/distlock 与
// etcdx/distlock 的历史接口方法集本就同形，此处取两仓接口并集）。
//
// 实现约定：
//   - ownerID 是本次持有的凭据（随机串或租约 ID），Unlock 必须校验匹配——
//     「只有持有者能释放」是分布式锁的正确性底线；
//   - 后端特有能力（自动续期、最大持锁时长等）留在具体类型/构造选项上，
//     不进中立接口；需要可判定的可选能力时按接口断言外置（参考 cache 包
//     Sweeper/PeekGetter 的形态）；
//   - ttl / timeout 的边界语义（ttl 必须为正、timeout<=0 单次尝试等）由
//     各后端在自己的文档逐条声明（两仓现状口径见各自 Locker 注释），本
//     接口只定形状与哨兵，不装作差异不存在（etcdx#86）。
type Locker interface {
	// TryLock 非阻塞尝试获取锁。
	// 返回 (ownerID, nil) 表示成功，ownerID 用于后续 Unlock。
	// 返回 ("", ErrLockNotAcquired) 表示锁被占用。
	TryLock(ctx context.Context, key string, ttl time.Duration) (string, error)

	// Lock 阻塞获取锁，带超时重试。
	// 返回 (ownerID, nil) 表示成功。
	// 返回 ("", ErrLockTimeout) 表示超时未获取到。
	Lock(ctx context.Context, key string, ttl time.Duration, timeout time.Duration) (string, error)

	// Unlock 释放锁，ownerID 必须匹配。
	// 返回 nil 表示锁已删除；锁不存在或已易主（陈旧持有者解锁）返回
	// ErrLockNotHeld（调用方不得继续假装持有）；其它 error 为后端通信故障。
	Unlock(ctx context.Context, key string, ownerID string) error
}

// 哨兵错误集（etcdx#123 单源；文案以 redisx/distlock 为基准，漂移史与
// etcdx 侧 ErrNotOwner 的映射关系见包文档）。同进程只此一套值——后端仓
// wave 2 起别名指向这里，不再各自 errors.New。
var (
	// ErrLockNotAcquired 表示锁被其他实例持有，获取失败（TryLock 单次
	// 未抢到，不是通信故障）。
	ErrLockNotAcquired = errors.New("lock not acquired")

	// ErrLockTimeout 表示在指定超时时间内未能获取锁。
	ErrLockTimeout = errors.New("lock acquire timeout")

	// ErrLockNotHeld 表示解锁时锁不存在或已易主——本次 Unlock **没有删除
	// 任何锁**。典型成因：锁 TTL 过期后已被他人重新获取、或对同一
	// (key, ownerID) 重复 Unlock。收到本哨兵即说明自己已不再持有锁，
	// 不得继续在临界区内工作（继续执行会与新持有者并发）；errors.Is 判定，
	// 与其它后端通信错误（可重试）区分。
	ErrLockNotHeld = errors.New("lock not held by this owner")
)
