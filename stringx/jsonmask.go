package stringx

import "encoding/json"

// MaskJSONFields 把 JSON 对象里指定字段的值替换为固定文案（字段级脱敏）。
//
// 语义（与既有消费方 kuboard-go maskSensitiveSystemConfig 逐字一致——已验证实现上移）：
//   - 输入必须是**顶层 JSON 对象**；解析失败时返回 json 包的**原始错误**（不包装——
//     消费方会把错误文本透传到响应，包装即改文案，这是刻意的）；
//   - masks 里声明的字段**缺省时也会补上**（map 赋值语义）；
//   - 输出经 json.Marshal 重序列化（map 键按字典序输出——与消费方既有行为一致）。
func MaskJSONFields(raw []byte, masks map[string]string) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for k, v := range masks {
		m[k] = v
	}
	return json.Marshal(m)
}
