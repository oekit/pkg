package container

import "testing"

// TestSet_ZeroValuePanics 钉住 `Set` 的零值口径：零值是 nil map，`Add`
// 直接 panic。
//
// 缺陷成因：同包的其它容器（Map / SafeMap / Slice）都在文档里写明「零值不可
// 直接使用，须经构造函数创建」，唯独 `Set`（本身是 map 类型、零值即 nil map）
// 没写——使用者按同包口径推断「零值可用」⇒ `Add` 撞 "assignment to entry in
// nil map"。
//
// 本用例把该口径钉成**可执行的事实**：文档与行为必须一致。
func TestSet_ZeroValuePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("零值 Set 上 Add 应 panic（nil map）——文档已声明零值不可用")
		}
	}()

	var s Set[int]
	s.Add(1)
}

// TestSet_NewSetUsable 对照：经构造函数创建的 Set 可正常使用。
func TestSet_NewSetUsable(t *testing.T) {
	s := NewSet(1, 2, 2)
	if !s.Contains(1) || !s.Contains(2) {
		t.Fatal("NewSet 创建的 Set 应含已加入的元素")
	}
	if len(s) != 2 {
		t.Fatalf("len = %d，期望 2（去重）", len(s))
	}
}
