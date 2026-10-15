package strmap

import (
	"fmt"
	"strconv"
	"testing"
)

func TestStrMap_Basic(t *testing.T) {
	m := NewFromMap(map[string]int{"alpha": 1, "beta": 2, "gamma": 3})
	if m.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", m.Len())
	}
	for k, want := range map[string]int{"alpha": 1, "beta": 2, "gamma": 3} {
		if got, ok := m.Get(k); !ok || got != want {
			t.Fatalf("Get(%q) = (%v, %v), want (%d, true)", k, got, ok, want)
		}
	}
	if _, ok := m.Get("missing"); ok {
		t.Fatal("Get(missing) 命中，want miss")
	}
}

// TestStrMap_EmptyGet 空表 Get 不 panic（哈希表未构建时直接 miss）。
func TestStrMap_EmptyGet(t *testing.T) {
	m := New[string]()
	if _, ok := m.Get("anything"); ok {
		t.Fatal("空表 Get 命中，want miss")
	}
	if m.Len() != 0 {
		t.Fatalf("空表 Len = %d, want 0", m.Len())
	}
}

// TestStrMap_LoadErrors LoadFromSlice 的入参校验。
func TestStrMap_LoadErrors(t *testing.T) {
	m := New[int]()
	if err := m.LoadFromSlice([]string{"a", "b"}, []int{1}); err != ErrKvLenMismatch {
		t.Fatalf("len 不一致 err = %v, want ErrKvLenMismatch", err)
	}
	if err := m.LoadFromSlice([]string{"a", "a"}, []int{1, 2}); err != ErrDuplicateKey {
		t.Fatalf("重复键 err = %v, want ErrDuplicateKey", err)
	}
	// 失败的 Load 不应破坏既有内容
	if _, ok := m.Get("a"); ok {
		t.Fatal("Load 失败后残留数据")
	}
}

// TestStrMap_ZeroValue 零值键与空串键的往返。
func TestStrMap_ZeroValue(t *testing.T) {
	m := NewFromMap(map[string]int{"": 0, "x": 42})
	if v, ok := m.Get(""); !ok || v != 0 {
		t.Fatalf("Get(\"\") = (%v, %v), want (0, true)", v, ok)
	}
	if v, ok := m.Get("x"); !ok || v != 42 {
		t.Fatalf("Get(x) = (%v, %v), want (42, true)", v, ok)
	}
}

// TestStrMap_Item 按 Item 遍历应覆盖全部条目且键值对应。
func TestStrMap_Item(t *testing.T) {
	want := map[string]int{"k1": 10, "k2": 20, "k3": 30}
	m := NewFromMap(want)
	seen := map[string]int{}
	for i := 0; i < m.Len(); i++ {
		k, v := m.Item(i)
		seen[k] = v
	}
	for k, v := range want {
		if got, ok := seen[k]; !ok || got != v {
			t.Fatalf("Item 遍历缺 %q: got (%v,%v)", k, got, ok)
		}
	}
}

// TestStrMap_KeyViewStability Item 返回的键视图指向 StrMap 底层内存。
// 重新 Load 复用底层缓冲后，旧视图失效是符合预期的（文档语义），但同一代内
// 视图必须稳定正确——这里构建大表后逐键比对，顺带验证 slot 冲突路径。
func TestStrMap_KeyViewStability(t *testing.T) {
	n := 10000
	kk := make([]string, n)
	vv := make([]int, n)
	for i := range kk {
		kk[i] = fmt.Sprintf("key-%06d-%s", i, "padding-to-spread-hash-buckets")
		vv[i] = i
	}
	m := NewFromSlice(kk, vv)
	for i := 0; i < n; i++ {
		got, ok := m.Get(kk[i])
		if !ok || got != i {
			t.Fatalf("Get(%q) = (%v, %v), want (%d, true)", kk[i], got, ok, i)
		}
	}
	// 全量 Item 重建映射比对
	seen := map[string]int{}
	for i := 0; i < m.Len(); i++ {
		k, v := m.Item(i)
		seen[k] = v
	}
	if len(seen) != n {
		t.Fatalf("Item 遍历条数 = %d, want %d", len(seen), n)
	}
}

// TestStrMap_Reload 重复 Load 复用内部缓冲（cap 不足才重分配）。
func TestStrMap_Reload(t *testing.T) {
	m := NewFromMap(map[string]int{"a": 1})
	if err := m.LoadFromMap(map[string]int{"b": 2, "c": 3}); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := m.Get("a"); ok {
		t.Fatal("reload 后旧键仍命中")
	}
	if v, ok := m.Get("c"); !ok || v != 3 {
		t.Fatalf("Get(c) = (%v, %v), want (3, true)", v, ok)
	}
}

// TestStrMap_Collision 冲突顺位扫描：同槽多键都能命中。
func TestStrMap_Collision(t *testing.T) {
	m := New[string]()
	// 小表 + 多键，冲突概率高；逐键断言全部可达
	kk := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	vv := make([]string, len(kk))
	for i, k := range kk {
		vv[i] = "v-" + k
	}
	if err := m.LoadFromSlice(kk, vv); err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, k := range kk {
		if v, ok := m.Get(k); !ok || v != "v-"+k {
			t.Fatalf("Get(%q) = (%q, %v)", k, v, ok)
		}
	}
}

func TestStrMap_String(t *testing.T) {
	m := NewFromMap(map[string]int{"a": 1})
	s := m.String()
	if len(s) == 0 {
		t.Fatal("String() 为空")
	}
}

func TestStr2Str_Basic(t *testing.T) {
	m := NewStr2StrFromMap(map[string]string{
		"content-type": "Content-Type",
		"user-agent":   "User-Agent",
	})
	if m.Len() != 2 {
		t.Fatalf("Len = %d, want 2", m.Len())
	}
	if v, ok := m.Get("content-type"); !ok || v != "Content-Type" {
		t.Fatalf("Get(content-type) = (%q, %v)", v, ok)
	}
	if _, ok := m.Get("x-missing"); ok {
		t.Fatal("Get(x-missing) 命中，want miss")
	}
}

// TestStr2Str_Empty 未构建时 Get/Len 不 panic。
func TestStr2Str_Empty(t *testing.T) {
	sm := NewStr2Str()
	if _, ok := sm.Get("k"); ok {
		t.Fatal("空表 Get 命中")
	}
	if sm.Len() != 0 {
		t.Fatal("空表 Len 非 0")
	}
}

func TestStr2Str_LoadErrors(t *testing.T) {
	sm := NewStr2Str()
	if err := sm.LoadFromSlice([]string{"a"}, []string{"1", "2"}); err != ErrKvLenMismatch {
		t.Fatalf("len 不一致 err = %v", err)
	}
	if err := sm.LoadFromSlice([]string{"a", "a"}, []string{"1", "2"}); err != ErrDuplicateKey {
		t.Fatalf("重复键 err = %v", err)
	}
}

func TestStr2Str_EmptyValue(t *testing.T) {
	sm := NewStr2StrFromSlice([]string{"a", "b"}, []string{"", "x"})
	if v, ok := sm.Get("a"); !ok || v != "" {
		t.Fatalf("Get(a) = (%q, %v), want (\"\", true)", v, ok)
	}
}

func BenchmarkStrMap_Get(b *testing.B) {
	n := 1024
	kk := make([]string, n)
	vv := make([]int, n)
	std := make(map[string]int, n)
	for i := range kk {
		kk[i] = strconv.Itoa(i)
		vv[i] = i
		std[kk[i]] = i
	}
	sm := NewFromSlice(kk, vv)

	b.Run("strmap", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, ok := sm.Get(kk[i%n]); !ok {
				b.Fatal("miss")
			}
		}
	})
	b.Run("stdmap", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, ok := std[kk[i%n]]; !ok {
				b.Fatal("miss")
			}
		}
	})
}
