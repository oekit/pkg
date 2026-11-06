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
