package errors

import (
	stderrors "errors"
	"fmt"
	"strings"
)

// BizError 是业务层错误类型，携带业务码（BizCode）和 HTTP 状态码（HTTPCode）。
// 在 service 层返回此错误后，handler 层可通过 errors.As 提取 code 和 message。
//
// 字段语义（v0.17.0 起分离）：
//   - BizCode：业务语义标识，写入响应 body 的 code 字段（如 1001、40404）
//   - HTTPCode：HTTP 状态码；为 0 时由 middleware.StatusMapper 从 BizCode 推导
//
// 用法:
//
//	return nil, errors.NewNotFoundError("项目不存在")
//	return nil, errors.NewBadRequestError("keyword 不能为空")
//	return nil, errors.NewBizError(1001, "余额不足")              // HTTPCode=0, 走 mapper
//	return nil, errors.NewBizErrorWithStatus(1001, 402, "余额不足") // 显式两码
type BizError struct {
	BizCode  int    `json:"biz_code"`  // 业务码（语义标识）
	HTTPCode int    `json:"http_code"` // HTTP 状态码（0 = 由 StatusMapper 推导）
	Message  string `json:"message"`   // 业务描述
	Cause    error  `json:"-"`         // 原始错误（可选）
}

// maxFlattenDepth 是 Error() 展开 cause 链的深度上限（防御成环链——
// A 包 B、B 又包 A 时 Error() 不得死循环；正常业务链远浅于此）。
const maxFlattenDepth = 32

// Error 实现 error 接口。
// 包含 BizCode 以确保日志中不丢失业务码上下文。
//
// 格式（#202 起钉死，表驱动测试钉住）：
//
//		biz_code:%d[ http_code:%d] message[: cause 片段]*
//
//	  - http_code 段仅在 HTTPCode != 0 时出现（显式两码分离的错误两码都
//	    可见，单码错误不带 http_code:0 噪声）——Error() 与 String() 统一
//	    走本格式（此前 String() 恒带 http_code 且丢 cause，同一错误两种
//	    字符串：日志走 Stringer、响应走 Error 时信息量不一致）；
//	  - cause 链上的 **BizError 只贡献 message**，不再嵌 biz_code:%d 前缀
//	    ——嵌套包装时外层码遮蔽内层码、双码混排外泄到用户可见消息的问题
//	    由此消除（外层是包装语义，最外层码即响应码；内层码经 errors.As/
//	    AsBizError 仍完整可取）；
//	  - cause 链上首个非 BizError 错误按其自身 Error() 渲染并终止展开
//	    （它自带完整的下层链文本）。
//
// nil 接收者不 panic：typed-nil（`var e *BizError; var err error = e`）是
// Go 里极常见的形态——函数返回 *BizError 而调用方把它赋给 error 接口时，
// 接口本身非 nil。此时任何日志/格式化路径调用 Error() 都会空指针崩溃，
// 把「错误处理出错了」变成「进程崩了」。
func (e *BizError) Error() string {
	if e == nil {
		return "<nil *BizError>"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "biz_code:%d", e.BizCode)
	if e.HTTPCode != 0 {
		fmt.Fprintf(&b, " http_code:%d", e.HTTPCode)
	}
	b.WriteByte(' ')
	b.WriteString(e.Message)

	// cause 链扁平化：BizError 只贡献 message，非 BizError 按自身
	// Error() 渲染后终止。逐层直断（不用 errors.As——As 会跳过中间
	// 包装层，丢掉它们的上下文文本）。
	cur := e.Cause
	for depth := 0; cur != nil && depth < maxFlattenDepth; depth++ {
		if inner, ok := cur.(*BizError); ok {
			if inner == nil || inner == e { // typed-nil / 自引用防御
				cur = nil // 链在此终止，不是深度截断
				break
			}
			b.WriteString(": ")
			b.WriteString(inner.Message)
			cur = inner.Cause
			continue
		}
		b.WriteString(": ")
		b.WriteString(cur.Error())
		cur = nil // 非 BizError 的 Error() 已含其完整下层链，展开终止
		break
	}
	if cur != nil { // 深度上限击穿（成环链防御）——如实标注，不静默截断
		b.WriteString(": ...(cause 链过深，已截断)")
	}
	return b.String()
}

// Unwrap 实现 errors.Unwrap 接口，支持 errors.Is / errors.As 链式匹配。
func (e *BizError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// String 实现 fmt.Stringer 接口。与 Error() **完全一致**（#202：此前
// String() 恒带 http_code:%d 且不带 cause，与 Error() 分叉——同一错误
// 两种字符串，日志与响应的信息量不一致。现在统一走同一格式，格式契约
// 见 Error）。
func (e *BizError) String() string {
	return e.Error()
}

// NewBizError 创建一个自定义业务码错误，HTTPCode 留空（0），
// handler 层会经 StatusMapper 从 BizCode 推导 HTTP 状态码。
//
// 适用于业务码与 HTTP 对齐的场景（如 BizCode=404 → HTTP 404），
// 或接受默认映射（BizCode=1001 → HTTP 400）。
// 需要显式分离两码时使用 NewBizErrorWithStatus。
func NewBizError(code int, msg string) *BizError {
	return &BizError{BizCode: code, Message: msg}
}

// NewBizErrorWithStatus 创建一个显式指定 HTTP 状态码的业务错误。
// 适用于业务码与 HTTP 状态码需要分离的场景（如 BizCode=1001 但需返回 HTTP 402）。
func NewBizErrorWithStatus(bizCode, httpCode int, msg string) *BizError {
	return &BizError{BizCode: bizCode, HTTPCode: httpCode, Message: msg}
}

// NewNotFoundError 创建 404 业务错误（资源不存在）。
func NewNotFoundError(msg string) *BizError {
	return &BizError{BizCode: 404, HTTPCode: 404, Message: msg}
}

// NewBadRequestError 创建 400 业务错误（请求参数错误）。
func NewBadRequestError(msg string) *BizError {
	return &BizError{BizCode: 400, HTTPCode: 400, Message: msg}
}

// NewUnauthorizedError 创建 401 业务错误（未认证）。
func NewUnauthorizedError(msg string) *BizError {
	return &BizError{BizCode: 401, HTTPCode: 401, Message: msg}
}

// NewForbiddenError 创建 403 业务错误（无权限）。
func NewForbiddenError(msg string) *BizError {
	return &BizError{BizCode: 403, HTTPCode: 403, Message: msg}
}

// NewConflictError 创建 409 业务错误（资源冲突）。
func NewConflictError(msg string) *BizError {
	return &BizError{BizCode: 409, HTTPCode: 409, Message: msg}
}

// WrapBizError 包装原始错误为业务错误，HTTPCode 留空（0），
// handler 层经 StatusMapper 从 BizCode 推导。
func WrapBizError(code int, msg string, cause error) *BizError {
	return &BizError{BizCode: code, Message: msg, Cause: cause}
}

// WrapBizErrorWithStatus 包装原始错误为业务错误，显式指定 HTTP 状态码。
// 字符串形态与 WrapBizError 同构（#202）：Error()/String() 均带 cause——
// 此前 cause 从不出现在本构造的错误字符串里（Unwrap 链在、字符串诊断链
// 断头），双码错误的 console_log 看不到内层原因。
func WrapBizErrorWithStatus(bizCode, httpCode int, msg string, cause error) *BizError {
	return &BizError{BizCode: bizCode, HTTPCode: httpCode, Message: msg, Cause: cause}
}

// IsBizError 检查错误是否为 BizError 类型。
//
// typed-nil（`var e *BizError; var err error = e`）不算命中：接口非 nil 但
// 指向 nil 指针，此时按文档取 b.BizCode 就是空指针解引用。
func IsBizError(err error) bool {
	bizErr, ok := AsBizError(err)
	return ok && bizErr != nil
}

// AsBizError 从错误链中提取 BizError。
//
// 第二个返回值在「链上是 typed-nil *BizError」时为 false——此时 bizErr 为
// nil 指针，按文档用法 `if ok { use b.BizCode }` 直接用就会崩。
func AsBizError(err error) (*BizError, bool) {
	var bizErr *BizError
	if stderrors.As(err, &bizErr) && bizErr != nil {
		return bizErr, true
	}
	return nil, false
}
