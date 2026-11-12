package errors

import (
	stderrors "errors"
	"fmt"
)

// Wrap 用 msg 包装 err 并返回新 err。err 为 nil 时返回 nil(不创建无意义包装)。
// kv 列表作为附加上下文附加到包装消息(用于日志关联,例如 operation 名)。
//
// 跟 fmt.Errorf("...: %w", err) 的区别: Wrap 显式 nil-safe(避免
// "%!w(<nil>)" 之类输出),并支持附加 kv 对。
//
// 业务场景:
//
//	if err := db.Query(...); err != nil {
//	    return errors.Wrap(err, "query user", "user_id", uid)
//	}
//
// 输出: "query user: <原始 err> user_id=42"(kv 用 = 分隔拼接)
// 解包: errors.Is / errors.As 仍能识别原始 err(走 Unwrap)。
//
// kv 还有**结构化出口**（#292）：errors.Kv(err) 取最外层 kv、
// errors.KvFromChain(err) 沿链全量收集——交给结构化日志时把 kv 升格为
// 独立字段（检索/聚合/过滤全通），错误文本照旧留在 err 字段。
func Wrap(err error, msg string, kv ...any) error {
	if err == nil {
		return nil
	}
	// 拼接 msg + ": " + err.Error()
	// 加上 kv 后缀(每对 key+value 用 = 拼接)
	if len(kv) == 0 {
		return &wrappedError{msg: msg, err: err}
	}
	return &wrappedError{msg: msg, err: err, kv: kv}
}

// wrappedError 是 Wrap 内部用的结构,实现 Unwrap + 携带 kv。
type wrappedError struct {
	msg string
	err error
	kv  []any
}

func (w *wrappedError) Error() string {
	if len(w.kv) == 0 {
		return w.msg + ": " + w.err.Error()
	}
	return w.msg + ": " + w.err.Error() + " " + formatKV(w.kv)
}

// Unwrap 让 errors.Is / errors.As 能识别原始 err。
func (w *wrappedError) Unwrap() error {
	return w.err
}

// Kv 返回本层 Wrap 附带的上下文 kv 对（#292 的结构化出口；经 KVCarrier
// 接口被 errors.Kv / errors.KvFromChain 与 logx 类结构化日志出口消费）。
func (w *wrappedError) Kv() []any {
	return w.kv
}

// formatKV 把 k/v 对格式化成 "k1=v1 k2=v2" 形式。
// 奇数长度时保留最后一个参数并渲染为 "!BADKEY=<value>"（对齐 log/slog 约定），
// 不再静默丢弃——漏传 value 的编程错误必须在输出里留下痕迹。
func formatKV(kv []any) string {
	if len(kv) == 0 {
		return ""
	}
	var sb []byte
	for i := 0; i < len(kv); i += 2 {
		if i > 0 {
			sb = append(sb, ' ')
		}
		if i+1 >= len(kv) {
			sb = append(sb, fmt.Sprintf("!BADKEY=%v", kv[i])...)
			break
		}
		sb = append(sb, fmt.Sprintf("%v=%v", kv[i], kv[i+1])...)
	}
	return string(sb)
}

// Kv() []any 是本仓 Wrap 上下文 kv 的**结构化读取口**（#292）。实现本接口
// 的错误可被 logx 等结构化日志出口升格为独立字段，而不是退化成 error 字符串
// 里的一段文本。
type KVCarrier interface {
	error
	// Kv 返回本层附带的上下文 kv 对（无 kv 返回 nil）。
	Kv() []any
}

// Kv 返回最外层 Wrap 附带的上下文 kv 对（#292）：无 kv 或 err 不携带 kv
// （非本仓 Wrap 构造、已到链底）返回 nil。
//
// 要**全量**收集多层 Wrap 的上下文用 KvFromChain。logx 侧消费形态（#412：
// 错误字段名示范用 logx.ErrKey 常量——消费方业务仓有 logx 依赖，禁止手写
// "err" 字面量；pkg 自身零依赖底座引不了 logx，内部以 internal/logkv.ErrKey
// 作上游镜像，值与 logx.ErrKey 逐字一致）：
//
//	err := errors.Wrap(err, "query user", "user_id", uid)
//	logger.Error(ctx, "query failed", logx.ErrKey, err)   // err 字段保持文本
//	// kv 升格为独立字段（检索/聚合/按 user_id 过滤全通）：
//	logger.Error(ctx, append([]any{logx.ErrKey, err}, errors.Kv(err)...)...)
func Kv(err error) []any {
	if c, ok := err.(KVCarrier); ok {
		return c.Kv()
	}
	return nil
}

// KvFromChain 沿错误链收集**每层** Wrap 附带的 kv 对（#292）：多层 Wrap 时
// 上下文全量展开，外层在前、内层在后（同名 key 由日志端的后写覆盖语义决定
// 最终值——与 slog Attrs 的重复 key 行为一致）。链上多层 kv 不做去重：
// 去重会掩盖「两层都带了 user_id 但值不同」的异常现场。多分支链
// （Unwrap() []error，如 Errorf 的多 %w 形态）按深度先序遍历。
// err 为 nil 或整条链都不携带 kv 时返回 nil。
//
// 典型消费（logx 侧 ErrAttrs 形态）：
//
//	func ErrAttrs(err error) []slog.Attr {
//	    var attrs []slog.Attr
//	    for i := 0; i < len(kv); i += 2 { attrs = append(attrs, ...) }
//	}
func KvFromChain(err error) []any {
	var out []any
	var walk func(e error, depth int)
	walk = func(e error, depth int) {
		if e == nil || depth >= maxFlattenDepth {
			// 深度上限对齐 biz_error.go 的 maxFlattenDepth 纪律（#420）：
			// 成环链（Unwrap() error 自指、或 Unwrap() []error 互指回祖先
			// ——biz_error.go 注释承认的重构/聚合形态）此前无上限：单分支
			// 环是永久自旋（goroutine 卡死 + CPU 打满），多分支环是无限
			// 递归 stack overflow **打崩进程**。kv 收集是日志辅助面，
			// 超限宁缺勿崩——正常业务链远浅于 32。
			return
		}
		if c, ok := e.(KVCarrier); ok {
			if kv := c.Kv(); len(kv) > 0 {
				out = append(out, kv...)
			}
		}
		// 多分支链（Unwrap() []error，如 Errorf 的多 %w 形态）深度先序；
		// stderrors.Unwrap 只认 Unwrap() error，多错误形态对它返回 nil，
		// 必须先走类型断言分支。
		if m, ok := e.(interface{ Unwrap() []error }); ok {
			for _, sub := range m.Unwrap() {
				walk(sub, depth+1)
			}
			return
		}
		if u := stderrors.Unwrap(e); u != nil {
			walk(u, depth+1)
		}
	}
	walk(err, 0)
	return out
}

// Cause 是 errors.Cause 的便捷版:返回 wrap 链最底层的 error。
// 等价 errors.Unwrap 递归到 nil。
//
// 多分支链（Unwrap() []error，Go 1.20+ 多错误解包，如 Errorf 的多 %w 形态）
// 取**首分支**继续下钻（#420）：单值语义的 Cause 在多分支形态没有「唯一
// 最底层」，首分支与 KvFromChain 的深度先序口径一致。stdlib 语义优先——
// 同时实现两种 Unwrap 的类型先走单分支（errors.Unwrap 的既有序）。
func Cause(err error) error {
	// 深度上限对齐 biz_error.go 的 maxFlattenDepth 纪律：成环 Unwrap 链
	// （A 包 B、B 又包 A——biz_error.go 注释承认的重构/聚合形态）会让
	// 无界 for 永久迭代（goroutine 卡死 + CPU 自旋，#298）。超限返回当前
	// 已到达的最深节点并停止。
	for range maxFlattenDepth {
		if u := stderrors.Unwrap(err); u != nil {
			err = u
			continue
		}
		// 单分支到底：多分支链（含 Errorf 多 %w、Join 聚合）取首分支下钻。
		if m, ok := err.(interface{ Unwrap() []error }); ok {
			subs := m.Unwrap()
			if len(subs) == 0 {
				return err
			}
			err = subs[0]
			continue
		}
		return err
	}
	return err
}
