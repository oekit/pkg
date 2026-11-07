package jsonrepair

import (
	"strings"
)

// fenceTagEnd 返回 info string 首词（语言标记，[a-zA-Z0-9_+-]*）的结束位置。
// 语言标记按字符集而非白名单：LLM 的结构化输出普遍带语言标记（yaml/toml/
// sql/sh/python…），白名单外的标记会残留在结果里污染下游解析（oagentkit/
// textx#21）。局限：info string 带空格元数据（```python title="x.py"）时
// 元数据之后的部分按行规则留在内容里——本函数只承诺剥语言标记。
func fenceTagEnd(rest string) int {
	i := 0
	for i < len(rest) && isFenceTagByte(rest[i]) {
		i++
	}
	return i
}

func isFenceTagByte(c byte) bool {
	return c == '_' || c == '+' || c == '-' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// fenceRun 返回行首反引号连跑长度与其后的行内容。
func fenceRun(line string) (run int, rest string) {
	for run < len(line) && line[run] == '`' {
		run++
	}
	return run, line[run:]
}

// StripCodeFence 剥离 markdown 代码围栏：取第一个围栏块（语言标记
// json/yaml/toml/sql/sh/python… 一并剥掉，大小写均可）；无围栏原样
// TrimSpace 返回。多围栏块输出（模型补多段代码）语义为「取第一块」——与
// 宽容解析链一致。
//
// 判定按行扫（markdown 围栏规则，#241 起弃整串正则——非贪婪 `(.*?)` 会把
// 围栏**内容里**的 ``` 当结束标记，嵌套围栏被腰斩）：
//   - 开栏行：行首 ≥3 个反引号 + 可选语言标注（同行开闭形态 ```{...}```
//     单独处理）；
//   - 闭栏行：行首反引号个数 ≥ 开栏个数、其后仅空白——内容里的三反引号
//     行（个数不足，如 4 反引号外栏包 3 反引号内栏）或行中围栏（不在行首）
//     都不再被误当闭栏；
//   - 闭栏之后的其余行丢弃（「取第一块」契约不变）；只有开栏没有闭栏时
//     返回开栏行之后的全部内容（模型漏写闭栏是高频形态，内容仍是要的 JSON，
//     不因缺闭栏整体作废）。
//
// 本函数是**围栏语义的单源**（下游 oagentkit/agentkit、agentkit-bisect、
// textx 各自的 textutil.StripCodeFence 均为此语义的副本，pkg#74 起因）：
// 围栏口径（多块取哪块、是否识别 json 变体等）要改只改此处，副本会漂移。
//
// 只剥围栏、不做 JSON 切片——需要「提取 JSON 文本」请用 ExtractJSON，
// 需要「宽容解析」请用 Unmarshal / ParseLenient。
func StripCodeFence(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	for i, line := range lines {
		run, rest := fenceRun(line)
		if run < 3 {
			continue
		}
		// 同行开闭形态（```{...}``` / ```json{...}```）：剥掉行首反引号与
		// 语言标注后，余下部分以 ≥run 个反引号结尾即闭合，中间即内容。
		body := strings.TrimSpace(rest[fenceTagEnd(rest):])
		if body != "" {
			if end := trailingFenceRun(body); end >= run {
				return strings.TrimSpace(body[:len(body)-end])
			}
		}
		// 多行形态：找行首闭合（反引号个数 ≥ 开栏、其后仅空白）。
		for j := i + 1; j < len(lines); j++ {
			cRun, cRest := fenceRun(lines[j])
			if cRun >= run && strings.TrimSpace(cRest) == "" {
				return strings.TrimSpace(strings.Join(lines[i+1:j], "\n"))
			}
		}
		// 只有开栏没有闭栏：返回开栏行之后的全部。
		return strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
	}
	return trimmed
}

// trailingFenceRun 返回 s 结尾的反引号连跑长度。
func trailingFenceRun(s string) (n int) {
	for n < len(s) && s[len(s)-1-n] == '`' {
		n++
	}
	return n
}

// stripCodeFence 是 StripCodeFence 的包内别名——包内既有调用点沿用原名，
// 语义单源仍只有 StripCodeFence 一处实现。
func stripCodeFence(s string) string { return StripCodeFence(s) }
