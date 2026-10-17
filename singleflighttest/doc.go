// Package singleflighttest 是 singleflight 语义的共享契约测试套件。
//
// 矩阵内存在两份等价实现（concurrency/singleflight 公开版与
// pkg/internal/singleflight fork，fork 因 pkg 零依赖约束而保留）。本包把
// 关键语义固化为可复用的 Run，双方 CI 各自调用，任一侧语义漂移立刻双红。
// 先例：pkg/cache/cachetest。
package singleflighttest
