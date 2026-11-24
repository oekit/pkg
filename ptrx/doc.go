// Package ptrx 提供指针相关的泛型工具函数。
//
// 包含指针的创建（Of）和安全解引用（Deref）等功能。
//
// 生命周期: Stable——只允许 additive 变更（新增导出符号 / 新增方法且给零值默认实现 / 新增错误值）；改签名、改语义、改默认值、删导出符号都算 breaking，判定规则与变更流程见 docs/api-compat.md，逐包理由见 api-lifecycle.yaml。
package ptrx
