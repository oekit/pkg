package errors

import (
	"fmt"
	"testing"
)

// TestStdShims 钉住 #8：New/As/Is/Unwrap 与 stdlib 同型透传——下游哨兵错误
// 不再需要双 import。
func TestStdShims(t *testing.T) {
	sentinel := New("deadline reached")
	if !Is(fmt.Errorf("do: %w", sentinel), sentinel) {
		t.Fatal("Is broken")
	}
	if As(sentinel, new(*BizError)) {
		t.Fatal("As should not match unrelated type")
	}

	wrapped := Wrap(sentinel, "with ctx")
	if !Is(wrapped, sentinel) {
		t.Fatal("Is through Wrap broken")
	}
	if Unwrap(wrapped) == nil {
		t.Fatal("Unwrap broken")
	}

	// 与 stdlib errors 互操作：stdlib 构造的错误可被本包 As 展开
	var target *wrappedError
	_ = As(fmt.Errorf("x"), &target) // 编译面验证：target any 语义与 stdlib 一致
}
