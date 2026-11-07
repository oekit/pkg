package httpjson

import "strings"

// NormalizeBaseURL 规范化「SDK base 配置」形态的站点地址（zentao/内部平台
// 类客户端的 server 字段高频踩点）：补默认 scheme、去结尾斜杠、去首尾空白。
//
// 规则（字符级，不解析 URL——畸形输入按同样的字符规则走一遍）：
//   - 首尾空白去除；空串原样返回（调用方的「未配置」语义不被改写）；
//   - 不带 scheme 前缀时补 `http://`——不猜 https：内网自建服务占多数，
//     显式配置 https 的调用方不受影响；
//   - 结尾斜杠全部去掉，调用方拼路径时无需再写 strings.TrimSuffix。
//
// 收编自 zentao-mini/zentao-max 四份同源内联规范化（yi-nology#22 下沉）。
func NormalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "http://" + raw
	}
	return strings.TrimRight(raw, "/")
}
