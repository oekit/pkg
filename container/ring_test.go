package container

import "testing"

func TestRing_Fifo(t *testing.T) {
	r := NewRing[int](3)
	if r.Len() != 0 {
		t.Fatalf("expected empty, got %d", r.Len())
	}
	r.Add(1)
	r.Add(2)
	r.Add(3)
	if r.Len() != 3 {
		t.Fatalf("expected len 3, got %d", r.Len())
	}
	for want := 1; want <= 3; want++ {
		v, ok := r.Take()
		if !ok || v != want {
			t.Fatalf("expected %d, got %d (ok=%v)", want, v, ok)
		}
	}
	if _, ok := r.Take(); ok {
		t.Fatal("expected empty after drain")
	}
}

func TestRing_OverwriteOldest(t *testing.T) {
	r := NewRing[int](2)
	r.Add(1)
	r.Add(2)
	r.Add(3) // 覆盖 1
	if r.Len() != 2 {
		t.Fatalf("expected len capped at 2, got %d", r.Len())
	}
	// 消费侧取到现存最旧（错位后为 2），随后 3
	if v, ok := r.Take(); !ok || v != 2 {
		t.Fatalf("expected 2 first, got %d (ok=%v)", v, ok)
	}
	if v, ok := r.Take(); !ok || v != 3 {
		t.Fatalf("expected 3 second, got %d (ok=%v)", v, ok)
	}
}

func TestRing_WrapAround(t *testing.T) {
	r := NewRing[string](2)
	for i := 0; i < 10; i++ {
		r.Add("v")
		if r.Len() > 2 {
			t.Fatalf("len should never exceed capacity, got %d", r.Len())
		}
		for {
			if _, ok := r.Take(); !ok {
				break
			}
		}
	}
}

func TestRing_InvalidSize(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on zero size")
		}
	}()
	NewRing[int](0)
}
