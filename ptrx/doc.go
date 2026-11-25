// Package ptrx 提供指针相关的泛型工具函数。
//
// 包含指针的创建（Of）和安全解引用（Deref）等功能。
//
// 生命周期: Stable——同一 minor 线内只允许 additive 变更（新增导出符号 / 新增方法且给零值默认实现 / 新增错误值）；跨 minor 的破坏性变更须先降级并公告（判定规则与流程见 docs/api-compat.md，逐包理由见 api-lifecycle.yaml）。
package ptrx
