package encoding

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Marshal 将 v 序列化为 JSON 字节。
func Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// MarshalToString 将 v 序列化为 JSON 字符串。
func MarshalToString(v any) (string, error) {
	data, err := Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Unmarshal 将 JSON 字节反序列化到 v。
// 使用 UseNumber 模式解析数字，避免精度丢失。
// 与 encoding/json.Unmarshal 语义一致：整个输入必须是单一 JSON 值，
// 尾部残留数据（截断修复失败/拼接脏数据）视为错误而非静默忽略。
func Unmarshal(data []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := unmarshalStrict(decoder, v); err != nil {
		return formatError(string(data), err)
	}
	return nil
}

// UnmarshalFromString 将 JSON 字符串反序列化到 v。
// 语义同 Unmarshal：尾部残留数据视为错误。
func UnmarshalFromString(str string, v any) error {
	decoder := json.NewDecoder(strings.NewReader(str))
	if err := unmarshalStrict(decoder, v); err != nil {
		return formatError(str, err)
	}
	return nil
}

// UnmarshalUseNumber 将 JSON 字节反序列化到 v，数字显式解析为 json.Number
// （而非 float64）。js 数值只有 53 位整数精度，金额/雪花 ID 等 >2^53 的整数
// 经 float64 中转会静默丢失精度；json.Number 保留原文字面量，由业务按需
// 转 int64/string。目标字段为 any 时生效最明显；已声明类型的字段仍按
// 该类型解析。尾部残留数据视为错误（同 Unmarshal）。
func UnmarshalUseNumber(b []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(b))
	if err := unmarshalStrict(decoder, v); err != nil {
		return formatError(string(b), err)
	}
	return nil
}

// UnmarshalFromStringUseNumber 将 JSON 字符串反序列化到 v。
// 语义同 UnmarshalUseNumber：数字保留为 json.Number，防 >2^53 精度丢失。
func UnmarshalFromStringUseNumber(s string, v any) error {
	decoder := json.NewDecoder(strings.NewReader(s))
	if err := unmarshalStrict(decoder, v); err != nil {
		return formatError(s, err)
	}
	return nil
}

// UnmarshalFromReader 从 Reader 读取 JSON 并反序列化到 v。
//
// 流式解析，不把整个输入攒进内存：错误摘录只需要开头 256 字节，此前用
// strings.Builder 做 Tee 把**每个流过的字节**都复制下来，成功路径也照攒
// （Builder 倍增扩容 + 一次 String() 拷贝 ≈ 输入的 6 倍），8 MiB 报文吃掉
// ~50 MiB 堆（[#260]）。现在只驻留前 maxExcerpt 字节。
//
// 与 Unmarshal 语义一致：尾部残留数据视为错误。
func UnmarshalFromReader(reader io.Reader, v any) error {
	head := &excerptWriter{max: maxExcerpt}
	decoder := json.NewDecoder(io.TeeReader(reader, head))
	if err := unmarshalStrict(decoder, v); err != nil {
		return formatError(string(head.buf), err)
	}
	return nil
}

// maxExcerpt 错误摘录上限：完整原文会把请求体里的 token/密码等敏感字段
// 带进错误日志，大 payload 也会撑爆日志。
const maxExcerpt = 256

// excerptWriter 只保留前 max 字节的 io.Writer——TeeReader 的搭档。
// 超出 max 后纯计数直通，不再复制任何字节进 buf。
type excerptWriter struct {
	buf []byte
	max int
}

func (w *excerptWriter) Write(p []byte) (int, error) {
	if room := w.max - len(w.buf); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		w.buf = append(w.buf, p[:room]...)
	}
	return len(p), nil
}

func unmarshalUseNumber(decoder *json.Decoder, v any) error {
	decoder.UseNumber()
	return decoder.Decode(v)
}

// unmarshalStrict 解析首个 JSON 值后继续读，尾部必须是 EOF——
// 否则 Decode 只消费第一个值，"{...} garbage" 会静默成功。
func unmarshalStrict(decoder *json.Decoder, v any) error {
	if err := unmarshalUseNumber(decoder, v); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return fmt.Errorf("invalid trailing data: %w", err)
		}
		return errTrailingData
	}
	return nil
}

// errTrailingData 尾部残留数据（非单一 JSON 值）。
var errTrailingData = errors.New("invalid character after top-level value: input must be a single JSON value")

// formatError 构造带输入摘录的错误。摘录截断到 maxExcerpt（256）字节：
// 理由见该常量。
func formatError(v string, err error) error {
	excerpt := v
	if len(excerpt) > maxExcerpt {
		excerpt = excerpt[:maxExcerpt] + "...(truncated)"
	}
	return fmt.Errorf("string: `%s`, error: `%w`", excerpt, err)
}

// MarshalIndent 将 v 序列化为带缩进的 JSON 字节。
func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(v, prefix, indent)
}

// Valid 检查 data 是否是合法的 JSON。
func Valid(data []byte) bool {
	return json.Valid(data)
}
