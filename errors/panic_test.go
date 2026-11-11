package errors

import (
	stderrors "errors"
	"fmt"
	"strings"
	"testing"
)

// #381 回归：panic→error 收编必须保留 error 身份、带栈、带类型信息。
// 本文件同时是全矩阵（concurrency/core/timertask/workflow/bootstrap）消费
// Recovered 的行为契约。

func TestRecovered_NilIsNil(t *testing.T) {
	// 无 panic（recover() 返回 nil）必须返回 nil——defer 惯用法的零开销路径。
	if err := Recovered(nil); err != nil {
		t.Fatalf("Recovered(nil) 应返回 nil, got %v", err)
	}
}

func TestRecovered_ErrorValue_IdentityPreserved(t *testing.T) {
	sentinel := stderrors.New("disk on fire")
	var perr error
	func() {
		defer func() {
			perr = Recovered(recover())
		}()
		panic(sentinel) // 业务侧 panic(err) 的常见形态
	}()

	if perr == nil {
		t.Fatal("Recovered 应把 panic 值收编为非 nil error")
	}
	// error 身份不丢：errors.Is 穿透 PanicError 直达原始错误。
	if !Is(perr, sentinel) {
		t.Fatal("errors.Is 应穿透 PanicError 命中 panic 出去的原始错误")
	}
	var pe *PanicError
	if !As(perr, &pe) {
		t.Fatal("errors.As 应能取回 *PanicError 载体")
	}
	// Error() 文本必须包含 panic 值类型信息。
	if !strings.Contains(perr.Error(), "*errors.errorString") {
		t.Fatalf("Error() 文本应包含 panic 值类型信息, got %q", perr.Error())
	}
	// 栈必须在场且包含 panic 现场帧。
	if len(pe.Stack) == 0 {
		t.Fatal("PanicError.Stack 应非空")
	}
	if !strings.Contains(string(pe.Stack), "TestRecovered_ErrorValue_IdentityPreserved") {
		t.Fatalf("栈应包含 panic 现场帧, got:\n%s", pe.Stack)
	}
}

func TestRecovered_NonErrorValue_TypeInfo(t *testing.T) {
	var perr error
	func() {
		defer func() {
			perr = Recovered(recover())
		}()
		panic("boom string")
	}()

	if perr == nil {
		t.Fatal("非 error panic 值同样应收编为非 nil error")
	}
	want := "panic: string: boom string"
	if perr.Error() != want {
		t.Fatalf("Error() = %q, want %q", perr.Error(), want)
	}
	if Unwrap(perr) != nil {
		t.Fatalf("非 error panic 值 Unwrap 应为 nil, got %v", Unwrap(perr))
	}
}

func TestRecovered_DeferIdiom_WrapsWithErrorIdentity(t *testing.T) {
	sentinel := stderrors.New("task failed")
	// godoc 推荐惯用法的端到端形态：defer + 具名返回值 + %w 再包一层。
	runTask := func() (err error) {
		defer func() {
			if perr := Recovered(recover()); perr != nil {
				err = fmt.Errorf("worker: task panic: %w", perr)
			}
		}()
		panic(sentinel)
	}

	err := runTask()
	if !Is(err, sentinel) {
		t.Fatalf("两层包装后 errors.Is 仍应命中原始错误, got %v", err)
	}
	if !strings.Contains(err.Error(), "worker: task panic:") ||
		!strings.Contains(err.Error(), "panic: *errors.errorString") {
		t.Fatalf("文本应同时含组件前缀与 panic 类型信息, got %q", err.Error())
	}
}
