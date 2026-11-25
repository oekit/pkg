// Package contextx 提供上下文（context.Context）变体工具。
//
// 当前能力：ValueOnlyFrom——剥离 Done/Err/Deadline 的只传值轻量 ctx。
//
// 典型场景：把请求携带的业务值（trace、用户标识等）传给后台任务，
// 但不继承请求的取消/超时——否则请求结束时后台任务被连带取消，
// 或 deadline 已近乎耗尽导致后台任务立刻失败。
//
//	bgCtx := contextx.ValueOnlyFrom(reqCtx)
//	async.GoSafe(func() { process(bgCtx, payload) })
//
// 生命周期: Stable——同一 minor 线内只允许 additive 变更（新增导出符号 / 新增方法且给零值默认实现 / 新增错误值）；跨 minor 的破坏性变更须先降级并公告（判定规则与流程见 docs/api-compat.md，逐包理由见 api-lifecycle.yaml）。
package contextx
