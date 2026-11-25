// Package singleflighttest 是 singleflight 语义的共享契约测试套件。
//
// 矩阵内存在两份等价实现（concurrency/singleflight 公开版与
// pkg/internal/singleflight fork，fork 因 pkg 零依赖约束而保留）。本包把
// 关键语义固化为可复用的 Run，双方 CI 各自调用，任一侧语义漂移立刻双红。
// 先例：pkg/cache/cachetest。
// 生命周期: Stable——同一 minor 线内只允许 additive 变更（新增导出符号 / 新增方法且给零值默认实现 / 新增错误值）；跨 minor 的破坏性变更须先降级并公告（判定规则与流程见 docs/api-compat.md，逐包理由见 api-lifecycle.yaml）。
package singleflighttest
