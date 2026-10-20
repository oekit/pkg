package jsonrepair

import (
	"encoding/json"
	"testing"
)

// TestExtractArray 抽数组件的形状面（与 TestExtractObject 对称的一对面）。
func TestExtractArray(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"plain array", `[1,2]`, `[1,2]`},
		{"array of objects", `[{"a":1}]`, `[{"a":1}]`},
		{"empty array", `[]`, `[]`},
		{"nested array", `[[1,2],3]`, `[[1,2],3]`},
		{"object field array", `[{"b":[2]}]`, `[{"b":[2]}]`},
		{"bracket inside string", `[{"a":"x]y"}]`, `[{"a":"x]y"}]`},
		{"escaped quote inside string", `[{"a":"x\"y]z"}]`, `[{"a":"x\"y]z"}]`},
		{"first of many segments", "[1]\n[2]", `[1]`},
		{"prose wrapped", "结果如下：\n[1,2]", `[1,2]`},
		{"prose with brace before", "说明 {备注}\n[1,2]", `[1,2]`},
		{"no array", `{"a":1}`, ``},
		{"empty", ``, ``},
	}
	for _, c := range cases {
		got := ExtractArray(c.input)
		if got != c.want {
			t.Errorf("%s: ExtractArray(%q) = %q, want %q", c.name, c.input, got, c.want)
		}
	}
}

// TestExtractObjectArraySymmetry 两侧抽取件同构：同一段文本，抽出各自形状的
// 首个平衡值（issue #37 的病根是「对象面有件、数组面没件」）。
func TestExtractObjectArraySymmetry(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantObj   string
		wantArray string
	}{
		{"each side one value", `{"a":1} [2]`, `{"a":1}`, `[2]`},
		{"multi segment", "{\"a\":1}\n{\"b\":2}\n[3]\n[4]", `{"a":1}`, `[3]`},
		{"prose wrapped", "结果：\n{\"a\":1}\n和\n[2]", `{"a":1}`, `[2]`},
	}
	for _, c := range cases {
		if got := ExtractObject(c.input); got != c.wantObj {
			t.Errorf("%s: ExtractObject(%q) = %q, want %q", c.name, c.input, got, c.wantObj)
		}
		if got := ExtractArray(c.input); got != c.wantArray {
			t.Errorf("%s: ExtractArray(%q) = %q, want %q", c.name, c.input, got, c.wantArray)
		}
	}
}

// arrayItem 目标 schema：数组面宿主形态。
type arrayItem struct {
	Assertion string `json:"assertion"`
}

// TestUnmarshalArrayFaceParity 数组面与对象面同构：多段/散文包裹两类形态，
// 数组面不再整段作废（issue #37 报的核心失效）。
func TestUnmarshalArrayFaceParity(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []arrayItem
	}{
		{
			"single array",
			`[{"assertion":"a"}]`,
			[]arrayItem{{Assertion: "a"}},
		},
		{
			"multi segment takes first",
			"[{\"assertion\":\"a\"}]\n[{\"assertion\":\"b\"}]",
			[]arrayItem{{Assertion: "a"}},
		},
		{
			"prose wrapped",
			"好的，结果如下：\n[{\"assertion\":\"a\"}]",
			[]arrayItem{{Assertion: "a"}},
		},
		{
			"prose with brace before array",
			"说明 {备注}\n[{\"assertion\":\"a\"}]",
			[]arrayItem{{Assertion: "a"}},
		},
		{
			"markdown fence",
			"```json\n[{\"assertion\":\"a\"}]\n```",
			[]arrayItem{{Assertion: "a"}},
		},
	}
	for _, c := range cases {
		var got []arrayItem
		if err := Unmarshal(c.input, &got); err != nil {
			t.Errorf("%s: Unmarshal(%q) error = %v, want nil", c.name, c.input, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: Unmarshal(%q) = %+v, want %+v", c.name, c.input, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: Unmarshal(%q) = %+v, want %+v", c.name, c.input, got, c.want)
				break
			}
		}
	}
}

// TestUnmarshalObjectFaceUnchanged 对象面既有行为不回归（issue #37 验收项）：
// 多段对象仍取首个，散文包裹仍能捞出对象。
func TestUnmarshalObjectFaceUnchanged(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  map[string]any
	}{
		{"multi segment takes first", "{\"action\":\"finish\"}\n{\"note\":\"补充\"}", map[string]any{"action": "finish"}},
		{"prose wrapped", "结果如下：\n{\"action\":\"finish\"}", map[string]any{"action": "finish"}},
		{"plain", `{"action":"finish"}`, map[string]any{"action": "finish"}},
	}
	for _, c := range cases {
		var got map[string]any
		if err := Unmarshal(c.input, &got); err != nil {
			t.Errorf("%s: Unmarshal(%q) error = %v, want nil", c.name, c.input, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: Unmarshal(%q) = %v, want %v", c.name, c.input, got, c.want)
			continue
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("%s: Unmarshal(%q) = %v, want %v", c.name, c.input, got, c.want)
				break
			}
		}
	}
}

// TestUnmarshalArrayFaceNoRegressionObjectTarget 数组面新增能力不得反过来
// 抢走对象面：目标类型是 map 时，数组文本仍按既有行为抽对象。
func TestUnmarshalArrayFaceNoRegressionObjectTarget(t *testing.T) {
	var got map[string]any
	if err := Unmarshal(`[{"a":1}]`, &got); err != nil {
		t.Fatalf("Unmarshal error = %v, want nil", err)
	}
	if got["a"] != float64(1) {
		t.Errorf("Unmarshal(`[{\"a\":1}]`) = %v, want map[a]=1", got)
	}
}

// TestUnmarshalLenientArrayFaceParity 宽松模式（WithAllowPartialArray）同样
// 覆盖数组面：多段数组与散文含 '{' 的数组不再整段作废。
func TestUnmarshalLenientArrayFaceParity(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []arrayItem
	}{
		{"multi segment", "[{\"assertion\":\"a\"}]\n[{\"assertion\":\"b\"}]", []arrayItem{{Assertion: "a"}}},
		{"prose with brace before", "说明 {备注}\n[{\"assertion\":\"a\"}]", []arrayItem{{Assertion: "a"}}},
	}
	for _, c := range cases {
		var got []arrayItem
		if err := UnmarshalWithOpts(c.input, &got, WithAllowPartialArray()); err != nil {
			t.Errorf("%s: UnmarshalWithOpts(%q) error = %v, want nil", c.name, c.input, err)
			continue
		}
		if len(got) != len(c.want) || got[0] != c.want[0] {
			t.Errorf("%s: UnmarshalWithOpts(%q) = %+v, want %+v", c.name, c.input, got, c.want)
		}
	}
}

// TestExtractArrayNotConfusedByBracesInStrings 字符串里的 '{' '}' 不参与括号栈
// 计数（抽取件的字符串感知纪律）。
func TestExtractArrayNotConfusedByBracesInStrings(t *testing.T) {
	got := ExtractArray(`[{"a":"}{"},{"b":1}]`)
	want := `[{"a":"}{"},{"b":1}]`
	if got != want {
		t.Errorf("ExtractArray = %q, want %q", got, want)
	}
}

// TestExtractArrayUnclosed 未闭合数组返回剩余文本（交 Repair 补全），与
// ExtractObject 的未闭合语义同构。
func TestExtractArrayUnclosed(t *testing.T) {
	got := ExtractArray(`[{"a":1},{"b":2`)
	if got != `[{"a":1},{"b":2` {
		t.Errorf("ExtractArray = %q, want %q", got, `[{"a":1},{"b":2`)
	}
}

// TestParseLenientArrayFace 兜底链的数组面分支直接可用（ParseLenient 是
// Unmarshal 的第三级）。
func TestParseLenientArrayFace(t *testing.T) {
	var got []arrayItem
	err := ParseLenient("[{\"assertion\":\"a\"}]\n[{\"assertion\":\"b\"}]", &got, nil)
	if err != nil {
		t.Fatalf("ParseLenient error = %v, want nil", err)
	}
	b, _ := json.Marshal(got)
	if string(b) != `[{"assertion":"a"}]` {
		t.Errorf("ParseLenient = %s, want %s", b, `[{"assertion":"a"}]`)
	}
}
