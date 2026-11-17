package contextx

import "context"

// OrBackground 把 nil ctx 归一为 context.Background()（#529）：
// ctx == nil 返回 context.Background()，非 nil 原样返回。
//
// context.Context 是接口，nil 接口值可以一路传到深处的工具函数才炸
// （nil 解引用 panic，堆栈离「传错了」的调用点很远）——
// `if ctx == nil { ctx = context.Background() }` 因此成了防御性内联的
// 高频写法（矩阵 #159 实测 28 处）。本函数是这批内联的**收敛目标**：
// 消费方后续迁移，本单只加符号不迁调用点。
//
// 用法:
//
//	func Do(ctx context.Context) error {
//	    ctx = contextx.OrBackground(ctx)
//	    ...
//	}
//
// ⚠️ 只做 nil 归一，不做其它「修复」：已取消 / 已过期的 ctx 原样透传
// （调用方要的是「别 panic」，不是「复活取消信号」——把取消中的 ctx 换成
// Background 会吞掉上游的超时与取消语义，恰是本函数要避免的事故）。
func OrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
