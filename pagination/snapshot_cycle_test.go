package pagination

import (
	"testing"
)

// 环防护回归（core#102 修法的 pkg 侧同步，core#130）：页快照的深拷贝此前
// 无环检测——元素类型含自引用/互引用指针（链表节点、父指针树）时，快照
// 递归无限下探，fatal error: stack overflow 崩进程（不是返回错误）。
// 环必须在快照路径上被终止，且环内结构共享关系原样保留。

type cycleNode struct {
	Val  int
	Next *cycleNode
}

type cycleTree struct {
	Name   string
	Parent *cycleTree
	Kids   []*cycleTree
}

func TestPageSnapshot_PointerCycle_NoStackOverflow(t *testing.T) {
	// 自引用环：两节点经 Next 互指。
	a := &cycleNode{Val: 1}
	b := &cycleNode{Val: 2, Next: a}
	a.Next = b
	items := []*cycleNode{a, b}

	got := pageSnapshot(items)
	if len(got) != 2 {
		t.Fatalf("快照长度不符: %d", len(got))
	}
	if got[0] == a || got[1] == b {
		t.Fatal("快照必须与原对象脱钩（不得复用同一指针）")
	}
	// 环在**单元素拷贝内**闭合（visited 按指针身份记忆）：got[0] 的 Next
	// 链走两步必须回到 got[0] 自身——递归在此终止，这正是防栈溢出的机制。
	// （边界：pageSnapshot 逐元素拷贝、每元素一个 visited——与 core#102
	// 修法同口径，环防护保证的是单次 deepCopyValue 调用内环闭合；同页
	// 两个条目互指的跨元素共享按独立树拷贝，本就不在快照语义内。）
	if got[0].Next == nil || got[0].Next.Next != got[0] {
		t.Fatalf("got[0] 拷贝内的环应闭合回自身: got[0].Next=%v", got[0].Next)
	}
	if got[1].Next == nil || got[1].Next.Next != got[1] {
		t.Fatalf("got[1] 拷贝内的环应闭合回自身: got[1].Next=%v", got[1].Next)
	}
	// 值内容一致。
	if got[0].Val != 1 || got[1].Val != 2 {
		t.Fatalf("值内容不符: %d %d", got[0].Val, got[1].Val)
	}
}

func TestPageSnapshot_ParentPointerCycle_NoStackOverflow(t *testing.T) {
	// 父指针环：Kids 与 Parent 互指（真实形态：树节点带父指针）。
	root := &cycleTree{Name: "root"}
	kid := &cycleTree{Name: "kid", Parent: root}
	root.Kids = []*cycleTree{kid}

	got := pageSnapshot([]*cycleTree{root, kid})
	if len(got) != 2 {
		t.Fatalf("快照长度不符: %d", len(got))
	}
	if got[0] == root || got[1] == kid {
		t.Fatal("快照必须与原对象脱钩")
	}
	if got[0].Name != "root" || got[1].Name != "kid" {
		t.Fatalf("值内容不符: %q %q", got[0].Name, got[1].Name)
	}
	// kid 拷贝内的环闭合：Parent 链下探经 Kids 回到 kid 自身——递归终止。
	if got[1].Parent == nil || len(got[1].Parent.Kids) != 1 || got[1].Parent.Kids[0] != got[1] {
		t.Fatalf("kid 拷贝内的环应闭合回自身, got Parent=%v", got[1].Parent)
	}
	if got[1].Parent.Name != "root" {
		t.Fatalf("Parent 拷贝内容不符: %q", got[1].Parent.Name)
	}
	// 改写快照不污染原件。
	got[1].Name = "mutated"
	if kid.Name != "kid" {
		t.Fatal("改写快照污染了原件")
	}
}
