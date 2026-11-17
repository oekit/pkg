package errors

import (
	"fmt"
	"runtime/debug"
)

// panic→error 收编单源（#381 / 全 org 战役基石）。
//
// 本文件是 oekit 全矩阵 panic 收编的**唯一实现**：pkg 内所有
// `fmt.Errorf("...: %v", recover())` 形态的转换点都改走 Recovered，
// concurrency / core / timertask / workflow / bootstrap 五仓在 wave 2
// 消费同一 API——panic 值是 error 时身份不丢（errors.Is / errors.As 可
// 判定）、栈与类型信息恒在场，此前「error 分支 %w、其余 %v」的双分支
// 写法与「统一 %v 丢身份」的漂移都到头了。

// PanicError 是 panic 值收编为 error 后的载体。
//
// 三个承诺：
//   - error 身份不丢：panic 值本身是 error 时 Unwrap() 返回它——业务侧
//     panic(err)（常见形态）经收编后，调用方仍能 errors.Is / errors.As
//     到原始错误；
//   - 带发生时的 goroutine 栈：Stack 字段是捕获时刻的 debug.Stack() 输出
//     （defer recover 执行在 panic 现场 goroutine 上，栈即现场）；
//   - Error() 文本包含 panic 值类型信息：非 error 的 panic 值（string、
//     int、自定义 struct……）也能从日志/告警里看出它是什么类型。
type PanicError struct {
	// Value 是 recover() 返回的原始 panic 值。
	Value any

	// Stack 是 panic 发生时的 goroutine 栈（Recovered 内采集）。
	// 渲染为文本用 string(perr.Stack)。
	Stack []byte
}

// Error 实现 error。文本格式 "panic: <类型>: <值>"——panic 值的类型信息
// 始终在场（%T）；值本身是 error 时 %v 渲染其 Error() 文本。
func (e *PanicError) Error() string {
	return fmt.Sprintf("panic: %T: %v", e.Value, e.Value)
}

// Unwrap 返回 panic 值中的 error（panic 值不是 error 时返回 nil）。
// 这是「error 身份不丢」的机制所在：errors.Is / errors.As 穿透 PanicError
// 直达业务侧 panic 出去的原始错误。
func (e *PanicError) Unwrap() error {
	if err, ok := e.Value.(error); ok {
		return err
	}
	return nil
}

// Recovered 把 recover() 的返回值收编为 error：r 为 nil（无 panic）返回
// nil；否则返回携带栈与 error 身份的 *PanicError。
//
// 推荐惯用法（配合 defer + 具名返回值；无 panic 时 recover() 返回 nil、
// 本函数直接返回 nil，零包装开销）：
//
//	func worker() (err error) {
//	    defer func() {
//	        if perr := errors.Recovered(recover()); perr != nil {
//	            err = fmt.Errorf("worker: task panic: %w", perr)
//	        }
//	    }()
//	    ...
//	}
//
// 需要组件前缀时用 %w 再包一层（error 身份照旧透传）；不需要时把
// *PanicError 直接交给日志/上抛即可。
//
// ⚠️ 不要再写 `err = fmt.Errorf("...: %v", recover())`——%v 把 panic 值
// 拍平成纯文本，error 身份（errors.Is / errors.As）当场丢失，正是 #381
// 的病灶；包装纪律（对齐 pkg/internal/singleflight 的成文约定）「r 是
// error 时必须 %w 保留身份」已内建在本函数里，调用方无需再分叉。
func Recovered(r any) error {
	if r == nil {
		return nil
	}
	return &PanicError{Value: r, Stack: debug.Stack()}
}

// RecoverTo 是 [Recovered] 的一行式 defer 变体（#527）：返回值直接被 defer
// 调用，panic 值经 [Recovered] 收编后**直接赋值**给 *errp——
//
//	func worker() (err error) {
//	    defer errors.RecoverTo(&err)()
//	    ... // panic 值收编为 *PanicError 落入 err；正常返回时 err 不被触碰
//	}
//
// 与手写五行骨架完全同构（bootstrap 五处现有骨架即此形态）：
//
//	defer func() {
//	    if perr := errors.Recovered(recover()); perr != nil {
//	        err = perr
//	    }
//	}()
//
// 语义与纪律（与 [Recovered] 一致，#381 收编单源）：
//   - 无 panic 时 recover() 返回 nil、闭包零动作——err 保持调用方已赋的值
//     （业务错误不被触碰），正常返回路径零包装开销；
//   - panic 值是 error 时身份不丢：errors.Is / errors.As 可穿透 *PanicError
//     直达原始错误，栈与 panic 值类型信息恒在场；
//   - *errp 为**直接赋值（覆盖）**：panic 发生时，*errp 此前的值（通常为
//     nil；若 panic 前已赋业务错误则为该错误）被收编结果覆盖——对齐现有
//     骨架的 `err = perr` 直赋口径。panic 是比"此前赋值"更晚发生的失败
//     事实，以 *PanicError 呈现；原始 error 身份仍可经 Unwrap 链取回。
//
// errp 不能为 nil：构造期 fail-fast（仓规总则①），不等到 panic 收编时才炸
// ——那会把收编失败叠进正在传播的 panic，现场无法定位。
//
// ⚠️ 必须以 `defer RecoverTo(&err)()` 形态调用：返回的闭包被 defer **直接
// 调用**才处于 recover() 的生效位置。漏掉尾括号写成 `defer RecoverTo(&err)`
// 只 defer 了构造调用、返回的闭包被丢弃，panic 照常崩出（ recover 不生效）。
func RecoverTo(errp *error) func() {
	if errp == nil {
		panic("errors: RecoverTo(errp) 的 errp 不能为 nil")
	}
	return func() {
		if perr := Recovered(recover()); perr != nil {
			*errp = perr
		}
	}
}
