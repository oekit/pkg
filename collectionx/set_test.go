package collectionx

import (
	"slices"
	"testing"
)

func TestSet_AddContainsRemove(t *testing.T) {
	s := NewSet(1, 2, 2) // 去重
	if s.Len() != 2 {
		t.Fatalf("expected len 2 after dedup, got %d", s.Len())
	}
	if !s.Contains(1) || !s.Contains(2) {
		t.Fatal("expected contains 1 and 2")
	}
	s.Add(3)
	s.Remove(1)
	if s.Contains(1) {
		t.Fatal("expected 1 removed")
	}
	if s.Len() != 2 {
		t.Fatalf("expected len 2, got %d", s.Len())
	}
	s.Remove(42) // 不存在为 no-op
}

func TestSet_ItemsRangeClone(t *testing.T) {
	s := NewSet("a", "b", "c")
	items := s.Items()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	for _, want := range []string{"a", "b", "c"} {
		if !slices.Contains(items, want) {
			t.Fatalf("expected %q in items", want)
		}
	}

	var seen int
	s.Range(func(v string) bool {
		seen++
		return false // 提前终止
	})
	if seen != 1 {
		t.Fatalf("expected early stop after 1, got %d", seen)
	}

	c := s.Clone()
	c.Add("d")
	if s.Contains("d") || !c.Contains("d") {
		t.Fatal("clone should be independent")
	}
}

func TestSet_Algebra(t *testing.T) {
	a := NewSet(1, 2, 3)
	b := NewSet(3, 4)

	if got := a.Union(b); got.Len() != 4 {
		t.Fatalf("union len expected 4, got %d", got.Len())
	}
	if got := a.Intersect(b); got.Len() != 1 || !got.Contains(3) {
		t.Fatalf("intersect expected {3}, got %v", got.Items())
	}
	if got := a.Difference(b); got.Len() != 2 || got.Contains(3) {
		t.Fatalf("difference expected {1,2}, got %v", got.Items())
	}
	// 原集合不变
	if a.Len() != 3 || b.Len() != 2 {
		t.Fatal("algebra ops must not mutate operands")
	}
}
