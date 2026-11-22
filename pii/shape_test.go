package pii

// #295 判据先于实现：未登记的载荷形状不得静默透传。
//
// 缺陷（修复前）：applyValue 的 default 分支对一切未登记形状原样返回且
// blocked=false——宿主把工具返回解码成 []map[string]string / map[string][]string /
// []byte（含 json.RawMessage）时整段 PII 外发，连 block 档也拿不到阻断信号，
// 宿主无法区分「没命中敏感值」与「根本没看」。同一个文件对未登记**面名**是
// fail-closed（「无信号才是这里真正危险的地方」），对未登记**形状**却走了它
// 自己批判的静默放行路径。
//
// 验收标准逐条：
//
//	[]map[string]string / map[string][]string / []byte 三种载荷 fail-closed
//	（整段替换 + blocked，与未登记面名同档）；
//	已支持的四种容器 + 标量行为保持不变（回归护栏）；
//	接线期自查入口 ValidateShape（类似 ValidateSurface 的载荷形状契约面）；
//	反向验证：default 分支改回原样透传 ⇒ 第一条用例转红。

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/oekit/pkg/errors"
)

// unknownShapePayload issue 实证的载荷：四处里只有 "ok"（登记形状）被处置，
// 其余三处在缺陷实现下原文保留且 blocked=false。
func unknownShapePayload() map[string]any {
	return map[string]any{
		"list": []map[string]string{{"tel": synMobile}},
		"blob": []byte("tel " + synMobile),
		"mp":   map[string][]string{"tel": {synMobile}},
		"ok":   synMobile,
	}
}

// TestUnknownShapesFailClosed 未登记形状与未登记面名同档 fail-closed：
// 整段替换 + 要求阻断。标量与 nil 不在内（文档化的覆盖盲区，见回归护栏用例）。
//
// 反向验证：把 default 分支改回原样透传 ⇒ blocked=false、原文保留，本用例转红。
func TestUnknownShapesFailClosed(t *testing.T) {
	restore(t)

	// mask 档（缺省）就要 fail-closed——这不是 block 档专属。
	out, blocked := ApplyValue(SurfaceToolResults, unknownShapePayload())
	if !blocked {
		t.Fatal("未登记载荷形状必须回报阻断信号——宿主无权把「根本没看」误读成「没命中」")
	}
	m := out.(map[string]any)
	for _, k := range []string{"list", "blob", "mp"} {
		if m[k] != redactedPlaceholder {
			t.Fatalf("未登记形状 %q 应整段替换，实得 %#v", k, m[k])
		}
	}
	if m["ok"] != "138****1234" {
		t.Fatalf("登记形状的处置不得受影响: %v", m["ok"])
	}
	if _, leaked := m["list"].([]map[string]string); leaked {
		t.Fatal("未登记形状不得以原类型透传")
	}

	// block 档同样阻断——实证里「换成 StrategyBlock 后仍 blocked=false」正是缺陷。
	if err := setStrategyForTest(t, StrategyBlock); err != nil {
		t.Fatal(err)
	}
	if _, blocked := ApplyValue(SurfaceToolResults, unknownShapePayload()); !blocked {
		t.Fatal("block 档下未登记形状同样必须回报阻断")
	}

	// 三种实证形状顶层直传同样 fail-closed。
	top := []any{
		[]map[string]string{{"tel": synMobile}},
		map[string][]string{"tel": {synMobile}},
		[]byte(synMobile),
	}
	for i, v := range top {
		got, b := ApplyValue(SurfaceToolResults, v)
		if !b {
			t.Fatalf("第 %d 种未登记形状顶层直传必须回报阻断", i)
		}
		if got != redactedPlaceholder {
			t.Fatalf("第 %d 种未登记形状顶层直传应整段替换，实得 %#v", i, got)
		}
	}
}

// TestSupportedShapesUnchanged 回归护栏：已支持的四种容器 + 标量 + nil 的行为
// 不得因收紧 unknown 而漂移（防止支持面扩大成误检面，或把标量误杀）。
func TestSupportedShapesUnchanged(t *testing.T) {
	restore(t)

	// 四种登记容器照常处置。
	out, blocked := ApplyValue(SurfaceToolResults, map[string]any{
		"m1": map[string]any{"tel": synMobile},
		"m2": map[string]string{"tel": synMobile},
		"s1": []any{synMobile, 42, true},
		"s2": []string{synMobile},
	})
	if blocked {
		t.Fatal("登记形状不应要求阻断")
	}
	m := out.(map[string]any)
	if m["m1"].(map[string]any)["tel"] != "138****1234" {
		t.Fatalf("map[string]any 处置漂移: %v", m["m1"])
	}
	if m["m2"].(map[string]string)["tel"] != "138****1234" {
		t.Fatalf("map[string]string 处置漂移: %v", m["m2"])
	}
	s1 := m["s1"].([]any)
	if s1[0] != "138****1234" || s1[1] != 42 || s1[2] != true {
		t.Fatalf("[]any 混合元素处置漂移: %v", s1)
	}
	if m["s2"].([]string)[0] != "138****1234" {
		t.Fatalf("[]string 处置漂移: %v", m["s2"])
	}

	// 标量原样（USAGE.md 已文档化的覆盖盲区）：数字、布尔、json.Number、
	// 自定义字符串命名类型都按标量放行——判定面只认 string。
	in := map[string]any{
		"num":    42,
		"f64":    3.14,
		"flag":   true,
		"jn":     json.Number("13800001234"),
		"custom": shapeTestStr(synMobile),
	}
	out2, _ := ApplyValue(SurfaceToolResults, in)
	m2 := out2.(map[string]any)
	if m2["num"] != 42 || m2["f64"] != 3.14 || m2["flag"] != true {
		t.Fatalf("标量被误杀: %v", m2)
	}
	if m2["jn"] != json.Number("13800001234") {
		t.Fatalf("json.Number 应按标量原样返回（文档化盲区）: %v", m2["jn"])
	}
	if _, ok := m2["custom"].(shapeTestStr); !ok {
		t.Fatalf("字符串命名类型应保类型原样返回，实得 %T", m2["custom"])
	}

	// nil（无类型与带类型）原样保留。
	var nilByte []byte
	out3, b3 := ApplyValue(SurfaceToolResults, map[string]any{
		"nilAny": nil, "nilBytes": nilByte,
	})
	if b3 {
		t.Fatal("nil 不是未登记形状，不得阻断")
	}
	m3 := out3.(map[string]any)
	if m3["nilAny"] != nil {
		t.Fatalf("无类型 nil 应原样: %v", m3["nilAny"])
	}
	if v, ok := m3["nilBytes"].([]byte); !ok || v != nil {
		t.Fatalf("带类型 nil 应保类型原样: %#v", m3["nilBytes"])
	}
}

// shapeTestStr 自定义字符串命名类型（回归护栏夹具：type switch 不认它，
// 行为必须与修复前一致——原样返回）。
type shapeTestStr string

// TestValidateShapeCatchesUnsupported 接线期自查入口：像 ValidateSurface 一样，
// 把「运行期才会炸出来的 fail-closed」提前到装配期。
func TestValidateShapeCatchesUnsupported(t *testing.T) {
	// 实证三形状必须被拦下。
	for name, v := range map[string]any{
		"[]map[string]string": []map[string]string{{"tel": synMobile}},
		"map[string][]string": map[string][]string{"tel": {synMobile}},
		"[]byte":              []byte(synMobile),
	} {
		if err := ValidateShape(v); !errors.Is(err, ErrUnsupportedShape) {
			t.Fatalf("%s 应被 ValidateShape 拦下，实得 %v", name, err)
		}
	}

	// 支持面放行：四种容器 + 标量 + nil + 嵌套。
	ok := []any{
		"plain",
		map[string]any{"a": "x", "b": []string{"y"}, "c": 42, "d": nil},
		map[string]string{"k": "v"},
		[]any{"a", 1, true, map[string]any{"deep": []any{}}},
		[]string{"a", "b"},
		42, 3.14, true,
		json.Number("13800001234"),
		shapeTestStr(synMobile),
		nil,
	}
	var nilBytes []byte
	ok = append(ok, nilBytes) // 带类型 nil：运行期原样保留，接线期放行
	for i, v := range ok {
		if err := ValidateShape(v); err != nil {
			t.Fatalf("第 %d 个合法形状不得报错: %v", i, err)
		}
	}

	// 嵌套深处的未登记形状也要被揪出来，且报出可定位的路径。
	err := ValidateShape(map[string]any{"a": map[string]any{"b": []any{[]byte("x")}}})
	if !errors.Is(err, ErrUnsupportedShape) || !strings.Contains(err.Error(), "$.a.b[0]") {
		t.Fatalf("嵌套未登记形状应报出路径，实得 %v", err)
	}

	// map 迭代无序：错误信息必须确定（按键名排序取第一个坏形状）。
	e1 := ValidateShape(map[string]any{"zbad": []byte("x"), "abad": map[string][]int{}})
	e2 := ValidateShape(map[string]any{"zbad": []byte("x"), "abad": map[string][]int{}})
	if e1 == nil || e1.Error() != e2.Error() {
		t.Fatalf("ValidateShape 的报错必须与迭代顺序无关: %v vs %v", e1, e2)
	}
	if !strings.Contains(e1.Error(), "$.abad") {
		t.Fatalf("应报按键名排序靠前的坏形状，实得 %v", e1)
	}
}

// TestUnknownShapeNestedInSupportedContainer 未登记形状嵌在登记容器**内部**
// 时，只替换该子树并上浮阻断，不得波及兄弟字段。
func TestUnknownShapeNestedInSupportedContainer(t *testing.T) {
	restore(t)
	out, blocked := ApplyValue(SurfaceToolResults, map[string]any{
		"good":   []string{synMobile},
		"nested": map[string]any{"blob": []byte(synMobile), "tel": synMobile},
		"n":      42,
	})
	if !blocked {
		t.Fatal("嵌套的未登记形状必须把阻断上浮到顶层")
	}
	m := out.(map[string]any)
	if m["good"].([]string)[0] != "138****1234" {
		t.Fatalf("兄弟字段不得被波及: %v", m["good"])
	}
	nested := m["nested"].(map[string]any)
	if nested["blob"] != redactedPlaceholder {
		t.Fatalf("嵌套未登记形状应整段替换: %#v", nested["blob"])
	}
	if nested["tel"] != "138****1234" {
		t.Fatalf("同层的登记形状照常处置: %v", nested["tel"])
	}
	if m["n"] != 42 {
		t.Fatalf("标量原样: %v", m["n"])
	}
	if reflect.TypeOf(m["nested"]).Kind() != reflect.Map {
		t.Fatalf("外层登记容器结构保持: %T", m["nested"])
	}
}
