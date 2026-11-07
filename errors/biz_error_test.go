package errors

import (
	stderrors "errors"
	"fmt"
	"strings"
	"testing"
)

func TestBizError_Error(t *testing.T) {
	err := NewBizError(400, "bad request")
	expected := "biz_code:400 bad request"
	if err.Error() != expected {
		t.Errorf("expected '%s', got '%s'", expected, err.Error())
	}

	// with cause
	cause := stderrors.New("original error")
	err2 := WrapBizError(500, "internal error", cause)
	expected2 := "biz_code:500 internal error: original error"
	if err2.Error() != expected2 {
		t.Errorf("expected '%s', got '%s'", expected2, err2.Error())
	}
}

func TestBizError_Unwrap(t *testing.T) {
	cause := stderrors.New("root cause")
	err := WrapBizError(500, "wrapped", cause)

	if !stderrors.Is(err, cause) {
		t.Error("expected errors.Is to match cause")
	}
}

func TestBizError_String(t *testing.T) {
	// #202 起 String() 与 Error() 统一（同一格式，见 TestBizError_Format）。
	err := NewBizErrorWithStatus(404, 404, "not found")
	expected := "biz_code:404 http_code:404 not found"
	if err.String() != expected {
		t.Errorf("expected '%s', got '%s'", expected, err.String())
	}
	if err.String() != err.Error() {
		t.Errorf("String() 应与 Error() 完全一致, error='%s' string='%s'", err.Error(), err.String())
	}
}

// TestBizError_Format 表驱动钉住 #202 统一后的字符串格式：
//
//	biz_code:%d[ http_code:%d] message[: cause 片段]*
//
// 覆盖：单码、双码、普通 cause、嵌套 BizError 扁平化（内层只贡献
// message，不嵌 biz_code 前缀）、中间夹非 BizError 包装、深层链。
func TestBizError_Format(t *testing.T) {
	cases := []struct {
		name string
		err  *BizError
		want string
	}{
		{
			name: "单码无cause",
			err:  NewBizError(400, "bad request"),
			want: "biz_code:400 bad request",
		},
		{
			name: "双码无cause",
			err:  NewBizErrorWithStatus(1001, 402, "余额不足"),
			want: "biz_code:1001 http_code:402 余额不足",
		},
		{
			name: "单码普通cause",
			err:  WrapBizError(500, "internal error", stderrors.New("original error")),
			want: "biz_code:500 internal error: original error",
		},
		{
			name: "双码带cause",
			err:  WrapBizErrorWithStatus(503, 500, "update image failed", stderrors.New("argocd client not initialized")),
			want: "biz_code:503 http_code:500 update image failed: argocd client not initialized",
		},
		{
			name: "嵌套BizError扁平化-内层只贡献message",
			err: WrapBizError(500, "update image failed",
				WrapBizError(503, "argocd client not initialized", nil)),
			want: "biz_code:500 update image failed: argocd client not initialized",
		},
		{
			name: "三层嵌套逐层串联message",
			err: WrapBizError(500, "deploy failed",
				WrapBizError(503, "update image failed",
					WrapBizError(502, "argocd unreachable", nil))),
			want: "biz_code:500 deploy failed: update image failed: argocd unreachable",
		},
		{
			name: "BizError夹非BizError包装",
			err: WrapBizError(500, "deploy failed",
				fmt.Errorf("rollout: %w", stderrors.New("connection refused"))),
			want: "biz_code:500 deploy failed: rollout: connection refused",
		},
		{
			name: "嵌套链末端是非BizError",
			err: WrapBizError(500, "update image failed",
				WrapBizError(503, "argocd client not initialized",
					stderrors.New("dial tcp: refused"))),
			want: "biz_code:500 update image failed: argocd client not initialized: dial tcp: refused",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Fatalf("Error() 格式不符\n want: %s\n got:  %s", tc.want, got)
			}
			if got := tc.err.String(); got != tc.want {
				t.Fatalf("String() 应与 Error() 同格式\n want: %s\n got:  %s", tc.want, got)
			}
		})
	}
}

// TestBizError_FormatTypedNil typed-nil 接收者不 panic。
func TestBizError_FormatTypedNil(t *testing.T) {
	var e *BizError
	if got := e.Error(); got != "<nil *BizError>" {
		t.Fatalf("typed-nil Error(): got %q", got)
	}
	if got := e.String(); got != "<nil *BizError>" {
		t.Fatalf("typed-nil String(): got %q", got)
	}
	// cause 是 typed-nil *BizError 时展开安全终止
	e = &BizError{BizCode: 1, Message: "m", Cause: (*BizError)(nil)}
	if got := e.Error(); got != "biz_code:1 m" {
		t.Fatalf("typed-nil cause 展开应安全终止, got %q", got)
	}
}

// TestBizError_FormatCycleDefense 成环 cause 链不死循环（防御位）。
// 两种成环形态：环回到接收者自身（自引用守卫干净终止）与环不经过
// 接收者（深度上限兜底，如实标注截断）。
func TestBizError_FormatCycleDefense(t *testing.T) {
	// 形态一：环回到接收者自身
	a := &BizError{BizCode: 1, Message: "a"}
	b := &BizError{BizCode: 2, Message: "b", Cause: a}
	a.Cause = b
	if got := a.Error(); got != "biz_code:1 a: b" {
		t.Fatalf("自引用环应在回到自身处终止, got %q", got)
	}

	// 形态二：环不经过接收者——深度上限兜底
	c := &BizError{BizCode: 3, Message: "c"}
	d := &BizError{BizCode: 4, Message: "d", Cause: c}
	c.Cause = d // c ↔ d 成环
	e := WrapBizError(1, "a", c)
	got := e.Error()
	if !strings.HasPrefix(got, "biz_code:1 a: c: d") {
		t.Fatalf("成环链应正常展开前几层, got %q", got)
	}
	if !strings.Contains(got, "已截断") {
		t.Fatalf("成环链应在深度上限处如实标注截断, got %q", got)
	}
}

// TestBizError_UnwrapChainIntact 字符串扁平化不改变 Unwrap/As/Is 链式匹配
// （内层 BizError 仍完整可取——扁平化只影响字符串渲染）。
func TestBizError_UnwrapChainIntact(t *testing.T) {
	inner := NewBizError(503, "argocd client not initialized")
	outer := WrapBizError(500, "update image failed", inner)

	var got *BizError
	if !stderrors.As(outer, &got) || got != outer {
		t.Fatal("As 应首先命中链上最外层 BizError")
	}
	// 经 Unwrap 逐层下钻可取到内层码
	if u, ok := outer.Unwrap().(*BizError); !ok || u.BizCode != 503 {
		t.Fatalf("Unwrap 应保留内层 BizError（码 503）, got %v", outer.Unwrap())
	}
}

func TestNewNotFoundError(t *testing.T) {
	err := NewNotFoundError("user not found")
	if err.BizCode != 404 || err.HTTPCode != 404 {
		t.Errorf("expected biz/http 404, got biz=%d http=%d", err.BizCode, err.HTTPCode)
	}
	if err.Message != "user not found" {
		t.Errorf("expected 'user not found', got '%s'", err.Message)
	}
}

func TestNewBadRequestError(t *testing.T) {
	err := NewBadRequestError("invalid param")
	if err.BizCode != 400 || err.HTTPCode != 400 {
		t.Errorf("expected biz/http 400, got biz=%d http=%d", err.BizCode, err.HTTPCode)
	}
}

func TestNewUnauthorizedError(t *testing.T) {
	err := NewUnauthorizedError("token expired")
	if err.BizCode != 401 || err.HTTPCode != 401 {
		t.Errorf("expected biz/http 401, got biz=%d http=%d", err.BizCode, err.HTTPCode)
	}
}

func TestNewForbiddenError(t *testing.T) {
	err := NewForbiddenError("no permission")
	if err.BizCode != 403 || err.HTTPCode != 403 {
		t.Errorf("expected biz/http 403, got biz=%d http=%d", err.BizCode, err.HTTPCode)
	}
}

func TestNewConflictError(t *testing.T) {
	err := NewConflictError("duplicate entry")
	if err.BizCode != 409 || err.HTTPCode != 409 {
		t.Errorf("expected biz/http 409, got biz=%d http=%d", err.BizCode, err.HTTPCode)
	}
}

// TestNewBizErrorWithStatus 验证显式两码分离构造（v0.17.0 新增）。
func TestNewBizErrorWithStatus(t *testing.T) {
	err := NewBizErrorWithStatus(1001, 402, "insufficient balance")
	if err.BizCode != 1001 || err.HTTPCode != 402 {
		t.Errorf("expected biz=1001 http=402, got biz=%d http=%d", err.BizCode, err.HTTPCode)
	}
	// NewBizError 仅设 BizCode，HTTPCode 应为 0（走 mapper 回退）
	basic := NewBizError(1001, "msg")
	if basic.BizCode != 1001 || basic.HTTPCode != 0 {
		t.Errorf("expected biz=1001 http=0, got biz=%d http=%d", basic.BizCode, basic.HTTPCode)
	}
}

func TestIsBizError(t *testing.T) {
	bizErr := NewBizError(400, "bad")
	if !IsBizError(bizErr) {
		t.Error("expected IsBizError to return true for BizError")
	}

	normalErr := stderrors.New("normal error")
	if IsBizError(normalErr) {
		t.Error("expected IsBizError to return false for non-BizError")
	}

	// wrapped error
	wrapped := fmt.Errorf("context: %w", bizErr)
	if !IsBizError(wrapped) {
		t.Error("expected IsBizError to return true for wrapped BizError")
	}
}

func TestAsBizError(t *testing.T) {
	bizErr := NewBizError(400, "bad request")

	extracted, ok := AsBizError(bizErr)
	if !ok {
		t.Fatal("expected AsBizError to return true")
	}
	if extracted.BizCode != 400 {
		t.Errorf("expected biz_code 400, got %d", extracted.BizCode)
	}

	// non-BizError
	normalErr := stderrors.New("normal")
	_, ok = AsBizError(normalErr)
	if ok {
		t.Error("expected AsBizError to return false for non-BizError")
	}

	// wrapped
	wrapped := fmt.Errorf("wrap: %w", bizErr)
	extracted, ok = AsBizError(wrapped)
	if !ok {
		t.Fatal("expected AsBizError to return true for wrapped")
	}
	if extracted.BizCode != 400 {
		t.Errorf("expected biz_code 400, got %d", extracted.BizCode)
	}
}
