package errors

import (
	stderrors "errors"
	"testing"
	"time"
)

var errBase = stderrors.New("base error")

func TestWrap_NilError(t *testing.T) {
	if err := Wrap(nil, "ignored"); err != nil {
		t.Errorf("Wrap(nil, ...) should return nil, got %v", err)
	}
}

func TestWrap_BasicMessage(t *testing.T) {
	err := Wrap(errBase, "operation X")
	want := "operation X: base error"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestWrap_WithKV(t *testing.T) {
	err := Wrap(errBase, "query user", "user_id", 42, "table", "users")
	got := err.Error()
	if got != "query user: base error user_id=42 table=users" {
		t.Errorf("got %q", got)
	}
}

func TestWrap_HandlesOddKV(t *testing.T) {
	// 奇数长度：保留末参并标 !BADKEY，不静默丢弃（对齐 log/slog）
	err := Wrap(errBase, "op", "k1", "v1", "extra")
	if err.Error() != "op: base error k1=v1 !BADKEY=extra" {
		t.Errorf("got %q", err.Error())
	}
}

func TestWrap_OddKVSoleKey(t *testing.T) {
	// 典型漏传 value：Wrap(err, "read config", "path")
	err := Wrap(errBase, "read config", "path")
	if err.Error() != "read config: base error !BADKEY=path" {
		t.Errorf("got %q", err.Error())
	}
}

func TestWrap_Unwrap(t *testing.T) {
	err := Wrap(errBase, "operation X")
	if !stderrors.Is(err, errBase) {
		t.Error("errors.Is should match wrapped err to errBase")
	}
}

func TestWrap_NestedUnwrap(t *testing.T) {
	inner := Wrap(errBase, "inner")
	outer := Wrap(inner, "outer")
	if !stderrors.Is(outer, errBase) {
		t.Error("nested wrap should still match errBase via errors.Is")
	}
}

func TestCause_Layers(t *testing.T) {
	inner := Wrap(errBase, "inner")
	outer := Wrap(inner, "outer")
	cause := Cause(outer)
	if cause != errBase {
		t.Errorf("Cause = %v, want errBase", cause)
	}
}

func TestCause_NotWrapped(t *testing.T) {
	if Cause(errBase) != errBase {
		t.Error("Cause on plain error should return itself")
	}
}

func TestCause_Nil(t *testing.T) {
	if Cause(nil) != nil {
		t.Error("Cause(nil) should return nil")
	}
}

func TestFormatKV(t *testing.T) {
	tests := []struct {
		kv  []any
		exp string
	}{
		{[]any{"a", "1", "b", "2"}, "a=1 b=2"},
		{[]any{}, ""},
		{[]any{"x"}, "!BADKEY=x"}, // 奇数：保留末参
		{[]any{"k1", "v1", "extra"}, "k1=v1 !BADKEY=extra"},
		{nil, ""},
	}
	for _, tt := range tests {
		got := formatKV(tt.kv)
		if got != tt.exp {
			t.Errorf("formatKV(%v) = %q, want %q", tt.kv, got, tt.exp)
		}
	}
}

// 成环 Unwrap 链：Cause 按深度截断返回，不死循环（#298；对齐 Error() 层
// TestBizError_FormatCycleDefense 的防御先例）。
func TestCause_CycleDefense(t *testing.T) {
	a, b := newCyclePair()
	got := Cause(a)
	if got == nil {
		t.Fatal("Cause(成环链) 不得返回 nil")
	}
	// 带超时的死循环哨兵：修复回归时这里会先超时
	done := make(chan struct{})
	go func() { _ = Cause(a); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Cause 在成环链上死循环（#298 回归）")
	}
	_ = b
}

// cycleErr 是最小成环对：A.Unwrap()=B、B.Unwrap()=A。
type cycleErr struct{ next *cycleErr }

func (e *cycleErr) Error() string { return "cycle" }
func (e *cycleErr) Unwrap() error { return e.next }

func newCyclePair() (error, error) {
	a := &cycleErr{}
	b := &cycleErr{next: a}
	a.next = b
	return a, b
}
