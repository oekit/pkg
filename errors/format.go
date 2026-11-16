package errors

import "fmt"

// Newf 格式化构造错误（无错误链）——等价 fmt.Errorf 不带 %w 的形态。
//
// 仓规（#378 修订，见 doc.go）：错误构造单 import 收口——本包调用方
// 不再需要为「错误构造」面引入 fmt。需要错误链时用 Wrapf(err, ...)，
// 需要结构化 kv 上下文时用 Wrap(err, msg, kv...)，三态各司其职。
func Newf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

// Wrapf 带格式化消息包装错误，err 恒追加为尾参 %w（errors.Is/As 全链可辨）。
//
// 与 Wrap(err, msg, kv...) 的分工：Wrapf 走 fmt 文案（占位符插值），
// Wrap 走 kv 结构化上下文（经 Kv/KvFromChain 升格为结构化日志字段）。
// 内部对 args 做防御性拷贝后再追加 err——不突变调用方切片。
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	full := append(append(make([]any, 0, len(args)+1), args...), err)
	return fmt.Errorf(format+": %w", full...)
}
