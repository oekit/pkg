package errors

// DegradedError 表示**软依赖降级**（#533 单源，语义逐字对齐 hertz/health
// 的同名类型，#140）：不影响主流程可用性/就绪的失败——依赖项不可用应保持
// 可见（错误在场、可被 errors.Is/As 识别），但不应让整体被判定为硬失败
// （就绪探针失败导致实例被摘流）。与硬失败（探针报错 → 503 摘流）区分：
// 缓存、旁路服务这类软依赖挂了，健康检查应返回降级而非错误。
//
// 消费方（后续接入，本单不迁移）：hertz/health（DegradedError/Degraded
// 改薄委托本包）、bootstrap/healthz。
type DegradedError struct {
	// Err 是导致降级的底层错误，经 Unwrap 暴露，可被 errors.Is/As 链识别。
	Err error
}

// Error 实现 error 接口：Err 为 nil 时回退 "degraded"（裸降级标记，与
// hertz/health 口径一致）；否则透传底层错误文本。
func (e *DegradedError) Error() string {
	if e.Err == nil {
		return "degraded"
	}
	return e.Err.Error()
}

// Unwrap 返回底层错误，支持 errors.Is/As 链式判定。
func (e *DegradedError) Unwrap() error {
	return e.Err
}

// Degraded 将错误标记为软依赖降级（返回 *DegradedError 载体）。
// 典型形态：健康检查器里软依赖 ping 失败时返回 Degraded(err)——探针整体
// 仍算通过（HTTP 200 / 就绪），该项检查以降级状态呈现供告警识别。
//
// err 为 nil 时同样返回非 nil 的 *DegradedError（Error() 为 "degraded"）
// ——「标记降级」本身是事实，不需要底层错误也在场。
//
// 用法:
//
//	if err := cache.Ping(ctx); err != nil {
//	    return pkgerrors.Degraded(err) // 软依赖：降级但不摘流
//	}
//	return nil
func Degraded(err error) error {
	return &DegradedError{Err: err}
}
