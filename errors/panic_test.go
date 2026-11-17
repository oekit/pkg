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

// #527：RecoverTo 一行式 defer 收编的行为契约。

func TestRecoverTo_PanicError_IdentityPreserved(t *testing.T) {
	cases := []struct {
		name    string
		panicV  any
		wantTxt string // Error() 文本必须包含的片段
	}{
		{"error 值", stderrors.New("disk on fire"), "panic: *errors.errorString: disk on fire"},
		{"string 值", "boom string", "panic: string: boom string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := func() (err error) {
				defer RecoverTo(&err)()
				panic(tc.panicV)
			}

			err := run()
			if err == nil {
				t.Fatal("panic 值应收编为非 nil error")
			}
			if err.Error() != tc.wantTxt {
				t.Fatalf("Error() = %q, want %q", err.Error(), tc.wantTxt)
			}
			var pe *PanicError
			if !As(err, &pe) {
				t.Fatal("errors.As 应能取回 *PanicError 载体")
			}
			if len(pe.Stack) == 0 {
				t.Fatal("PanicError.Stack 应非空")
			}
			if !strings.Contains(string(pe.Stack), "TestRecoverTo_PanicError_IdentityPreserved") {
				t.Fatalf("栈应包含 panic 现场帧, got:\n%s", pe.Stack)
			}
		})
	}
}

func TestRecoverTo_ErrorIdentityUnwrap(t *testing.T) {
	sentinel := stderrors.New("task failed")
	run := func() (err error) {
		defer RecoverTo(&err)()
		panic(sentinel)
	}

	err := run()
	if !Is(err, sentinel) {
		t.Fatalf("errors.Is 应穿透 *PanicError 命中 panic 出去的原始错误, got %v", err)
	}
}

func TestRecoverTo_NoPanic_KeepsBusinessError(t *testing.T) {
	// 无 panic 时闭包零动作：已赋的业务错误不被触碰；无业务错误时 err
	// 保持 nil（闭包不会把 nil 写进来）。
	biz := stderrors.New("biz failure")
	runWithBiz := func() (err error) {
		defer RecoverTo(&err)()
		return biz
	}
	if err := runWithBiz(); !Is(err, biz) {
		t.Fatalf("无 panic 时业务错误应原样返回, got %v", err)
	}

	runClean := func() (err error) {
		defer RecoverTo(&err)()
		return nil
	}
	if err := runClean(); err != nil {
		t.Fatalf("正常返回无错误时 err 应为 nil, got %v", err)
	}
}

func TestRecoverTo_DirectAssignment_Overwrites(t *testing.T) {
	// 直接赋值语义（文档写明）：panic 前已赋业务错误，panic 收编结果覆盖它
	// ——对齐 bootstrap 五处现有骨架的 `err = perr` 直赋口径。
	biz := stderrors.New("biz failure")
	sentinel := stderrors.New("task failed")
	run := func() (err error) {
		defer RecoverTo(&err)()
		err = biz
		panic(sentinel)
	}

	err := run()
	var pe *PanicError
	if !As(err, &pe) {
		t.Fatalf("收编结果应覆盖 *errp 为 *PanicError, got %v", err)
	}
	if !Is(err, sentinel) {
		t.Fatalf("覆盖后的 *PanicError 仍应携带 panic 值的 error 身份, got %v", err)
	}
	if Is(err, biz) {
		t.Fatal("panic 前赋的业务错误应已被覆盖（不再直接在链上）")
	}
}

func TestRecoverTo_NilErrp_FailFast(t *testing.T) {
	// 构造期 fail-fast（仓规总则①）：nil errp 在 RecoverTo 调用时立即 panic，
	// 不等到 panic 收编时才炸。
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("nil errp 应在构造期立即 panic")
			}
		}()
		_ = RecoverTo(nil)
	}()
}

func TestRecoverTo_MissingCall_PanicPropagates(t *testing.T) {
	// 误用护栏：漏掉尾括号（defer RecoverTo(&err) 不调用返回闭包）时
	// recover 不生效，panic 照常向上传播——文档 ⚠️ 的可观察后果。
	run := func() (err error) {
		defer RecoverTo(&err) // ⚠️ 故意漏掉 ()
		panic("boom")
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("漏掉尾括号时 panic 应照常传播（不被收编）")
		}
	}()
	_ = run()
	t.Fatal("panic 应在上一步传播到这里")
}
