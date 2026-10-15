package contextx

import (
	"context"
	"time"
)

// valueOnly 只透传 Value 的轻量 ctx 包装：Done 恒为 nil（对其接收将永久
// 阻塞，select 语义上等于"永不取消"）、Err 恒为 nil、无 deadline。
type valueOnly struct {
	ctx context.Context
}

func (v valueOnly) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (v valueOnly) Done() <-chan struct{} {
	return nil
}

func (v valueOnly) Err() error {
	return nil
}

func (v valueOnly) Value(key any) any {
	return v.ctx.Value(key)
}

// ValueOnlyFrom 返回只保留取值能力的 context.Context：Value 透传 parent，
// 剥离 Done/Err/Deadline——parent 的取消与超时不影响返回的 ctx。
// 适合把请求 ctx 携带的值交给后台任务（配合 async.GoSafe 等），
// 同时切断内部取消的传播。parent 须非 nil（与标准库行为一致）。
func ValueOnlyFrom(parent context.Context) context.Context {
	return valueOnly{ctx: parent}
}
