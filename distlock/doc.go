// Package distlock 提供分布式锁的**后端中立契约单源**（etcdx#123 治本侧）：
// Locker 接口、哨兵错误集与 WithLock 便捷包装在此定义一次，redisx/distlock
// 与 etcdx/distlock 后续波次改为消费本包——同一份代码不再在两个后端仓各持
// 一份、靠注释互相提醒手工对账。
//
// 本包**零依赖**（连 oekit 内部也不依赖，pkg 仓红线）：只有接口、哨兵与
// 纯标准库的 WithLock 骨架，不含任何 Redis/etcd 语义。
//
// # 哨兵错误集（以 redisx/distlock 文案为基准）
//
//   - ErrLockNotAcquired "lock not acquired"——锁被他人持有（TryLock 单次
//     未抢到，不是故障）；
//   - ErrLockTimeout "lock acquire timeout"——Lock 在 timeout 内未抢到；
//   - ErrLockNotHeld "lock not held by this owner"——Unlock 时锁不存在或
//     已易主，本次**没有删除任何锁**（调用方不得继续假装持有）。
//
// 文案漂移记录（etcdx#123 的核心痛点）：etcdx/distlock 历史上的
// ErrNotOwner 文案是 "lock not owned by this owner"，与 redisx 侧的
// ErrLockNotHeld 同语义不同文案——两仓各自 errors.New 出的哨兵是不同的值，
// errors.Is 跨包恒 false，适配层换后端后错误分类随之漂移（429 变 500），
// etcdx 为此打了两层补丁（sentinel.Is 同文案等价 + IsOneOf 双向判定）。
// 契约单源后哨兵只有一个值，errors.Is 即正确分类，两套补丁机制不再有
// 存在必要。wave 2 改造时：etcdx 的 ErrNotOwner 语义统一映射到本包
// ErrLockNotHeld（读侧若需兼容旧文案，由 etcdx 自己的适配层负责翻译）。
//
// # WithLock 释放纪律（单源口径，采 redisx 的 context.WithoutCancel）
//
// 持锁期 caller ctx 取消/超时**不吞释放动作**：解锁用与 fn 的取消状态解耦
// 的 context（WithoutCancel）——fn 执行期间 ctx 到期时，取消的 ctx 会让
// 后端发解锁命令前就返回 context.Canceled，DEL/租约撤销永不执行，锁变孤儿，
// 其他实例阻塞到 TTL 过期。etcdx 历史实现用 WithTimeout(Background, 3s)，
// 是同一诉求的另一形态；单源采 WithoutCancel（较新纪律，无魔法常数）。
// 释放失败只记日志（log/slog，component=distlock）不覆盖 fn 的返回值——
// TTL/租约到期是后端的安全网，失败要可观测但不必把业务错误顶掉。
//
// # 后端实现接入指引（redisx / etcdx wave 2 改造照此执行）
//
//  1. 接口：后端的 Locker 实现**直接实现本包 Locker**（方法集逐参数同形，
//     两仓现状已满足；redisx 的 Renew、etcdx 的 MaxHold 等后端特有能力
//     留在各自具体类型/构造选项上，不进中立接口——可选能力走接口断言，
//     参考 cache 包 Sweeper/PeekGetter 的形态）。
//  2. 哨兵：后端包删除自己的 errors.New 哨兵，包级变量别名指向本包
//     （`var ErrLockTimeout = distlock.ErrLockTimeout`——同类型同值，
//     下游 errors.Is 两侧任一写法都命中），同进程只此一套值。
//  3. WithLock：后端包的 WithLock 改为薄委托——把 Locker 实现传进来即可
//     （签名兼容，调用方零改动）；带 per-instance logger 等增强的
//     （redisx#120），后端可自行组合 Lock + [ReleaseUncancelable] 骨架，
//     释放纪律仍走单源。
//  4. ttl/timeout 等参数校验口径（ttl 必须为正、timeout<=0 单次尝试等）
//     属后端行为差异记录在各自 Locker 文档，本包不做运行时校验——契约层
//     只定形状与哨兵，语义差异逐条声明不装作不存在（etcdx#86 的教训）。
//
// # 用法（业务侧）
//
//	var l distlock.Locker = redisxdistlock.NewRedisLocker(cli) // wave 2 起
//	err := distlock.WithLock(ctx, l, "job:nightly", 30*time.Second, 5*time.Second, func() error {
//	    return runNightlyJob()
//	})
//	if errors.Is(err, distlock.ErrLockTimeout) { /* 429 / 不重试 */ }
package distlock
