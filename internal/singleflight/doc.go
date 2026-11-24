// Package singleflight 提供并发请求合并（防击穿）工具。
//
// fork 自 github.com/oekit/concurrency/singleflight（stdlib-only 泛型实现）：
// pkg 是最底层模块（零 oekit 内部依赖），cache.SingleFlightCache 需要它
// 又不能反向依赖 concurrency，故内部化。concurrency 侧修 bug 时须同步本 fork
// （nil ctx 兜底曾单侧落地漂移过一次，#249 → 本仓已同步并契约化）。
//
// 与 concurrency 侧的已知分叉点清单（同步修复时逐项核对）：
//   - 功能面：concurrency 是超集（DoShared 折叠观测、DoChan channel 形态、
//     Waiters 计数）；本 fork 只保留 cache 需要的 Do/Forget 最小面
//   - 骨架：concurrency 已重构为 doCore/waitCall/release/runLeader 四件套；
//     本 fork 是 Do 内联 defer 形态——语义核心（先摘键后唤醒、
//     CompareAndDelete、panic 转 error 给 waiter + 原样 re-panic）双侧一致
//   - panic 收编载体：本侧 *pkg/errors.PanicError；concurrency 侧
//     *async.PanicError（文档化分工：捕获转换全走 pkg/errors.Recovered 单源，
//     async 类型只补 Task 标签）
//
// 同一个 key 的并发调用只执行一次 fn，其他调用共享结果。
// 适用于缓存穿透防护、token 刷新、配置加载等场景。
//
// # 核心特性
//
//   - 基于 sync.Map + close(chan struct{}) 实现，零外部依赖
//   - 泛型支持，类型安全
//   - race detector 友好
//   - panic 安全：fn panic 不会导致等待者死锁
//
// # 快速接入
//
//	sf := singleflight.New[string]()
//	val, err := sf.Do(ctx, "token:wechat_app", func(ctx context.Context) (string, error) {
//	    return fetchAccessToken(ctx)
//	})
//
// # 原理
//
//	调用 Do(key, fn) 时：
//	 1. sync.Map.LoadOrStore 保证只有一个 goroutine 成为 caller
//	 2. caller 执行 fn，完成后 close(ready) 广播通知
//	 3. 其他 goroutine (waiter) 通过 <-ready 等待，读取共享结果
//	 4. close 提供 happens-before 保证，字段读取无 data race
//
// 生命周期: Experimental——本包仍会改、删、拆，不承诺任何兼容，升级风险自负；定级理由见 api-lifecycle.yaml，分级规则见 docs/api-compat.md。
package singleflight
