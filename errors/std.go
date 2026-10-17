package errors

import (
	stderrors "errors"
)

// New 构造哨兵错误。与 stdlib errors.New 同型（errors.Is/As 可判定）。
// 补齐后下游的哨兵错误不再需要双 import（stdlib + oekit）——
// 「同一进程只允许一套 errors 包」的类型同一性红线才可完整执行（#8）。
func New(msg string) error { return stderrors.New(msg) }

// As 断言 err 链上是否有 target 指向类型的错误。等价 stdlib errors.As。
func As(err error, target any) bool { return stderrors.As(err, target) }

// Is 判定 err 链上是否有与 target 等价的错误。等价 stdlib errors.Is。
func Is(err, target error) bool { return stderrors.Is(err, target) }

// Unwrap 返回 err 的下一层错误。等价 stdlib errors.Unwrap（无下层返回 nil）。
func Unwrap(err error) error { return stderrors.Unwrap(err) }
