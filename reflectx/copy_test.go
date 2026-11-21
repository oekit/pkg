package reflectx

import (
	"reflect"
	"slices"
	"testing"
	"time"
)

type leafInner struct {
	Names []string
}

type leafMixed struct {
	mu     syncMutex // 未导出：整块拷贝后保持原状
	Config leafInner // 导出引用成员：必须被深拷贝替换
	N      int
}

// syncMutex 占位未导出可拷贝字段（不引入 sync 语义，只占形状）。
type syncMutex struct{ state int }

func TestDeepCopyValueScalars(t *testing.T) {
	if got := DeepCopyValue(reflect.ValueOf(42)).Interface(); got != 42 {
		t.Fatalf("int 拷贝失真: got %v", got)
	}
	s := "hello"
	if got := DeepCopyValue(reflect.ValueOf(s)).Interface(); got != s {
		t.Fatalf("string 拷贝失真: got %v", got)
	}
	if got := DeepCopy(3.5); got != 3.5 {
		t.Fatalf("float 拷贝失真: got %v", got)
	}
	now := time.Now()
	if got := DeepCopy(now); !got.Equal(now) {
		t.Fatalf("time.Time 拷贝失真: got %v want %v", got, now)
	}
}

func TestDeepCopyValueReferenceIndependence(t *testing.T) {
	src := map[string][]int{"a": {1, 2}}
	cp := DeepCopyValue(reflect.ValueOf(src)).Interface().(map[string][]int)
	cp["a"][0] = 99
	if src["a"][0] != 1 {
		t.Fatalf("map 值切片仍共享: src=%v", src)
	}

	slice := []*leafInner{{Names: []string{"x"}}}
	cps := DeepCopy(slice)
	cps[0].Names[0] = "y"
	if slice[0].Names[0] != "x" {
		t.Fatalf("指针切片元素仍共享: %v", slice[0].Names)
	}
}

func TestDeepCopyValuePreservesUnexported(t *testing.T) {
	src := leafMixed{mu: syncMutex{state: 7}, Config: leafInner{Names: []string{"a"}}, N: 3}
	cp := DeepCopy(src)
	if cp.mu.state != 7 || cp.N != 3 {
		t.Fatalf("未导出/标量字段失真: %+v", cp)
	}
	// 导出引用成员必须独立（core#84 语义：含未导出字段的 struct 不豁免导出
	// 引用字段的深拷贝——这是相对「整块 memcpy」更强的隔离）。
	cp.Config.Names[0] = "b"
	if src.Config.Names[0] != "a" {
		t.Fatalf("含未导出字段的 struct 导出引用字段仍共享: %v", src.Config.Names)
	}
}

func TestDeepCopyValueCycleSafe(t *testing.T) {
	// 自引用环（core#102）：无环防护会栈溢出崩进程。
	type node struct {
		Val  int
		Next *node
	}
	a := &node{Val: 1}
	b := &node{Val: 2, Next: a}
	a.Next = b // a→b→a 环

	cp := DeepCopy(a)
	if cp == a || cp.Next == nil || cp.Next == b {
		t.Fatal("环拷贝未产生独立副本")
	}
	// 环在副本里闭合且共享关系保留：cp.Next.Next == cp。
	if cp.Next.Next != cp {
		t.Fatal("副本内环未闭合或共享关系丢失")
	}
	if cp.Val != 1 || cp.Next.Val != 2 {
		t.Fatalf("环内值失真: %d %d", cp.Val, cp.Next.Val)
	}
}

func TestDeepCopyValueDAGShared(t *testing.T) {
	// DAG（两指针指向同一块）：拷贝后**内部共享关系保留**（visited 命中同一
	// 副本），且与源脱钩——副本写入不波及 src。
	type holder struct{ N *int }
	shared := 42
	src := struct {
		L, R holder
	}{holder{&shared}, holder{&shared}}
	cp := DeepCopy(src)
	if cp.L.N != cp.R.N {
		t.Fatal("副本内部 DAG 共享丢失（visited 应命中同一已拷贝节点）")
	}
	if cp.L.N == &shared {
		t.Fatal("副本仍别名源对象")
	}
	*cp.L.N = 7
	if *cp.R.N != 7 {
		t.Fatalf("副本内部 DAG 共享丢失: %d != 7", *cp.R.N)
	}
	if shared != 42 {
		t.Fatalf("副本写入波及源: shared=%d", shared)
	}
}

func TestDeepCopyValueNilAndInvalid(t *testing.T) {
	var nilMap map[string]int
	if got := DeepCopy(nilMap); got != nil {
		t.Fatal("nil map 应保持 nil")
	}
	var nilPtr *int
	if got := DeepCopy(nilPtr); got != nil {
		t.Fatal("nil 指针应保持 nil")
	}
	var invalid reflect.Value
	if got := DeepCopyValue(invalid); got.IsValid() {
		t.Fatal("invalid Value 应原样返回")
	}
	// nil 接口经泛型糖仍拿得到类型（nil 接口字段形态）。
	var nilAny any
	if got := DeepCopy(nilAny); got != nil {
		t.Fatal("nil interface 应保持 nil")
	}
}

func TestDeepCopyInterfaceDynamicValue(t *testing.T) {
	src := struct {
		Any any
	}{Any: map[string]int{"k": 1}}
	cp := DeepCopy(src)
	m := cp.Any.(map[string]int)
	m["k"] = 2
	if src.Any.(map[string]int)["k"] != 1 {
		t.Fatal("接口内动态值仍别名")
	}
}

// ---- #426 回归：Map/Slice 自引用环不再 stack overflow ----
//
// 环检测此前只挂 Pointer 分支——m["self"]=m 的 map 自引用、s[0]=s 的
// slice 头自引用直接无限递归，goroutine stack exceeds fatal 打崩进程
// （不是可恢复 error）。修后 Map/Slice 与 Pointer 同口径「先注册再下探」，
// 环上第二次访问返回已拷贝节点，结构共享原样保留。

func TestDeepCopy_MapSelfReference(t *testing.T) {
	m := map[string]any{}
	m["self"] = m
	m["v"] = 42

	out := DeepCopy(m)
	if out["v"] != 42 {
		t.Fatalf("普通键丢失: %v", out)
	}
	inner, ok := out["self"].(map[string]any)
	if !ok {
		t.Fatalf("自引用键形态丢失: %T", out["self"])
	}
	// 副本内部的自引用必须指向**副本自身**（结构共享保留），而不是源 map。
	// map 不可比较——用底层存储指针判身份。
	if reflect.ValueOf(inner["self"]).Pointer() != reflect.ValueOf(out).Pointer() {
		t.Fatal("副本自引用未指向副本自身（环闭合语义破坏）")
	}
	// 副本与源完全独立：改副本不影响源。
	inner["v"] = 99
	if m["v"] != 42 {
		t.Fatal("改副本污染了源")
	}
}

func TestDeepCopy_SliceSelfReference(t *testing.T) {
	s := make([]any, 2)
	s[0] = s
	s[1] = "tail"

	out := DeepCopy(s)
	if out[1] != "tail" {
		t.Fatalf("尾元素丢失: %v", out)
	}
	inner, ok := out[0].([]any)
	if !ok {
		t.Fatalf("自引用元素形态丢失: %T", out[0])
	}
	if len(inner) != 2 || reflect.ValueOf(inner[0]).Pointer() != reflect.ValueOf(out).Pointer() {
		t.Fatal("副本自引用未指向副本自身（环闭合语义破坏）")
	}
}

// TestDeepCopy_SubSliceWindowsNotTruncated 回归（#561）：Slice 的
// reflect.Value.Pointer() 是首元素地址、不区分长度——s[:1] 与 s[:3] 指针
// 相同。visited 只按指针记忆时，结构体同含两个窗口会撞键：后访问者命中
// 先访问者的副本（长度不同），长窗口被静默截断成短窗口。
// 钉住：两个窗口都按各自长度独立拷贝，元素完整，互不别名。
func TestDeepCopy_SubSliceWindowsNotTruncated(t *testing.T) {
	s := make([]int, 3)
	s[0], s[1], s[2] = 1, 2, 3

	// 字段顺序决定访问顺序：短窗口先登记时，旧实现把长窗口截断成 len 1；
	// 长窗口先登记时，旧实现把短窗口错误张开成 len 3（别名同一副本）。
	t.Run("短窗口在前", func(t *testing.T) {
		src := struct {
			A []int // s[:1]
			B []int // s[:3]
		}{s[:1], s[:3]}
		cp := DeepCopy(src)
		if got := len(cp.B); got != 3 {
			t.Fatalf("长窗口被截断：len(cp.B) = %d, want 3", got)
		}
		if !slices.Equal(cp.B, []int{1, 2, 3}) {
			t.Fatalf("长窗口元素失真: %v, want [1 2 3]", cp.B)
		}
		if len(cp.A) != 1 || !slices.Equal(cp.A, []int{1}) {
			t.Fatalf("短窗口失真: %v, want [1]", cp.A)
		}
		// 两个窗口是独立副本：改长窗口不波及短窗口/源（避免错误别名）。
		cp.B[0] = 99
		if cp.A[0] != 1 || src.A[0] != 1 {
			t.Fatalf("窗口间/与源仍别名: cp.A=%v src.A=%v", cp.A, src.A)
		}
	})
	t.Run("长窗口在前", func(t *testing.T) {
		src := struct {
			B []int // s[:3]
			A []int // s[:1]
		}{s[:3], s[:1]}
		cp := DeepCopy(src)
		if !slices.Equal(cp.B, []int{1, 2, 3}) {
			t.Fatalf("长窗口元素失真: %v", cp.B)
		}
		if len(cp.A) != 1 || !slices.Equal(cp.A, []int{1}) {
			t.Fatalf("短窗口被错误张开/别名: len=%d %v, want [1]", len(cp.A), cp.A)
		}
	})
	t.Run("零长窗口", func(t *testing.T) {
		src := struct {
			Z []int // s[:0]
			B []int // s[:3]
		}{s[:0], s[:3]}
		cp := DeepCopy(src)
		if len(cp.Z) != 0 {
			t.Fatalf("零长窗口丢形状: %v", cp.Z)
		}
		if !slices.Equal(cp.B, []int{1, 2, 3}) {
			t.Fatalf("长窗口被零长窗口截断: %v", cp.B)
		}
	})
}

func TestDeepCopy_MutualMapReference(t *testing.T) {
	a := map[string]any{}
	b := map[string]any{}
	a["b"] = b
	b["a"] = a

	outA := DeepCopy(a)
	outB := outA["b"].(map[string]any)
	if reflect.ValueOf(outB["a"]).Pointer() != reflect.ValueOf(outA).Pointer() {
		t.Fatal("互引用环闭合语义破坏（副本内 a/b 应互相指认）")
	}
}
