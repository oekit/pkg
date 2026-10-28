package slicex

import "testing"

// TestChunk_SharesBackingArray 钉住 `Chunk` 的别名语义：分片与入参共享底层
// 数组，改写分片元素会改写原切片。
//
// 同包的 `Reverse` 明确「返回新切片，不修改原始切片」——`Chunk` 的口径与之
// 不同（与 stdlib `slices.Chunk` 一致），此前文档没说清，使用方容易按同包
// 口径推断「拿到的是独立切片」⇒ 改写分片时意外改掉了原切片。
func TestChunk_SharesBackingArray(t *testing.T) {
	src := []int{1, 2, 3, 4}
	chunks := Chunk(src, 2)
	if len(chunks) != 2 {
		t.Fatalf("分片数 = %d，期望 2", len(chunks))
	}
	chunks[0][0] = 99
	if src[0] != 99 {
		t.Fatal("Chunk 的分片本应与入参共享底层数组（stdlib slices.Chunk 同语义）")
	}
}

// TestChunk_NonPositiveSize 回归护栏：size <= 0 返回 nil（不 panic、不死循环）。
func TestChunk_NonPositiveSize(t *testing.T) {
	for _, size := range []int{0, -1} {
		if got := Chunk([]int{1, 2, 3}, size); got != nil {
			t.Errorf("Chunk(size=%d) = %v，期望 nil", size, got)
		}
	}
}

// TestReverse_DoesNotMutateInput 回归护栏：Reverse 返回新切片（与 Chunk 的
// 别名语义相反，文档已分别写明）。
func TestReverse_DoesNotMutateInput(t *testing.T) {
	src := []int{1, 2, 3}
	got := Reverse(src)
	got[0] = 99
	if src[0] != 1 {
		t.Fatal("Reverse 应返回新切片，不修改原始切片")
	}
}
