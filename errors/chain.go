package errors

import (
	"errors"
)

// Chain 顺序执行 fns，任一返回非 nil 立即停止并返回该错误；
// 全部成功返回 nil。适合"必须按序执行、前一步失败后续无意义"的场景
// （如多阶段清理：阶段间有依赖时不能像 MultiError 那样继续执行后续步骤）。
//
//	fns := []func() error{flushBuffers, closeFile, removeLock}
//	if err := errors.Chain(fns...); err != nil { ... }
func Chain(fns ...func() error) error {
	for _, fn := range fns {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

// In 报告 err 是否匹配 targets 中的任意一个（errors.Is 语义，
// 支持 wrapped 错误链与 Is 自定义匹配）。
//
//	if errors.In(err, ErrNotFound, ErrForbidden) { ... }
func In(err error, targets ...error) bool {
	for _, target := range targets {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}
