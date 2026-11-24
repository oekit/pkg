// Package convert 提供常用类型转换工具函数。
//
// 所有转换函数都支持默认值，转换失败时返回默认值而非 error。
//
//	convert.Str2Int64("123", 0)     // 123
//	convert.Str2Int64("abc", 0)     // 0 (转换失败返回默认值)
//	convert.Str2Float64("3.14", 0)  // 3.14
//	convert.Bool2Str(true)          // "true"
//
// 生命周期: Stable——只允许 additive 变更（新增导出符号 / 新增方法且给零值默认实现 / 新增错误值）；改签名、改语义、改默认值、删导出符号都算 breaking，判定规则与变更流程见 docs/api-compat.md，逐包理由见 api-lifecycle.yaml。
package convert
