// Package timex 提供常用的时间操作工具函数。
//
// 包含时间戳转换、日期格式化、时区处理、时间段边界
// （BeginningOfX/EndOfX：分/时/日/周/月/季/年）等功能。
//
// # 包内依赖纪律（成文，原为无出处的口头约束）
//
// timex 顶层包**零包内依赖**（不 import 本仓其他包，保持叶子工具形态）：
// SleepContext 的 nil ctx 兜底因此是 contextx.OrBackground 的同口径内联
// 而非引用——改动 OrBackground 语义时此处需人工同步。cron 子包不受本纪律
// 约束（已依赖 pkg/errors）。
package timex
