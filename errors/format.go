package errors

import (
	stderrors "errors"
	"fmt"
	"strings"
)

// Errorf 构造带格式化的错误，支持 %w 包装（可出现多个），与 fmt.Errorf 同型。
//
// 与 Wrap 的分工：Errorf 用于「消息里带动态值」，Wrap 用于「固定消息 + kv 上下文」。
// 两者互补而非替代：需要「动态消息 + 包装 + kv 上下文」时，先 Errorf 再 Wrap。
//
// 相对 fmt.Errorf 的差异（都写在这里，行为可预期）：
//
//   - nil-safe：%w 的实参为 nil 时**不进包装链**，消息按 %v 渲染为 "<nil>"。
//     fmt.Errorf 在此产出 "%!w(<nil>)" 噪声，且整条包装链被丢弃——这正是
//     Wrap 当初要解决的坑（见 Wrap 的注释），新原语不重蹈。
//   - %w 的实参不是 error 时同样按 %v 渲染、不进链（fmt.Errorf 产出 "%!w(...)"）。
//   - 格式化结果为空串时返回 nil（与 Wrap(nil, ...) → nil 同一取向）。
//   - 多个 %w 时包装链是 Unwrap() []error（Go 1.20+ 多错误形态），
//     errors.Is / errors.As 对链上每个错误都生效。
//
// 典型用法：
//
//	if err := db.Query(ctx, q); err != nil {
//	    return errors.Errorf("load profile %s: %w", userID, err)
//	}
//
// 消息渲染与 fmt.Errorf 一致（%w 位置渲染被包装错误的 Error() 文本），
// 因此从 fmt.Errorf 迁过来时日志与告警的文案不会变。
func Errorf(format string, args ...any) error {
	idxs, rewritten := parseFormatW(format)

	// 收集要包装的错误：%w 的实参是 error 且非 nil 才入链。
	var wrapped []error
	for _, idx := range idxs {
		if idx < 0 || idx >= len(args) {
			continue
		}
		err, ok := args[idx].(error)
		if !ok || err == nil {
			continue
		}
		wrapped = append(wrapped, err)
	}

	msg := fmt.Sprintf(rewritten, args...)
	if msg == "" {
		return nil
	}
	switch len(wrapped) {
	case 0:
		return stderrors.New(msg)
	case 1:
		return &formattedError{msg: msg, err: wrapped[0]}
	default:
		return &formattedErrors{msg: msg, errs: wrapped}
	}
}

// formattedError 是 Errorf 单 %w 时的包装体：单层解包。
type formattedError struct {
	msg string
	err error
}

func (e *formattedError) Error() string { return e.msg }

// Unwrap 让 errors.Is / errors.As 能识别被包装的原始错误。
func (e *formattedError) Unwrap() error { return e.err }

// formattedErrors 是 Errorf 多 %w 时的包装体：多层解包（Go 1.20+）。
type formattedErrors struct {
	msg  string
	errs []error
}

func (e *formattedErrors) Error() string { return e.msg }

// Unwrap 支持 errors.Is / errors.As 遍历链上每个被包装的错误
// （Go 1.20 多错误解包）。与 stdlib fmt.Errorf 多 %w 的形态一致。
func (e *formattedErrors) Unwrap() []error { return e.errs }

// parseFormatW 扫描 format，返回每个 %w 动词对应的实参下标，
// 以及把 %w 换成 %v 后的 format。
//
// 换成 %v 的理由：%v 与 %w 对 error 实参的渲染完全一致（都走 Error()），
// 而 %v 遇 nil 实参渲染 "<nil>"，不像 %w 那样产出 "%!w(<nil>)"。
// 换成 %v 后就能直接用 fmt.Sprintf 渲染消息，不必依赖 fmt.Errorf 内部
// 只对首个 %w 生效的包装语义。
//
// 实参计数规则对齐 fmt/print.go：每个动词消耗一个实参；width / precision
// 的 '*' 各额外消耗一个；显式下标 %[n] 优先，其后从 n+1 继续。
func parseFormatW(format string) (idxs []int, rewritten string) {
	var sb strings.Builder
	sb.Grow(len(format))
	next := 0 // 下一个顺序实参下标
	for i := 0; i < len(format); {
		if format[i] != '%' {
			sb.WriteByte(format[i])
			i++
			continue
		}
		start := i
		i++ // 跳过 '%'
		if i >= len(format) {
			sb.WriteString(format[start:])
			break
		}
		if format[i] == '%' { // %% = 字面量 %
			sb.WriteString(format[start : i+1])
			i++
			continue
		}
		// 显式实参下标 %[n]（fmt 的下标是 **1 基**）
		explicit := -1
		if format[i] == '[' {
			j := i + 1
			n := 0
			for j < len(format) && format[j] >= '0' && format[j] <= '9' {
				n = n*10 + int(format[j]-'0')
				j++
			}
			if j < len(format) && format[j] == ']' && j > i+1 {
				explicit = n - 1 // 转成 0 基下标
				i = j + 1
			}
		}
		// 标志位
		for i < len(format) && strings.IndexByte("+-# 0", format[i]) >= 0 {
			i++
		}
		// 宽度
		if i < len(format) && format[i] == '*' {
			i++
			next++ // '*' 消耗一个实参
		} else {
			for i < len(format) && format[i] >= '0' && format[i] <= '9' {
				i++
			}
		}
		// 精度
		if i < len(format) && format[i] == '.' {
			i++
			if i < len(format) && format[i] == '*' {
				i++
				next++ // '*' 消耗一个实参
			} else {
				for i < len(format) && format[i] >= '0' && format[i] <= '9' {
					i++
				}
			}
		}
		if i >= len(format) {
			sb.WriteString(format[start:])
			break
		}
		verbPos := i
		verb := format[i]
		i++

		argIdx := explicit
		if argIdx < 0 {
			argIdx = next
		}
		next = argIdx + 1

		if verb == 'w' {
			idxs = append(idxs, argIdx)
			sb.WriteString(format[start:verbPos])
			sb.WriteByte('v')
		} else {
			sb.WriteString(format[start : verbPos+1])
		}
	}
	return idxs, sb.String()
}
