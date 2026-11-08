package jsonrepair

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractObject(t *testing.T) {
	if got := ExtractObject(`前缀散文 {"a":{"b":1}} 后缀`); got != `{"a":{"b":1}}` {
		t.Errorf("ExtractObject = %q", got)
	}
	if got := ExtractObject(`no object here`); got != "" {
		t.Errorf("无对象应空串, got %q", got)
	}
	// 未闭合
	got := ExtractObject(`{"a": {"b": 1`)
	if got == "" {
		t.Fatal("未闭合应返回剩余文本")
	}
}

func TestRepair(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a": 1,}`, `{"a": 1}`},
		{`{"a": "x\|y"}`, `{"a": "x\\|y"}`}, // #263：非法转义不再丢反斜杠（\| → \\| 字面量保留）
		{`{"a": 1`, `{"a": 1}`},
		{`{"a"：1}`, `{"a":1}`},
		{`{"a"：1，"b"：2}`, `{"a":1,"b":2}`},
	}
	for _, c := range cases {
		if got := Repair(c.in); got != c.want {
			t.Errorf("Repair(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// 字符串内全角应保留
	if got := Repair(`{"a": "中文：保留"}`); got != `{"a": "中文：保留"}` {
		t.Errorf("字符串内全角被改动: %q", got)
	}
}

// TestRepairConvergesSinglePass 钉住 #262：连续尾逗号一次清空——单次调用的
// 返回值必须已是合法 JSON，而不是「再修一轮才收敛」。
func TestRepairConvergesSinglePass(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":1,,}`, `{"a":1}`},
		{`{"a":1,,,}`, `{"a":1}`},
		{`{,,}`, `{}`},
		{`[[1,,]]`, `[[1]]`},
		{`{"a":1, ,}`, `{"a":1}`},            // 逗号段夹空白同样一次清空
		{`{"a":1,,,"b":2}`, `{"a":1,"b":2}`}, // 值间连续逗号合并为一个
	}
	for _, c := range cases {
		got := Repair(c.in)
		if got != c.want {
			t.Errorf("Repair(%q) = %q, want %q", c.in, got, c.want)
		}
		if again := Repair(got); again != got {
			t.Errorf("Repair(%q) 不收敛：%q → %q", c.in, got, again)
		}
		if !json.Valid([]byte(got)) {
			t.Errorf("Repair(%q) = %q 仍非法 JSON（#262 契约：单次输出即合法）", c.in, got)
		}
	}
}

// TestRepairIllegalEscapeKeepsBackslash 钉住 #263：非法转义不再丢反斜杠——
// 丢掉会把值改写成另一个（\x41 → x41），且输出合法 JSON 无人能发现。反斜杠
// 转义为 `\\` 忠实保留「反斜杠 + 后续字符」字面量；残缺 \u 形态同口径。
func TestRepairIllegalEscapeKeepsBackslash(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":"\x41"}`, `{"a":"\\x41"}`},
		{`{"a":"\q"}`, `{"a":"\\q"}`},
		{`{"a":"\uZZZZ"}`, `{"a":"\\uZZZZ"}`}, // 残缺 \u 与其它非法转义统一为保留字面量
		{`{"a":"\u0041"}`, `{"a":"\u0041"}`},  // 合法 \uXXXX 原样保留
		{`{"a":"\/ok"}`, `{"a":"\/ok"}`},      // \/ 本就是合法转义
	}
	for _, c := range cases {
		got := Repair(c.in)
		if got != c.want {
			t.Errorf("Repair(%q) = %q, want %q", c.in, got, c.want)
		}
		if !json.Valid([]byte(got)) {
			t.Errorf("Repair(%q) = %q 仍非法 JSON", c.in, got)
		}
	}
	// EOF 悬挂 `\`：反斜杠按字面量保留（不再静默吞掉）。字符串本身未闭合
	// 超出 Repair 能力（见 closePartial 文档），此处只钉「反斜杠不被吃」。
	if got := Repair(`{"a":"abc\`); !strings.Contains(got, `\\`) {
		t.Errorf("Repair(%q) = %q，EOF 悬挂反斜杠被吃掉", `{"a":"abc\`, got)
	}
	// 语义核对：修出来的字符串值就是原文的「反斜杠 + 字符」。
	var v struct {
		A string `json:"a"`
	}
	if err := json.Unmarshal([]byte(Repair(`{"a":"\x41"}`)), &v); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if v.A != `\x41` {
		t.Errorf("值被改写: %q, want %q", v.A, `\x41`)
	}
}

// TestRepairMissingValueNull 钉住 #268：缺值分支补 null——`{"a":}` 是非法
// JSON 的稳定不动点，此前修复只补括号不补值，修复链路无出路。
func TestRepairMissingValueNull(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":`, `{"a":null}`},
		{`{"a":1,"b":`, `{"a":1,"b":null}`},
		{`{"a":}`, `{"a":null}`},
		{`{"a": ,"b":2}`, `{"a": null,"b":2}`}, // 空白原样保留，null 落在逗号前
		{`{"a":,}`, `{"a":null}`},
		{`{"a":  `, `{"a":  null}`}, // 尾随空白后 EOF 同样补值
		{`{"a":[1,{"b":`, `{"a":[1,{"b":null}]}`},
	}
	for _, c := range cases {
		got := Repair(c.in)
		if got != c.want {
			t.Errorf("Repair(%q) = %q, want %q", c.in, got, c.want)
		}
		if !json.Valid([]byte(got)) {
			t.Errorf("Repair(%q) = %q 仍非法 JSON", c.in, got)
		}
		if again := Repair(got); again != got {
			t.Errorf("Repair(%q) 不收敛：%q → %q", c.in, got, again)
		}
	}
}

// TestRepairOutputValidJSONInvariant 不变量测试（#268 建议）：对一批已知畸形
// 输入，Repair 的输出必须逐一满足 json.Valid——同时挡住「尾逗号只删一个」
// 与「非法转义被吃掉」两类「修完还是非法」的回归。
func TestRepairOutputValidJSONInvariant(t *testing.T) {
	broken := []string{
		// 连续尾逗号（#262）
		`{"a":1,,}`, `{"a":1,,,}`, `{,,}`, `[[1,,]]`, `{"a":1,}`, `{"a":1, ,}`,
		`{"a":1,,,"b":2}`,
		// 缺值（#268）
		`{"a":`, `{"a":1,"b":`, `{"a":}`, `{"a": ,"b":2}`, `{"a":,}`, `{"a":[1,{"b":`,
		// 非法转义（#263）
		`{"a":"\x41"}`, `{"a":"\q"}`, `{"a":"\uZZZZ"}`,
		// 其它语法级损坏（注：字符串未闭合 / 字面量截断（tru）/ 错配括号 /
		// 值位后的多余冒号不在 Repair 能力内——closePartial 文档记录的能力
		// 边界，不入本清单）
		`{"a":1`, `{"a"：1，}`, `[1,2,`, `[`, `{`, `{"a":{"b":1`,
		`[{"a":1},]`,
	}
	for _, in := range broken {
		out := Repair(in)
		if !json.Valid([]byte(out)) {
			t.Errorf("Repair(%q) = %q 不是合法 JSON（不变量被破坏）", in, out)
		}
	}
}

func TestNormalize(t *testing.T) {
	schema := &Schema{
		StringKeys: map[string]bool{"summary": true},
		ListKeys:   map[string]bool{"steps": true},
	}
	raw := json.RawMessage(`{"summary": true, "steps": "only-one", "other": 1}`)
	out, err := Normalize(raw, schema)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatal(err)
	}
	if m["summary"] != "true" {
		t.Errorf("summary = %v", m["summary"])
	}
	steps, ok := m["steps"].([]any)
	if !ok || len(steps) != 1 || steps[0] != "only-one" {
		t.Errorf("steps = %v", m["steps"])
	}
}

func TestNormalizeFlatten(t *testing.T) {
	schema := &Schema{StringKeys: map[string]bool{"redirect": true, "conclusion": true}}
	raw := json.RawMessage(`{"redirect":{"to":"x","reason":"y"}, "conclusion":[{"title":"A","detail":"B"}]}`)
	out, err := Normalize(raw, schema)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(out), &m)
	if m["redirect"] != "x；y" && m["redirect"] != "y；x" {
		// 字典序：reason < to → "y；x"
		t.Logf("redirect = %v", m["redirect"])
		if s, _ := m["redirect"].(string); s == "" {
			t.Errorf("redirect 未扁平化: %v", m["redirect"])
		}
	}
	if s, _ := m["conclusion"].(string); s != "1. A：B" {
		t.Errorf("conclusion = %v", m["conclusion"])
	}
}

func TestParseLenient(t *testing.T) {
	var v map[string]any
	// 栅栏
	if err := ParseLenient("```json\n{\"a\":1}\n```", &v, nil); err != nil || v["a"] != float64(1) {
		t.Fatalf("fence: %v %v", err, v)
	}
	// 尾逗号
	v = nil
	if err := ParseLenient(`{"a":1,}`, &v, nil); err != nil || v["a"] != float64(1) {
		t.Fatalf("trailing comma: %v %v", err, v)
	}
	// 散文包裹
	v = nil
	if err := ParseLenient(`根据分析，报告如下：{"a":2} 供参考`, &v, nil); err != nil || v["a"] != float64(2) {
		t.Fatalf("prose: %v %v", err, v)
	}
	// 彻底失败
	if err := ParseLenient(`not json at all`, &v, nil); err == nil {
		t.Fatal("非 JSON 应报错")
	}
}
