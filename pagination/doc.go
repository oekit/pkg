// Package pagination 提供框架无关的分页参数定义。
//
// 本包是 PageRequest 的唯一定义位置，middleware 和 data/gormx 均依赖本包，
// 避免 middleware 层反向依赖 data 层。
// 生命周期: Stable——只允许 additive 变更（新增导出符号 / 新增方法且给零值默认实现 / 新增错误值）；改签名、改语义、改默认值、删导出符号都算 breaking，判定规则与变更流程见 docs/api-compat.md，逐包理由见 api-lifecycle.yaml。
package pagination
