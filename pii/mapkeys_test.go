package pii

// #294 判据先于实现：map 的键与值走**同一条判定面**。
//
// 缺陷（修复前）：applyValue 的两个 map 分支只处置 value，键原样复制——
// 以手机号为键的载荷（中文业务系统的一等形态：手机号 → 订单/客户资料）整键
// 明文外泄，且 blocked=false，宿主收不到任何「没处置」的信号。这与 pii.go
// 「值本身即敏感，与键名无关」的声明正好相反：实现恰好漏在键上。
//
// 验收标准逐条：
//
//	键为手机号/身份证的 map[string]string 与 map[string]any：处置后键不再逐字可见；
//	普通键（order_id 等）逐字不变；
//	同一载荷重复处置，键形态恒同（可关联性，且独立于 map 迭代顺序）；
//	键命中与值命中同样产出 blocked 信号。

import (
	"reflect"
	"testing"
)

// TestMapKeysDisposedSameAsValues 键为手机号/身份证时，处置后键位不得残留原文；
// 普通键逐字不变（防「一律打码键」的误检）。
//
// 反向验证：摘掉键处置分支 ⇒ 键位原文残留，本用例当场转红。
func TestMapKeysDisposedSameAsValues(t *testing.T) {
	restore(t)

	in := map[string]string{
		"13812345678":         "order A",
		"11010519491231002X":  "order B",
		"order_id":            "ORD-1",
		"ts=1728643200000_ms": "plain",
	}
	out, blocked := ApplyValue(SurfaceToolResults, in)
	if blocked {
		t.Fatal("mask 策略下键命中不应要求阻断")
	}
	want := map[string]string{
		"138****5678":         "order A",
		"1101**************":  "order B",
		"order_id":            "ORD-1",
		"ts=1728643200000_ms": "plain",
	}
	got, ok := out.(map[string]string)
	if !ok {
		t.Fatalf("应保持 map[string]string 结构，实得 %T", out)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("键位未走与值同一条判定面：\n got  %v\n want %v", got, want)
	}

	// map[string]any 的键同样收口。
	out2, _ := ApplyValue(SurfaceToolResults, map[string]any{"13900001111": 42})
	m2, ok := out2.(map[string]any)
	if !ok {
		t.Fatalf("应保持 map[string]any 结构，实得 %T", out2)
	}
	if _, leaked := m2["13900001111"]; leaked {
		t.Fatalf("map[string]any 键位手机号明文残留: %v", m2)
	}
	if m2["139****1111"] != 42 {
		t.Fatalf("处置后的键应指向原值: %v", m2)
	}
}

// TestMapKeyBlockSignal 键命中必须与值命中一样产出 blocked 信号——
// 否则 block 档下宿主对「键位明文」依旧毫无感知（缺陷形态的第二重危害）。
func TestMapKeyBlockSignal(t *testing.T) {
	restore(t)
	if err := setStrategyForTest(t, StrategyBlock); err != nil {
		t.Fatal(err)
	}
	in := map[string]any{"13812345678": "profile", "order_id": "ORD-1"}
	out, blocked := ApplyValue(SurfaceToolResults, in)
	if !blocked {
		t.Fatal("block 策略下键命中必须上报阻断")
	}
	m := out.(map[string]any)
	if _, leaked := m["13812345678"]; leaked {
		t.Fatalf("block 档键位仍明文残留: %v", m)
	}
	if m["order_id"] != "ORD-1" {
		t.Fatalf("普通键不应被动: %v", m["order_id"])
	}
}

// TestMapKeyCorrelatableAcrossCalls 同一键名在两次处置后形态必须相同——
// map 迭代无序，可关联性不得依赖迭代顺序：同一载荷连跑 50 次，结果必须逐字一致。
func TestMapKeyCorrelatableAcrossCalls(t *testing.T) {
	restore(t)
	if err := setDigestKeyForTest(t, testDigestKey); err != nil {
		t.Fatal(err)
	}

	in := map[string]any{
		"13812345678": "a",
		"13900001111": "b",
		"order_id":    "ORD-1",
		"nested":      map[string]string{"11010519491231002X": "c", "note": "d"},
		"list":        []string{"13712345678", "plain"},
	}

	cases := []struct {
		name string
		s    StrategyKind
	}{
		{"mask", StrategyMask},
		{"redact", StrategyRedact},
		{"hash", StrategyHash},
		{"block", StrategyBlock},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			restore(t)
			if err := setDigestKeyForTest(t, testDigestKey); err != nil {
				t.Fatal(err)
			}
			if err := setStrategyForTest(t, c.s); err != nil {
				t.Fatal(err)
			}
			first, _ := ApplyValue(SurfaceToolResults, in)
			for i := 0; i < 50; i++ {
				again, _ := ApplyValue(SurfaceToolResults, in)
				if !reflect.DeepEqual(first, again) {
					t.Fatalf("第 %d 次处置结果与第一次不同——键形态依赖了 map 迭代顺序\n first %v\n again %v", i, first, again)
				}
			}
		})
	}
}

// TestMapKeyCollisionKeepsEntries 冲突消解护栏：不同原键处置出同一形态时
// （redact 下必然：所有 PII 键都变占位；mask 下中段不同的同前缀同尾号也可能），
// **不得丢条目**，且编号只依赖键集合（按原键字典序），与迭代顺序无关。
func TestMapKeyCollisionKeepsEntries(t *testing.T) {
	restore(t)
	if err := setStrategyForTest(t, StrategyRedact); err != nil {
		t.Fatal(err)
	}

	out, blocked := ApplyValue(SurfaceToolResults, map[string]string{
		"13812345678": "a",
		"13900001111": "b",
	})
	if blocked {
		t.Fatal("redact 不应阻断")
	}
	got, ok := out.(map[string]string)
	if !ok {
		t.Fatalf("结构应保持，实得 %T", out)
	}
	if len(got) != 2 {
		t.Fatalf("两个 PII 键同形不得合并丢条目，实得 %d 条: %v", len(got), got)
	}
	want := map[string]string{"[redacted]#1": "a", "[redacted]#2": "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("同形键应按原键字典序稳定编号:\n got  %v\n want %v", got, want)
	}

	// mask 的中段碰撞同规则。
	if err := setStrategyForTest(t, StrategyMask); err != nil {
		t.Fatal(err)
	}
	out2, _ := ApplyValue(SurfaceToolResults, map[string]string{
		"13800001234": "a", // → 138****1234
		"13899991234": "b", // → 138****1234（中段被掩，同形）
	})
	got2 := out2.(map[string]string)
	want2 := map[string]string{"138****1234#1": "a", "138****1234#2": "b"}
	if !reflect.DeepEqual(got2, want2) {
		t.Fatalf("mask 同形键应稳定编号不丢条目:\n got  %v\n want %v", got2, want2)
	}
}

// TestMapKeysUnderUnknownSurfaceKeepNavigability 未登记面名（fail-closed 档）
// 下：值整段替换的既有契约不动（结构可导航，宿主能定位哪些字段出的问题），
// 但 PII 键不得借道原文放行——span 级判定照走；普通键逐字保留。
func TestMapKeysUnderUnknownSurfaceKeepNavigability(t *testing.T) {
	restore(t)
	setEnabledForTest(t, true)
	if err := setStrategyForTest(t, StrategyRedact); err != nil {
		t.Fatal(err)
	}
	setSurfacesForTest(t, SurfaceAll)

	const secret = "联系 13800138000"
	bad := Surface("tool_result")
	out, blocked := ApplyValue(bad, map[string]any{"tool_output": secret, "13812345678": "v"})
	if !blocked {
		t.Fatal("未登记面名应回报阻断")
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("结构应保持，实得 %T", out)
	}
	// 既有契约：普通键保留，宿主仍能按原结构定位被替换的值。
	if got, exist := m["tool_output"]; !exist || got != redactedPlaceholder {
		t.Fatalf("普通键在 fail-closed 档应逐字保留，实得 %v（存在 %v）", got, exist)
	}
	// PII 键不得原文放行。
	if _, leaked := m["13812345678"]; leaked {
		t.Fatalf("未登记面名下 PII 键明文残留: %v", m)
	}
	// 值走整段替换（fail-closed 对整个字符串生效，即使 "v" 本身无敏感）；
	// 键走 span 级处置：PII 键变占位并保留指向。
	if got, exist := m["[redacted]"]; !exist || got != redactedPlaceholder {
		t.Fatalf("PII 键应按当前策略处置并保留指向: %v（存在 %v）", got, exist)
	}
}
