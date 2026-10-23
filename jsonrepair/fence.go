package jsonrepair

import (
	"regexp"
	"strings"
)

// fenceRe markdown 代码围栏（```json/``` 均可；取第一个围栏块内容）。
var fenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")

// StripCodeFence 剥离 markdown 代码围栏：取第一个 ``` 围栏块；无围栏原样
// TrimSpace 返回。多围栏块输出（模型补多段代码）语义为「取第一块」——与
// 宽容解析链一致。
//
// 本函数是**围栏语义的单源**（下游 oagentkit/agentkit、agentkit-bisect、
// textx 各自的 textutil.StripCodeFence 均为此语义的副本，pkg#74 起因）：
// 围栏口径（多块取哪块、是否识别 json 变体等）要改只改此处，副本会漂移。
//
// 只剥围栏、不做 JSON 切片——需要「提取 JSON 文本」请用 ExtractJSON，
// 需要「宽容解析」请用 Unmarshal / ParseLenient。
func StripCodeFence(s string) string {
	trimmed := strings.TrimSpace(s)
	if m := fenceRe.FindStringSubmatch(trimmed); m != nil {
		return strings.TrimSpace(m[1])
	}
	return trimmed
}

// stripCodeFence 是 StripCodeFence 的包内别名——包内既有调用点沿用原名，
// 语义单源仍只有 StripCodeFence 一处实现。
func stripCodeFence(s string) string { return StripCodeFence(s) }
