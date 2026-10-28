package container

import (
	"runtime"
	"testing"
)

// ptrBox 是一个大对象：被切片持有时 GC 收不走。
type ptrBox struct {
	data []byte
}

func newBox() *ptrBox { return &ptrBox{data: make([]byte, 1<<20)} }

// TestSlice_Clear_ReleasesElements 回归测试：Clear 之后底层数组不得**继续持有**
// 原元素。
//
// 缺陷成因：Clear 只做 `s.s = s.s[:0]`（缩 len 不清内容）⇒ 底层数组的旧位置
// 仍持有元素 ⇒ GC 收不走 ⇒ 大对象滞留（切片里放了大 buffer / 闭包 / 连接
// 句柄时是实打实的内存泄漏）。
func TestSlice_Clear_ReleasesElements(t *testing.T) {
	s := NewSlice[any]()
	box := newBox()
	s.Append(box)
	s.Append(newBox())

	s.Clear()

	// 底层数组的旧位置必须已被清零（否则元素仍可达）。
	backing := s.s[:cap(s.s)]
	for i := range backing {
		if backing[i] != nil {
			t.Fatalf("Clear 之后底层数组第 %d 个位置仍持有元素：%T", i, backing[i])
		}
	}
	_ = box
}

// TestSlice_Delete_ReleasesElement 回归测试：Delete 之后被删元素不得留在
// 底层数组的尾位。
//
// 缺陷成因：Delete 用 append 缩容（`s.s[:i]` + `s.s[i+1:]`）⇒ 尾位仍持有
// 被删元素 ⇒ 滞留。
func TestSlice_Delete_ReleasesElement(t *testing.T) {
	s := NewSlice[any]()
	s.Append(1)
	s.Append(newBox())
	s.Append(3)

	if !s.Delete(1) {
		t.Fatal("Delete 应返回 true")
	}
	if len(s.s) != 2 {
		t.Fatalf("len = %d，期望 2", len(s.s))
	}
	// 底层数组**超出切片长度**的那个槽位必须已被清零（被删元素原本在那里）。
	backing := s.s[:cap(s.s)]
	if backing[len(s.s)] != nil {
		t.Fatalf("Delete 之后底层数组的第 %d 槽仍持有被删元素：%T", len(s.s), backing[len(s.s)])
	}
	// 元素顺序不受影响。
	if s.s[0] != 1 || s.s[1] != 3 {
		t.Fatalf("Delete 之后元素顺序不对：%v", s.s)
	}
}

// TestSlice_Delete_OutOfRange 回归护栏：越界删除返回 false、不改内容。
func TestSlice_Delete_OutOfRange(t *testing.T) {
	s := NewSlice[int]()
	s.Append(1)
	if s.Delete(-1) || s.Delete(1) {
		t.Fatal("越界删除应返回 false")
	}
	if len(s.s) != 1 {
		t.Fatalf("len = %d，期望 1", len(s.s))
	}
}

// TestSlice_PushAfterClear 回归护栏：Clear 之后还能继续 Push（清零没把容量
// 弄坏）。
func TestSlice_PushAfterClear(t *testing.T) {
	s := NewSlice[int]()
	s.Append(1)
	s.Clear()
	s.Append(2)
	if len(s.s) != 1 || s.s[0] != 2 {
		t.Fatalf("Clear 之后 Push 失败：%v", s.s)
	}
	_ = runtime.NumGoroutine()
}
