package enumx

import "testing"

type testEnum struct {
	value int
	label string
}

func (e testEnum) Value() int    { return e.value }
func (e testEnum) Label() string { return e.label }
func (e testEnum) IsValid() bool { return e.value >= 0 }

// TestNewRegistry_DuplicateValue_Panics 回归测试：重复的 Value() 必须 panic。
//
// 缺陷成因：NewRegistry 用 map 存枚举，重复 Value 时**后者静默覆盖**前者
// （连 labels 一起覆盖）⇒ 调用方拿到哪个枚举、看到哪个标签全看传入顺序。
// 枚举定义写错是编程错误，构造期报出来。
func TestNewRegistry_DuplicateValue_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("重复的枚举值应 panic")
		}
	}()
	_ = NewRegistry(
		testEnum{value: 1, label: "a"},
		testEnum{value: 1, label: "b"},
	)
}

// TestNewRegistry_Normal 回归护栏：正常注册可取到枚举与标签。
func TestNewRegistry_Normal(t *testing.T) {
	r := NewRegistry(
		testEnum{value: 1, label: "one"},
		testEnum{value: 2, label: "two"},
	)
	e, ok := r.Get(2)
	if !ok || e.Label() != "two" {
		t.Fatalf("Get(2) = (%v, %v)", e, ok)
	}
	if got := r.Label(1); got != "one" {
		t.Fatalf("Label(1) = %q，期望 one", got)
	}
	// 未登记的值走 unknown 回退（文档已写明）。
	if got := r.Label(99); got != "unknown(99)" {
		t.Fatalf("Label(99) = %q，期望 unknown(99)", got)
	}
}
