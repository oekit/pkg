package cache

import (
	"testing"
)

// TestHashKey_NoCollision 回归测试：不同参数组**不得**算出同一个 key。
//
// 缺陷成因（旧实现）：摘要输入用 `fmt.Fprintf(h, "%v:", arg)` 逐参数拼接，
// 参数值里的 `:` 与「参数边界」不可区分 ⇒ 不同参数组产出完全相同的输入。
//
//	HashKey("p", "a", "b") == HashKey("p", "a:b")   // 旧写法：同键
//	HashKey("p", 1)        == HashKey("p", "1")    // 旧写法：同键
//
// ⇒ 语义不同的查询命中同一条缓存 ⇒ 串数据（跨租户/跨用户时即越权）。
// 旧测试只比对「不同简单输入产出不同 hash」，**无任何碰撞用例** ⇒ 漏检。
func TestHashKey_NoCollision(t *testing.T) {
	cases := []struct {
		name string
		a    []any
		b    []any
	}{
		{"值里的冒号 vs 参数边界", []any{"a", "b"}, []any{"a:b"}},
		{"值里的冒号（另一侧）", []any{"a:b", "c"}, []any{"a", "b:c"}},
		{"int 与 string 同形", []any{1}, []any{"1"}},
		{"拼接型碰撞", []any{"a", 1}, []any{"a1"}},
		{"多参数整体碰撞", []any{"x", "y:z"}, []any{"x:y", "z"}},
		{"空串与分隔符", []any{"a", ""}, []any{"a:"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k1 := HashKey("p", tc.a...)
			k2 := HashKey("p", tc.b...)
			if k1 == k2 {
				t.Fatalf("键碰撞：HashKey(%v) == HashKey(%v) == %q", tc.a, tc.b, k1)
			}
		})
	}
}

// TestHashKey_SameArgsSameKey 回归护栏：相同参数组必须稳定产出同一个 key
// （新旧写法都应满足）。
func TestHashKey_SameArgsSameKey(t *testing.T) {
	k1 := HashKey("p", "a", 1, 20)
	k2 := HashKey("p", "a", 1, 20)
	if k1 != k2 {
		t.Fatalf("同参数组产出不同 key：%q != %q", k1, k2)
	}
}

// TestHashKey_DifferentPrefix 回归护栏：不同前缀产出不同 key。
func TestHashKey_DifferentPrefix(t *testing.T) {
	if HashKey("a", "x") == HashKey("b", "x") {
		t.Fatal("不同前缀产出了相同 key")
	}
}

// TestHashKey_ArgumentOrderMatters 回归护栏：参数顺序不同 ⇒ key 不同。
func TestHashKey_ArgumentOrderMatters(t *testing.T) {
	if HashKey("p", "a", "b") == HashKey("p", "b", "a") {
		t.Fatal("参数顺序不同却产出相同 key")
	}
}
