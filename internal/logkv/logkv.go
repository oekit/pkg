// Package logkv 是 pkg 内部结构化日志字段的单一事实源（#412）。
//
// pkg 是零内部依赖底座，不能引 logx 门面（依赖方向 logx → core → pkg，
// 反向即成环）；但 AGENTS.md 的日志红线要求「错误字段名统一用
// logx.ErrKey 常量（值 "err"），禁止手写 "err"/"error" 字面量——两矩阵
// 落同一 Loki/ES」。本包以单源常量做 logx.ErrKey 的**上游镜像**：pkg 内
// 全部结构化日志的错误字段一律用 ErrKey，"err" 字面量只在本文件出现一次；
// 消费方（业务仓）照旧直接用 logx.ErrKey。
//
// 值由 logx.ErrKey 单向约定（逐字一致），改动任一侧必须同步另一侧——
// 不一致会把两矩阵的日志字段劈成两个，Loki/ES 的聚合查询双双失明。
package logkv

// ErrKey 是结构化日志错误字段的键名——logx.ErrKey（值 "err"）的上游镜像
// （#412）。pkg 零依赖底座不能引 logx，代码侧用本常量满足同一红线；
// 消费方业务仓不受此限制，请直接用 logx.ErrKey。
const ErrKey = "err"
