package httpjson

import "strings"

// NormalizeBaseURL 规范化「SDK base 配置」形态的站点地址（zentao/内部平台
// 类客户端的 server 字段高频踩点）：补默认 scheme、去结尾斜杠、去首尾空白。
//
// 规则（字符级，不整体解析 URL——畸形输入按同样的字符规则走一遍。刻意不走
// url.Parse 重组：u.String() 会重排/百分号转义输入，违背「小写输入零变化」；
// 这里只做三段字符级切分）：
//   - 首尾空白去除；空串原样返回（调用方的「未配置」语义不被改写）；
//   - scheme 前缀判定按 RFC 3986 §3.1 大小写不敏感（#239）：HTTP://、
//     Https:// 与小写形态等价，命中后 scheme 归一为小写（RFC 3986 的规范
//     形态），不再被二次补 http://（此前 `HTTPS://EXAMPLE.COM` 会产出
//     `http://HTTPS://EXAMPLE.COM`——scheme 降级 https→http、host 被截成
//     `HTTPS:`）。只认 http/https 两个 scheme：`host:port`、`mailto:x` 等
//     其余形态一律视为无 scheme 补 `http://`（v0.36.0 起的既有行为不变）；
//   - 不带 scheme 时补 `http://`——不猜 https：内网自建服务占多数，
//     显式配置 https 的调用方不受影响；
//   - 尾斜杠只裁 path 段（#240）：scheme 的 `//` 永不动（scheme-only 输入
//     `http://`、`http:///` 保持 `http://`，不再截成 `http:`）；`?query` 与
//     `#fragment` 原样保留（首个 ?/# 之后的内容不参与裁剪——此前
//     `http://host/?x=/` 的 query 值 `/` 被整串 TrimRight 静默吃成空串）。
//
// 收编自 zentao-mini/zentao-max 四份同源内联规范化（上游 zentao 矩阵#22 下沉）。
func NormalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	scheme, rest := "http://", raw
	for _, s := range [...]string{"http://", "https://"} {
		if len(raw) >= len(s) && strings.EqualFold(raw[:len(s)], s) {
			scheme, rest = s, raw[len(s):]
			break
		}
	}
	// 首个 ?/# 之后的 query/fragment 原样保留，尾斜杠只裁 authority+path 段。
	path, suffix := cutQueryFragment(rest)
	return scheme + strings.TrimRight(path, "/") + suffix
}

// cutQueryFragment 在首个 '?' 或 '#' 处切开 rest：前段是 authority+path
// （尾斜杠裁剪只作用于此段——scheme 已被剥掉，裁剪够不到 scheme 的 `//`），
// 后段（query/fragment）原样返回。
func cutQueryFragment(rest string) (head, tail string) {
	for i := range len(rest) {
		if rest[i] == '?' || rest[i] == '#' {
			return rest[:i], rest[i:]
		}
	}
	return rest, ""
}
