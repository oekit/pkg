package stringx

import "encoding/json"

// MaskJSONFields 把 JSON 对象里指定字段的值替换为固定文案（字段级脱敏）。
//
// 语义（与既有消费方 kuboard-go maskSensitiveSystemConfig 逐字一致——已验证实现上移）：
//   - 输入必须是**顶层 JSON 对象**；解析失败时返回 json 包的**原始错误**（不包装——
//     消费方会把错误文本透传到响应，包装即改文案，这是刻意的）；
//   - 输入为 JSON null 时与 `{}` 同口径处理（#254）：null 是合法 JSON 且
//     Unmarshal 后得到 nil map（err==nil，错误分支拦不住），nil map 写入
//     会 panic——脱敏常在日志/错误处理旁路上调用，panic 会盖掉真正的错误。
//     null 无字段可脱敏 ⇒ 原样返回；
//   - masks 里声明的字段**缺省时也会补上**（map 赋值语义）；
//   - 输出经 json.Marshal 重序列化（map 键按字典序输出——与消费方既有行为一致）。
func MaskJSONFields(raw []byte, masks map[string]string) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		// JSON null：合法输入、解出 nil map（#254）——无字段可脱敏，原样返回
		// （与空对象 `{}` 同口径），不得写 nil map panic。
		return raw, nil
	}
	for k, v := range masks {
		m[k] = v
	}
	return json.Marshal(m)
}
