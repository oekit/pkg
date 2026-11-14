package container

import "testing"

// #474：popLocked 压缩分支搬移存活元素后，旧尾部槽位仍持有已出队元素的
// 引用——出队 ≠ 释放，大对象滞留到下次覆写。压缩即补尾清零。
func TestQueuePopCompressClearsTailRefs(t *testing.T) {
	q := NewQueue[*int]()
	for range 8 {
		p := new(int)
		q.Put(p)
	}
	for range 8 {
		q.Take()
	}
	if q.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", q.Len())
	}
	// 队列已空且触发过压缩（head 过半）与整轮重置：检查底层完整数组——
	// 任何存活槽位都不得再持有已出队元素的引用。
	for i, slot := range q.items[:cap(q.items)] {
		if slot != nil {
			t.Fatalf("backing[%d] still holds a dequeued pointer (compress branch leaked refs)", i)
		}
	}
}

// #475：Ring.Take 取出槽位不清零——同包 Queue/Slice 的「释放引用」纪律
// 在 Ring 缺席。取出即清零。
func TestRingTakeClearsSlot(t *testing.T) {
	r := NewRing[*int](4)
	for range 4 {
		r.Add(new(int))
	}
	for range 4 {
		r.Take()
	}
	for i, slot := range r.elements {
		if slot != nil {
			t.Fatalf("r.elements[%d] still holds a taken pointer", i)
		}
	}
	// 清零不得破坏 FIFO 续写：再放两个、取两个，顺序保序。
	a, b := new(int), new(int)
	*a, *b = 1, 2
	r.Add(a)
	r.Add(b)
	v1, _ := r.Take()
	v2, _ := r.Take()
	if v1 != a || v2 != b {
		t.Fatalf("FIFO broken after slot clearing: got %v %v, want %v %v", *v1, *v2, *a, *b)
	}
}

// #476：Map/Slice 零值可用（此前文档宣称「零值不可直接使用」，与构造器
// 返回零值自相矛盾）。
func TestMapSliceZeroValueUsable(t *testing.T) {
	var m Map[string, int]
	m.Store("a", 1)
	if v, ok := m.Load("a"); !ok || v != 1 {
		t.Fatalf("zero-value Map Load = %v %v", v, ok)
	}
	var s Slice[int]
	s.Append(1)
	if got, _ := s.Get(0); got != 1 {
		t.Fatalf("zero-value Slice Get = %v", got)
	}
}
