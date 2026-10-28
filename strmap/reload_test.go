package strmap

import "testing"

// TestStrMap_ReloadDoesNotMutateReturnedKey 回归测试：重载不得改写**已经
// 返回**的键字符串。
//
// 缺陷成因：LoadFromSlice 此前是 `m.data = m.data[:0]` 后就地重写，而 key()
// 返回的是 data 上的 unsafe.String 零拷贝视图 ⇒ 重载把旧缓冲的字节就地
// 改写，之前返回的字符串内容随之改变（破坏 Go 字符串不可变保证）。
//
// 实测（修前）：Get("k") 拿到 "k" 后重载同一张表，旧字符串的内容被改写。
func TestStrMap_ReloadDoesNotMutateReturnedKey(t *testing.T) {
	m := New[string]()
	if err := m.LoadFromSlice([]string{"k", "kk"}, []string{"v1", "v2"}); err != nil {
		t.Fatal(err)
	}

	before, ok := m.Get("k")
	if !ok {
		t.Fatal("Get 未命中")
	}
	// Item(i) 按**内部存储序**返回（哈希槽序，maphash 种子逐进程随机），
	// 不是字典序 ⇒ 不能假定 Item(0) 是哪个键，只能断言「重载前后不变」。
	keyBefore, _ := m.Item(0)

	if err := m.LoadFromSlice([]string{"k", "kk"}, []string{"w1", "w2"}); err != nil {
		t.Fatal(err)
	}

	if before != "v1" {
		t.Fatalf("重载改写了已返回的值：%q（期望 v1）", before)
	}
	keyAfter, _ := m.Item(0)
	if keyAfter != keyBefore {
		t.Fatalf("重载改写了已返回的键：%q → %q", keyBefore, keyAfter)
	}

	// 重载后的表内容应当是新值。
	after, ok := m.Get("k")
	if !ok || after != "w1" {
		t.Fatalf("重载后 Get 未返回新值：%q ok=%v", after, ok)
	}
}

// TestStrMap_ReloadDoesNotMutateItemKey Item 返回的键视图同样不得被改写。
func TestStrMap_ReloadDoesNotMutateItemKey(t *testing.T) {
	m := New[string]()
	if err := m.LoadFromSlice([]string{"alpha"}, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	key, _ := m.Item(0)

	if err := m.LoadFromSlice([]string{"bravo"}, []string{"b"}); err != nil {
		t.Fatal(err)
	}
	if key != "alpha" {
		t.Fatalf("重载改写了 Item 返回的键：%q（期望 alpha）", key)
	}
}

// TestStr2Str_ReloadDoesNotMutateReturnedValue 回归测试：Str2Str.Get 返回的
// **值**同样是零拷贝视图（打包进 strStore 缓冲），重载不得改写它。
//
// 缺陷成因：strStore.Load 此前是 `s.buf = s.buf[:total]` 后就地重写。
func TestStr2Str_ReloadDoesNotMutateReturnedValue(t *testing.T) {
	sm := NewStr2StrFromSlice([]string{"k"}, []string{"v1"})
	before, ok := sm.Get("k")
	if !ok {
		t.Fatal("Get 未命中")
	}

	if err := sm.LoadFromSlice([]string{"k"}, []string{"w1"}); err != nil {
		t.Fatal(err)
	}

	if before != "v1" {
		t.Fatalf("重载改写了已返回的值：%q（期望 v1）", before)
	}
	after, _ := sm.Get("k")
	if after != "w1" {
		t.Fatalf("重载后 Get 未返回新值：%q（期望 w1）", after)
	}
}

// TestStr2Str_ReloadDoesNotMutateReturnedKey Str2Str 的键走 StrMap，同样不得
// 被改写。
func TestStr2Str_ReloadDoesNotMutateReturnedKey(t *testing.T) {
	sm := NewStr2StrFromSlice([]string{"k"}, []string{"v"})
	before, ok := sm.Get("k")
	if !ok {
		t.Fatal("Get 未命中")
	}
	if err := sm.LoadFromSlice([]string{"kk"}, []string{"w"}); err != nil {
		t.Fatal(err)
	}
	if before != "v" {
		t.Fatalf("重载改写了已返回的值：%q（期望 v）", before)
	}
}
