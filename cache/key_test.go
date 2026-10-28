package cache

import (
	"testing"
)

func TestKey(t *testing.T) {
	tests := []struct {
		parts []string
		want  string
	}{
		{[]string{"user", "123", "profile"}, "user:123:profile"},
		{[]string{"single"}, "single"},
		{[]string{"a", "b", "c", "d"}, "a:b:c:d"},
		{[]string{}, ""},
	}

	for _, tt := range tests {
		got := Key(tt.parts...)
		if got != tt.want {
			t.Errorf("Key(%v) = %q, want %q", tt.parts, got, tt.want)
		}
	}
}

func TestHashKey(t *testing.T) {
	// 同样的输入应该产生同样的 hash
	k1 := HashKey("search", "keyword", 1, 20)
	k2 := HashKey("search", "keyword", 1, 20)
	if k1 != k2 {
		t.Errorf("HashKey should be deterministic: %q != %q", k1, k2)
	}

	// 不同的输入应该产生不同的 hash
	k3 := HashKey("search", "different", 1, 20)
	if k1 == k3 {
		t.Error("HashKey should produce different results for different inputs")
	}

	// 应该包含前缀
	if len(k1) < 7 || k1[:6] != "search" {
		t.Errorf("HashKey should start with prefix, got %q", k1)
	}

	// 应该包含冒号分隔符
	if k1[6] != ':' {
		t.Errorf("HashKey should have ':' after prefix, got %q", k1)
	}
}

// TestHashKey_NoCollisionBetweenDifferentArgGroups 钉住契约：
// **不同的参数组 ⇒ 不同的 key**。
//
// 旧实现用 "%v:" 直接拼接，参数值里的 ':' 与「参数边界」不可区分 ⇒
// ("a","b") 与 ("a:b")、("a:b","c") 与 ("a","b:c")、1 与 "1" 三组全部同键。
// 后果是静默的：语义不同的两组参数命中同一条缓存 ⇒ A 的结果被当成 B 的返回；
// 且只在参数含 ':' 时触发 ⇒ 测试数据通常不带 ⇒ 很难复现（#146）。
func TestHashKey_NoCollisionBetweenDifferentArgGroups(t *testing.T) {
	cases := []struct {
		name  string
		left  []any
		right []any
	}{
		{"值里的冒号撞上参数边界", []any{"a", "b"}, []any{"a:b"}},
		{"冒号位置不同", []any{"a:b", "c"}, []any{"a", "b:c"}},
		{"int 与 string 互撞", []any{1}, []any{"1"}},
		{"参数个数不同", []any{"a"}, []any{"a", ""}},
		{"多参里的冒号", []any{"x", "a:b", "y"}, []any{"x", "a", "b:y"}},
	}
	for _, c := range cases {
		l := HashKey("p", c.left...)
		r := HashKey("p", c.right...)
		if l == r {
			t.Errorf("%s: HashKey(%v) == HashKey(%v) == %q（不同参数组算出同一个 key ⇒ 缓存串数据）",
				c.name, c.left, c.right, l)
		}
	}
}

// TestHashKey_SameArgsSameKey 护栏：改编码后确定性不能丢。
func TestHashKey_SameArgsSameKey(t *testing.T) {
	a := HashKey("p", "a", 1, true)
	b := HashKey("p", "a", 1, true)
	if a != b {
		t.Errorf("HashKey must be deterministic: %q != %q", a, b)
	}
	// 前缀不同 ⇒ key 不同（不同维度的缓存不能共用同一条目）。
	if HashKey("p1", "a") == HashKey("p2", "a") {
		t.Error("different prefixes must produce different keys")
	}
}

// TestHashKey_NilIsNotStringSentinel 钉住 nil 与字符串 "<nil>" 不撞：
// 「没有这个参数」与「这个参数的值是字符串 "<nil>"」是两组不同参数。
func TestHashKey_NilIsNotStringSentinel(t *testing.T) {
	if HashKey("p", nil) == HashKey("p", "<nil>") {
		t.Error("nil and the string \"<nil>\" must not share a cache key")
	}
}

func TestUserKey(t *testing.T) {
	tests := []struct {
		userID int64
		suffix string
		want   string
	}{
		{123, "profile", "user:123:profile"},
		{1, "settings", "user:1:settings"},
		{0, "test", "user:0:test"},
	}

	for _, tt := range tests {
		got := UserKey(tt.userID, tt.suffix)
		if got != tt.want {
			t.Errorf("UserKey(%d, %q) = %q, want %q", tt.userID, tt.suffix, got, tt.want)
		}
	}
}

func TestTenantKey(t *testing.T) {
	tests := []struct {
		tenantID string
		suffix   string
		want     string
	}{
		{"tenant1", "config", "tenant:tenant1:config"},
		{"abc", "data", "tenant:abc:data"},
	}

	for _, tt := range tests {
		got := TenantKey(tt.tenantID, tt.suffix)
		if got != tt.want {
			t.Errorf("TenantKey(%q, %q) = %q, want %q", tt.tenantID, tt.suffix, got, tt.want)
		}
	}
}
