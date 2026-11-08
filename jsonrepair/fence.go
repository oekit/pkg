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

// inlineFenceRun 在行内找**首个**长度 ≥3 的反引号连跑（其前缀是同一行的
// 散文，#293）：LLM 输出普遍把散文与开栏挤在同一段（"审查完成。```json"），
// v0.37.1 的正则支持该形态，逐行重写（#241）一度把它静默丢掉——整段输入
// 退化成空串。与 v0.37.1 一致取首个 ≥3 的连跑（前缀里可以有 1-2 反引号的
// 行内 code span，它们不是围栏候选）。
//
// 找不到返回 ok=false——调用方按「本行不是开栏」继续扫。
func inlineFenceRun(line string) (run int, rest string, ok bool) {
	for i := 0; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		n := 0
		for i+n < len(line) && line[i+n] == '`' {
			n++
		}
		if n >= 3 {
			return n, line[i+n:], true
		}
		i += n
	}
	return 0, "", false
}

// StripCodeFence 剥离 markdown 代码围栏：取第一个围栏块（语言标记
// json/yaml/toml/sql/sh/python… 一并剥掉，大小写均可）；无围栏原样
// TrimSpace 返回。多围栏块输出（模型补多段代码）语义为「取第一块」——与
// 宽容解析链一致。
//
// 判定按行扫（markdown 围栏规则，#241 起弃整串正则——非贪婪 `(.*?)` 会把
// 围栏**内容里**的 ``` 当结束标记，嵌套围栏被腰斩）：
//   - 开栏行：行首 ≥3 个反引号 + 可选语言标注（同行开闭形态 ```{...}```
//     单独处理）；**inline 开栏**（#293）：散文 + 同行 ≥3 反引号（
//     "审查完成。```json"）同样算开栏——该形态是 v0.37.1 正则既有的支持面，
//     #241 重写一度静默丢掉（整段退化成空输入，下游 Unmarshal 全线报
//     「空输入」）。inline 开栏的剥除结果是「剥掉散文前缀 + 语言标注」；
//   - 闭栏行：行首反引号个数 ≥ 开栏个数、其后仅空白——内容里的三反引号
//     行（个数不足，如 4 反引号外栏包 3 反引号内栏）或行中围栏（不在行首）
//     都不再被误当闭栏；
//   - 闭栏之后的其余行丢弃（「取第一块」契约不变）；只有开栏没有闭栏时
//     返回开栏行之后的全部内容（模型漏写闭栏是高频形态，内容仍是要的 JSON，
//     不因缺闭栏整体作废）。**inline 开栏例外**：闭栏缺席时本行按散文处理
//     继续扫——散文提及 ```（"用 ```json 语法"）而后续没有真围栏的输入，
//     若把散文后内容当块剥出会吞掉正文；v0.37.1 的正则同样要求闭栏才成块，
//     行首开栏「漏闭栏也返回其后全部」的宽容只保留给行首形态。
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
		inline := false
		if run < 3 {
			// inline 开栏（#293）：散文 + 同行 ≥3 反引号。找不到时本行不是
			// 开栏，继续扫下一行。
			r, r2, ok := inlineFenceRun(line)
			if !ok {
				continue
			}
			run, rest, inline = r, r2, true
		}
		// 同行开闭形态（```{...}``` / ```json{...}```）：剥掉开栏反引号与
		// 语言标注后，余下部分以 ≥run 个反引号结尾即闭合，中间即内容。
		body := strings.TrimSpace(rest[fenceTagEnd(rest):])
		if body != "" {
			if end := trailingFenceRun(body); end >= run {
				return strings.TrimSpace(body[:len(body)-end])
			}
		}
		// 多行形态：找行首闭合（反引号个数 ≥ 开栏、其后仅空白）。
		if inline {
			// inline 开栏的闭合扫描多一条**弃权规则**（#293）：行首出现
			// 「带语言标注的开栏」（反引号后不是空白）先于本候选的闭栏 ⇒
			// 本行多半是散文提及而非真围栏（"用 ```json 语法" 后面跟着
			// 真块）——放弃本候选，落到该真开栏上成块。带标注的行首围栏
			// 在 markdown 里不可能是闭栏，触发弃权是安全的。
			for j := i + 1; j < len(lines); j++ {
				cRun, cRest := fenceRun(lines[j])
				if cRun >= run && strings.TrimSpace(cRest) == "" {
					return strings.TrimSpace(strings.Join(lines[i+1:j], "\n"))
				}
				if cRun >= 3 && strings.TrimSpace(cRest) != "" {
					break
				}
			}
			continue // 没有闭合：inline 开栏按散文处理，不吞正文
		}
		for j := i + 1; j < len(lines); j++ {
			cRun, cRest := fenceRun(lines[j])
			if cRun >= run && strings.TrimSpace(cRest) == "" {
				return strings.TrimSpace(strings.Join(lines[i+1:j], "\n"))
			}
		}
		// 只有开栏没有闭栏：返回开栏行之后的全部（行首开栏保留该宽容）。
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
