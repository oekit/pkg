package convert

import (
	"encoding/json"
	"reflect"
	"testing"
)

// #391 回归：StructToMap 对 `json:"-,"` 的处理必须与 encoding/json 键名语义
// 一致——`json:"-"`（tag 整体恰为 "-"）排除；`json:"-,"`（带逗号 = 显式空
// 选项列表）是**名为 "-" 的普通字段**，照常输出。此前先剥选项再判 "-"，
// 两种形态都被排除 ⇒ 与 json.Marshal 悄悄分叉（敏感字段防泄漏场景下少一个
// 键且无任何信号）。对拍口径：StructToMap 的键集合与 json.Marshal 的键集合
// 完全一致（值经 json 往返比较）。

type structToMapJSONFixture struct {
	Plain  string `json:"plain"`
	Hidden string `json:"-"` // 整体 "-"：排除
	// legacy 形态：encoding/json 语义 = 名为 "-" 的普通字段（Go 1.25 起另有
	// 引号形态 json:"'-'"）。staticcheck SA5008 对 legacy 形态一律告警，但
	// 本测试的对象恰是该形态——StructToMap 必须与 encoding/json 对它同判。
	DashKey string `json:"-,"` //nolint:staticcheck // SA5008：legacy 形态正是本测试对象
	Omit    string `json:"omit,omitempty"`
	NoTag   string
}

func TestStructToMap_MatchesEncodingJSONKeys(t *testing.T) {
	v := structToMapJSONFixture{Plain: "p", Hidden: "h", DashKey: "d", Omit: "o", NoTag: "n"}

	got := StructToMap(v)

	// json.Marshal 的键集合为真值来源。
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(mapKeys(got), mapKeys(want)) {
		t.Fatalf("键集合与 json.Marshal 分叉: StructToMap=%v, json.Marshal=%v",
			mapKeys(got), mapKeys(want))
	}
	// 关键形态单独断言（失败信息直指分叉点）。
	if _, exists := got["-"]; !exists {
		t.Fatal(`json:"-," 字段应以键 "-" 输出（与 encoding/json 一致）`)
	}
	if _, exists := got["Hidden"]; exists {
		t.Fatal(`json:"-" 字段应被排除（敏感字段防泄漏语义保持）`)
	}
}

// TestStructToMap_DashKeyWithValueDash 形态收口：`json:"-,omitempty"` 同样是
// 名为 "-" 的普通字段（encoding/json：整体 tag != "-" 即走 parseTag 拆名）。
func TestStructToMap_DashKeyWithValueDash(t *testing.T) {
	type S struct {
		V string `json:"-,omitempty"` //nolint:staticcheck // SA5008：legacy 形态正是本测试对象
	}
	got := StructToMap(S{V: "x"})
	if got["-"] != "x" {
		t.Fatalf(`json:"-,omitempty" 应以键 "-" 输出, got %v`, got)
	}
	// 对照：整体 "-" 仍排除（既有用例 TestStructToMap_DashTag 的语义保持）。
	type T struct {
		V string `json:"-"`
	}
	if _, exists := StructToMap(T{V: "x"})["-"]; exists {
		t.Fatal(`json:"-" 应被排除`)
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// 排序保证比较确定性。
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
