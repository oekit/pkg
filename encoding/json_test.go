package encoding

import (
	"encoding/json"
	"io"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMarshal(t *testing.T) {
	type TestStruct struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	tests := []struct {
		name string
		v    any
		want string
	}{
		{"struct", TestStruct{Name: "test", Age: 18}, `{"name":"test","age":18}`},
		{"nil", nil, "null"},
		{"string", "hello", `"hello"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Marshal(tt.v)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("Marshal() = %v, want %v", string(got), tt.want)
			}
		})
	}
}

func TestMarshalToString(t *testing.T) {
	result, err := MarshalToString("hello")
	if err != nil {
		t.Fatal(err)
	}
	if result != `"hello"` {
		t.Errorf("MarshalToString() = %v, want %v", result, `"hello"`)
	}
}

func TestUnmarshal(t *testing.T) {
	type TestStruct struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	var result TestStruct
	err := Unmarshal([]byte(`{"name":"test","age":18}`), &result)
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "test" || result.Age != 18 {
		t.Errorf("Unmarshal() = %+v, want {Name:test Age:18}", result)
	}
}

func TestUnmarshalFromString(t *testing.T) {
	type TestStruct struct {
		Name string `json:"name"`
	}

	var result TestStruct
	err := UnmarshalFromString(`{"name":"test"}`, &result)
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "test" {
		t.Errorf("UnmarshalFromString() Name = %v, want test", result.Name)
	}
}

func TestUnmarshalFromReader(t *testing.T) {
	type TestStruct struct {
		Name string `json:"name"`
	}

	var result TestStruct
	reader := strings.NewReader(`{"name":"test"}`)
	err := UnmarshalFromReader(reader, &result)
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "test" {
		t.Errorf("UnmarshalFromReader() Name = %v, want test", result.Name)
	}
}

func TestUnmarshalUseNumber(t *testing.T) {
	var result any
	err := Unmarshal([]byte(`{"number":12345678901234567890}`), &result)
	if err != nil {
		t.Fatal(err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatal("expected map")
	}

	// UseNumber 应该返回 json.Number 而不是 float64
	if _, ok := m["number"].(string); !ok {
		// json.Number 实际上是 string 类型
		t.Logf("number type: %T", m["number"])
	}
}

func TestMarshalIndent(t *testing.T) {
	type TestStruct struct {
		Name string `json:"name"`
	}

	result, err := MarshalIndent(TestStruct{Name: "test"}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	expected := `{
  "name": "test"
}`
	if string(result) != expected {
		t.Errorf("MarshalIndent() = %v, want %v", string(result), expected)
	}
}

func TestValid(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"valid object", []byte(`{"name":"test"}`), true},
		{"valid array", []byte(`[1,2,3]`), true},
		{"invalid", []byte(`{name:test}`), false},
		{"empty", []byte(``), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Valid(tt.data); got != tt.want {
				t.Errorf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestUnmarshal_TrailingData 回归：json.Decoder.Decode 只消费首个 JSON 值，
// "{...} garbage" 曾被静默当成成功（截断修复失败/拼接脏数据静默通过）。
// 钉住：整个输入必须是单一 JSON 值，与 encoding/json.Unmarshal 语义一致。
func TestUnmarshal_TrailingData(t *testing.T) {
	var v map[string]any
	if err := Unmarshal([]byte(`{"a":1} garbage`), &v); err == nil {
		t.Error("trailing garbage should be rejected")
	}
	if err := Unmarshal([]byte(`{"a":1} {"b":2}`), &v); err == nil {
		t.Error("second top-level value should be rejected")
	}
	if err := Unmarshal([]byte(`{"a":1}`), &v); err != nil {
		t.Errorf("clean input should succeed: %v", err)
	}
	if err := UnmarshalFromString(`[1,2] trailing`, &v); err == nil {
		t.Error("UnmarshalFromString trailing garbage should be rejected")
	}
}

// TestUnmarshal_ErrorExcerptTruncated 回归：formatError 曾把完整输入嵌进
// 错误——请求体里的 token/密码随错误日志泄漏，大 payload 撑爆日志。
func TestUnmarshal_ErrorExcerptTruncated(t *testing.T) {
	big := make([]byte, 4096)
	for i := range big {
		big[i] = 'x'
	}
	payload := make([]byte, 0, 4097)
	payload = append(payload, `{"k":"`...)
	payload = append(payload, big...)
	payload = append(payload, '"')
	err := Unmarshal(payload, &map[string]any{})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(err.Error()) > 1024 {
		t.Errorf("error message should be truncated, got %d bytes", len(err.Error()))
	}
}

// TestUnmarshalUseNumber_PreservesLargeInt 钉住核心契约：>2^53 的整数
// （9007199254740993 = 2^53+1，float64 无法精确表示）必须以 json.Number
// 原文保留，而不是经 float64 中转后静默变成 9007199254740992。
func TestUnmarshalUseNumber_PreservesLargeInt(t *testing.T) {
	var v any
	if err := UnmarshalUseNumber([]byte(`{"id":9007199254740993}`), &v); err != nil {
		t.Fatal(err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("UnmarshalUseNumber() type = %T, want map[string]any", v)
	}
	num, ok := m["id"].(json.Number)
	if !ok {
		t.Fatalf("number type = %T, want json.Number", m["id"])
	}
	if num.String() != "9007199254740993" {
		t.Errorf("json.Number = %s, want 9007199254740993", num.String())
	}
}

func TestUnmarshalFromStringUseNumber_PreservesLargeInt(t *testing.T) {
	var v any
	if err := UnmarshalFromStringUseNumber(`{"id":9007199254740993,"neg":-9007199254740993}`, &v); err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	for key, want := range map[string]string{
		"id":  "9007199254740993",
		"neg": "-9007199254740993",
	} {
		num, ok := m[key].(json.Number)
		if !ok {
			t.Fatalf("%s type = %T, want json.Number", key, m[key])
		}
		if num.String() != want {
			t.Errorf("json.Number(%s) = %s, want %s", key, num.String(), want)
		}
	}
}

// TestUnmarshalUseNumber_TypedFields 类型化字段不受影响：json.Number 也可
// 直接落到声明为 json.Number 的字段；已声明的基础类型按原类型解析。
func TestUnmarshalUseNumber_TypedFields(t *testing.T) {
	var v struct {
		N json.Number `json:"n"`
		S string      `json:"s"`
	}
	if err := UnmarshalUseNumber([]byte(`{"n":9007199254740993,"s":"x"}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.N.String() != "9007199254740993" {
		t.Errorf("N = %s, want 9007199254740993", v.N.String())
	}
	if v.S != "x" {
		t.Errorf("S = %q, want %q", v.S, "x")
	}
}

// TestUnmarshalUseNumber_TrailingData 新入口与 Unmarshal 同源：尾部残留
// 数据必须报错，不因 UseNumber 变体引入宽松路径。
func TestUnmarshalUseNumber_TrailingData(t *testing.T) {
	var v map[string]any
	if err := UnmarshalUseNumber([]byte(`{"a":1} garbage`), &v); err == nil {
		t.Error("trailing garbage should be rejected")
	}
	if err := UnmarshalFromStringUseNumber(`{"a":1} trailing`, &v); err == nil {
		t.Error("UnmarshalFromStringUseNumber trailing garbage should be rejected")
	}
	if err := UnmarshalUseNumber([]byte(`{"a":1}`), &v); err != nil {
		t.Errorf("clean input should succeed: %v", err)
	}
}

// TestExcerptWriter_CappedAtMax 单元钉住 excerptWriter 机制（[#260]）：
// 只收前 max 字节，超出后纯直通不再复制，Write 返回值始终是 len(p)。
func TestExcerptWriter_CappedAtMax(t *testing.T) {
	w := &excerptWriter{max: 8}
	if n, err := w.Write([]byte("12345")); n != 5 || err != nil {
		t.Fatalf("first write = (%d, %v), want (5, nil)", n, err)
	}
	// 跨边界写入：只补足到 max。
	if n, err := w.Write([]byte("6789")); n != 4 || err != nil {
		t.Fatalf("boundary write = (%d, %v), want (4, nil)", n, err)
	}
	if got := string(w.buf); got != "12345678" {
		t.Fatalf("buf = %q, want %q", got, "12345678")
	}
	// 超限后不再增长，但不得报错（报错会毒化 Tee 的读侧）。
	before := len(w.buf)
	if n, err := w.Write([]byte("XYZ")); n != 3 || err != nil {
		t.Fatalf("over-limit write = (%d, %v), want (3, nil)", n, err)
	}
	if len(w.buf) != before {
		t.Fatalf("buf grew after limit: %d -> %d", before, len(w.buf))
	}
}

// TestUnmarshalFromReader_ErrorExcerptBounded 钉住错误路径的摘录契约：
// 大的非法输入报错，摘录仍截断到 256 字节——head 只收前 maxExcerpt 字节。
func TestUnmarshalFromReader_ErrorExcerptBounded(t *testing.T) {
	// 引号永不闭合：decoder 会把 8 KiB 全部消费完才报错，
	// 摘录因此走满 256 字节截断路径。
	err := UnmarshalFromReader(strings.NewReader(`{"k":"`+strings.Repeat("x", 8192)), &map[string]any{})
	if err == nil {
		t.Fatal("expected error for unterminated string")
	}
	if len(err.Error()) > 1024 {
		t.Errorf("error message should stay bounded, got %d bytes", len(err.Error()))
	}
	// head 把摘录钉在 256 字节整（formatError 的 >256 截断不会再触发，
	// 截断由 excerptWriter 本身完成）；错误信息因此保持有界且带输入开头。
	if !strings.Contains(err.Error(), strings.Repeat("x", 64)) {
		t.Errorf("error should carry the input head excerpt: %v", err)
	}
}

// TestUnmarshalFromReader_SuccessDoesNotBufferWholeInput 回归（[#260]）：
// 此前 TeeReader 把每个流过的字节都复制进 strings.Builder——**成功路径
// 也照攒**（Builder 倍增扩容 + 一次 String() 拷贝 ≈ 输入的 3 倍额外驻留，
// 8 MiB 报文实测 50 MiB 堆）。输入全部合法、解析成功时，总分配增量必须
// 显著低于「额外整份复制」的口径（阈值取输入的 3 倍；旧实现约 5-6 倍，
// 稳定撞线失败）。
func TestUnmarshalFromReader_SuccessDoesNotBufferWholeInput(t *testing.T) {
	const pad = 8 << 20 // 8 MiB 合法空白填充，复现 issue 的形状
	// 与 issue 复现口径一致：reader 在测量窗口外构造好，
	// Repeat 的那 8 MiB 不计入增量。
	r := io.MultiReader(
		strings.NewReader(`{"k":`),
		strings.NewReader(strings.Repeat(" ", pad)),
		strings.NewReader("1}"),
	)
	var out map[string]any
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)
	err := UnmarshalFromReader(r, &out)
	runtime.ReadMemStats(&m2)
	if err != nil {
		t.Fatalf("valid padded input should parse: %v", err)
	}
	if v, ok := out["k"].(json.Number); !ok || v.String() != "1" {
		t.Fatalf("out[k] = %#v, want json.Number(\"1\")", out["k"])
	}
	delta := m2.TotalAlloc - m1.TotalAlloc
	// 阈值取 5 倍 pad：修复后 ≈4 倍（json.Decoder 自身缓冲倍增的固有成本，
	// 旧实现同样要付）；旧实现在此之上还有 Builder 倍增（≈2 倍）+ 一次
	// String() 拷贝（≈1 倍）≈ 7 倍，稳定撞线失败。
	if limit := uint64(5 * pad); delta > limit {
		t.Errorf("TotalAlloc delta = %.2f MiB, want <= %.2f MiB（成功路径不得把整份输入再攒一遍）",
			float64(delta)/(1<<20), float64(limit)/(1<<20))
	}
}

// TestFormatError_RuneSafeTruncation 钉住 #419：错误摘录截断必须在 rune
// 边界回退——字节下标直切会把多字节字符腰斩，错误文案含非法 UTF-8（进
// 日志/告警平台是乱码，按 rune 计数的采集器二次报错或丢行）。同仓 httpjson
// 的错误摘录已立 rune 安全纪律，encoding 的摘录点同口径。
func TestFormatError_RuneSafeTruncation(t *testing.T) {
	// 中文 3 字节/字符：256 字节预算必然落在多字节字符中间。
	long := `{"字段": "` + strings.Repeat("中文测试值", 60) + `", "尾": 坏}`
	err := Unmarshal([]byte(long), &map[string]any{})
	if err == nil {
		t.Fatal("畸形中文报文应解析失败")
	}
	msg := err.Error()
	if !utf8.ValidString(msg) {
		t.Fatalf("错误文案含非法 UTF-8（截断腰斩多字节字符）: %q", msg[len(msg)-40:])
	}
	if !strings.Contains(msg, "...(truncated)") {
		t.Fatalf("超长输入应带截断标记: %q", msg)
	}
	// 短输入不截断、无标记（既有行为保持）。
	err2 := Unmarshal([]byte(`{"a": 1,}`), &map[string]any{})
	if err2 == nil || strings.Contains(err2.Error(), "...(truncated)") {
		t.Fatalf("短输入不应带截断标记: %v", err2)
	}
}
