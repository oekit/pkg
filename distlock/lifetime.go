package distlock

import "time"

// MaxLifetime 计算自动续期场景下的最大持锁生命周期（#534 单源，三态）：
// 语义与注释口径照搬 redisx/distlock watchdog 的 renewMaxLifetime
// （#159 两仓同形逻辑的收敛点）——
//
//   - configured == 0（未设置）：返回 10×ttl。忘 Unlock/Release 时续期到
//     期自行退出，锁/槽在 TTL 后自然过期回收——不形成永久持锁 + goroutine
//     泄漏（TTL 安全网重新生效）；
//   - configured < 0：返回 0 表示**不限**（无 deadline）——调用方显式声明
//     永久持锁语义（长期 lease 型任务），安全网由调用方自管。负值是显式
//     选项而非意外，与 0（未设置）分开表达；
//   - configured > 0：显式上限，原样返回。
//
// 返回值以 0 表达「无 deadline」（消费方以 `<= 0` 或 `== 0` 判定不再续期
// 上限，redisx/etcdx 注入时保持同一口径）。ttl 由调用方保证为正（Locker
// 契约 ttl 必须为正）；ttl <= 0 时 10×ttl 仍为非正，按「无 deadline」语义
// 自然归零，不额外校验。
//
// 两后端（redisx / etcdx 的 distlock）后续注入本函数，本单不迁移消费方。
func MaxLifetime(configured, ttl time.Duration) time.Duration {
	switch {
	case configured == 0:
		return 10 * ttl
	case configured < 0:
		return 0
	default:
		return configured
	}
}
