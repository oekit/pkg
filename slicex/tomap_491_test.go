package slicex

import "testing"

// #491：ToMap 重复 key 的语义此前未声明（实为 last-wins 静默覆盖）；补
// ToMapStrict 与 strmap.ErrDuplicateKey 的 fail-loud 口径对齐。
func TestToMapDuplicateKeySemantics(t *testing.T) {
	got := ToMap([]int{1, 2, 3}, func(int) string { return "k" })
	if len(got) != 1 || got["k"] != 3 {
		t.Fatalf("ToMap last-wins = %v, want map[k:3]", got)
	}

	m, err := ToMapStrict([]int{1, 2}, func(int) string { return "dup" })
	if err == nil || m != nil {
		t.Fatalf("ToMapStrict 重复 key 应报错: %v %v", m, err)
	}

	m2, err := ToMapStrict([]int{1, 2, 3}, func(v int) string {
		return []string{"a", "b", "c"}[v-1]
	})
	if err != nil || len(m2) != 3 || m2["b"] != 2 {
		t.Fatalf("ToMapStrict 无重复应通过: %v %v", m2, err)
	}
}
