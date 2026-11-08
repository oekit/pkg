package stringx

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestMaskJSONFields(t *testing.T) {
	out, err := MaskJSONFields([]byte(`{"a":1,"jwtTokenPrivateKey":"secret","authTimeSalt":"salt"}`),
		map[string]string{"jwtTokenPrivateKey": "<sensitive>", "authTimeSalt": "<sensitive>"})
	if err != nil {
		t.Fatalf("MaskJSONFields() error = %v", err)
	}
	s := string(out)
	if strings.Contains(s, "secret") || strings.Contains(s, `salt"`) {
		t.Fatalf("masked output leaks secret: %s", s)
	}
	// 固定文案形态（json.Marshal 默认 HTML 转义 < > 为 \u003c \u003e——与既有消费方字节一致）
	if !strings.Contains(s, `"jwtTokenPrivateKey":"\u003csensitive\u003e"`) {
		t.Fatalf("mask text wrong: %s", s)
	}
}

// 字段缺省时会补上（map 赋值语义——与既有消费方行为一致）。
func TestMaskJSONFieldsAddsMissingFields(t *testing.T) {
	out, err := MaskJSONFields([]byte(`{"a":1}`),
		map[string]string{"missing": "<masked>"})
	if err != nil {
		t.Fatalf("MaskJSONFields() error = %v", err)
	}
	if !strings.Contains(string(out), `"missing":"\u003cmasked\u003e"`) {
		t.Fatalf("missing field not added: %s", string(out))
	}
}

// 非对象 JSON 报错；错误是 json 包的原始错误（消费方会把文本透传到响应，
// 包装会改文案——这是刻意的）。
func TestMaskJSONFieldsRawError(t *testing.T) {
	_, err := MaskJSONFields([]byte(`not json`), map[string]string{"a": "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cannot unmarshal") && !strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("error must be raw json error, got: %v", err)
	}
	var syn *json.SyntaxError
	if !errors.As(err, &syn) {
		t.Fatalf("error must unwrap to *json.SyntaxError, got %T", err)
	}
}

// JSON null：合法 JSON、解出 nil map（err==nil，错误分支拦不住）——旧实现
// 直接写 nil map panic（assignment to entry in nil map）。钉住 #254：null
// 与 `{}` 同口径（无字段可脱敏，原样返回）；对照组：数组仍按解析失败拒绝。
func TestMaskJSONFields_NullInput(t *testing.T) {
	masks := map[string]string{"password": "***"}

	out, err := MaskJSONFields([]byte("null"), masks)
	if err != nil {
		t.Fatalf("null 输入不应报错, got %v", err)
	}
	if string(out) != "null" {
		t.Fatalf("null 输入应原样返回, got %s", string(out))
	}

	// 带空白的 null 同口径（Unmarshal 语义下同样是 nil map）；原样返回 ⇒
	// 空白也原样保留
	const wsNull = "  null  "
	if wsOut, wsErr := MaskJSONFields([]byte(wsNull), masks); wsErr != nil {
		t.Fatalf("带空白的 null 不应报错, got %v", wsErr)
	} else if string(wsOut) != wsNull {
		t.Fatalf("带空白的 null 应原样返回, got %s", string(wsOut))
	}

	// 对照组（issue 同批输入）：数组是解析失败，返回 json 原始错误
	if _, arrErr := MaskJSONFields([]byte("[1,2]"), masks); arrErr == nil {
		t.Fatal("数组输入应报错（顶层必须是对象）")
	}

	// 对照组：空对象走掩码补字段路径，不触发 nil map
	out, err = MaskJSONFields([]byte(`{}`), masks)
	if err != nil {
		t.Fatalf("空对象不应报错, got %v", err)
	}
	if !strings.Contains(string(out), `"password":"***"`) {
		t.Fatalf("空对象应补上掩码字段, got %s", string(out))
	}

	// 对照组：正常对象掩码照常
	out, err = MaskJSONFields([]byte(`{"a":1}`), masks)
	if err != nil {
		t.Fatalf("正常对象不应报错, got %v", err)
	}
	if !strings.Contains(string(out), `"password":"***"`) {
		t.Fatalf("正常对象应补上掩码字段, got %s", string(out))
	}
}
