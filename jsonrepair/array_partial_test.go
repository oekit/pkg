package jsonrepair

import (
	"encoding/json"
	"testing"
)

// TestClosePartial_ArrayElementStrings 回归测试：截断的数组、元素是字符串时，
// closePartial 必须闭合出**合法** JSON。
//
// 缺陷成因：closePartial 判断「引号是不是键位」只看它前面那个非空白字节是不是
// ','——但逗号既可能是对象的键值分隔符，也可能是数组元素的分隔符。数组里
// 第二个元素串前面正是 ',' ⇒ 被误判成键位 ⇒ EOF 闭合时补 ":null" ⇒ 产出
// ["a","b":null] 这种**非法** JSON。
func TestClosePartial_ArrayElementStrings(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"数组元素是字符串", `["a","b`, `["a","b"]`},
		{"嵌套数组元素是字符串", `{"a":["x","y`, `{"a":["x","y"]}`},
		{"数组里三个字符串", `["a","b","c`, `["a","b","c"]`},
		{"数组元素是对象", `[{"a":1},"b`, `[{"a":1},"b"]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			closed, isPartial := closePartial(c.text)
			if !isPartial {
				t.Fatalf("closePartial(%q) 未识别出截断", c.text)
			}
			if closed != c.want {
				t.Errorf("closePartial(%q) = %q，期望 %q", c.text, closed, c.want)
			}
			var probe any
			if err := json.Unmarshal([]byte(closed), &probe); err != nil {
				t.Fatalf("闭合结果不是合法 JSON：%q（%v）", closed, err)
			}
		})
	}
}

// TestUnmarshalLenient_TruncatedArrayOfStrings 回归测试：从解析入口看，
// 截断的数组（元素是字符串）必须能被宽松解析闭合出来。
//
// 实测（修前）：UnmarshalLenient(`["a","b`) 与 UnmarshalLenient(`{"a":["x","y`)
// 都返回 err「宽松模式无法闭合出合法 JSON」。
func TestUnmarshalLenient_TruncatedArrayOfStrings(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"数组元素是字符串", `["a","b`},
		{"嵌套数组元素是字符串", `{"a":["x","y`},
		{"数组里三个字符串", `["a","b","c`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var v any
			partial, err := UnmarshalLenient(c.text, &v)
			if err != nil {
				t.Fatalf("UnmarshalLenient(%q) 应能闭合出合法 JSON，实得 %v", c.text, err)
			}
			if !partial {
				t.Fatalf("UnmarshalLenient(%q) 应标记 partial=true", c.text)
			}
		})
	}
}

// TestUnmarshalLenient_TruncatedObjectKeyStillKey 回归护栏：**对象里的键位
// 截断**仍要补 ":null"（不能被上面的修复带偏）。
func TestUnmarshalLenient_TruncatedObjectKeyStillKey(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{`{"a":"b","c`, `{"a":"b","c":null}`},
		{`{"Host`, `{"Host":null}`},
		{`{"a":1,"b`, `{"a":1,"b":null}`},
	}
	for _, c := range cases {
		closed, isPartial := closePartial(c.text)
		if !isPartial {
			t.Fatalf("closePartial(%q) 未识别出截断", c.text)
		}
		if closed != c.want {
			t.Errorf("closePartial(%q) = %q，期望 %q", c.text, closed, c.want)
		}
		var probe any
		if err := json.Unmarshal([]byte(closed), &probe); err != nil {
			t.Errorf("闭合结果不是合法 JSON：%q（%v）", closed, err)
		}
	}
}

// TestClosePartial_ArrayElementAfterCommaNotKey 直接钉住判定口径：数组里
// 逗号之后的字符串**不是**键位。
func TestClosePartial_ArrayElementAfterCommaNotKey(t *testing.T) {
	closed, isPartial := closePartial(`["a","b`)
	if !isPartial {
		t.Fatal("应识别出截断")
	}
	if closed != `["a","b"]` {
		t.Fatalf("closePartial = %q，期望 [\"a\",\"b\"]", closed)
	}
}
