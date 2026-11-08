package jsonrepair

import "testing"

// #293 回归钉：v0.38.0 把 StripCodeFence 从正则改成行首扫描器时，把
// 「散文 + 同行开栏」的 inline 围栏形态静默丢掉了——`fenceRun` 只数行首
// 反引号，"审查完成。"```json`` 这样的行 run=0 被整行跳过，最后闭栏行被
// 当成「只有开栏没有闭栏」返回其后内容（空），整段输入退化成空串，
// Unmarshal 报「空输入」。v0.37.1 的正则对该形态一直工作正常，而模型输出
// 把散文与 JSON 围栏挤在同一段是高频形态。
//
// 语义（与 v0.37.1 对齐、且不复活 #241 嵌套围栏腰斩）：
//   - inline 开栏 = 同行首个 ≥3 反引号连跑（前缀可是任意散文，含 1-2 反引号
//     的行内 code span）；
//   - inline 开栏**必须**找到闭栏（行首闭合或同行闭合）才成块——散文提及
//     ``` 而后续无真围栏的输入原样返回，不吞正文；
//   - 真开栏（带语言标注的行首围栏）先于 inline 候选的闭栏出现时，inline
//     候选按散文弃权，真块照常成块。

func TestStripCodeFence_InlineOpening_Issue293(t *testing.T) {
	// issue 原样复现：散文与开栏同行，v0.38.0 返回 ""（丢整段）
	inline := "审查完成。" + "```json\n" + `[{"a":1}]` + "\n```"
	if got := StripCodeFence(inline); got != `[{"a":1}]` {
		t.Fatalf("StripCodeFence(inline) = %q, want %[2]q（#293：inline 开栏不得丢）", got, `[{"a":1}]`)
	}

	var v []map[string]int
	if err := Unmarshal(inline, &v); err != nil {
		t.Fatalf("Unmarshal(inline) = %v（#293：下游宽容解析链必须恢复）", err)
	}
	if len(v) != 1 || v[0]["a"] != 1 {
		t.Fatalf("Unmarshal 结果不符: %+v", v)
	}
}

func TestStripCodeFence_InlineOpening(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "英文散文前缀",
			in:   "Here is the result.```json\n{\"a\":1}\n```",
			want: `{"a":1}`,
		},
		{
			name: "同行开闭形态",
			in:   "审查完成。```json {\"a\":1}```",
			want: `{"a":1}`,
		},
		{
			name: "前缀含单反引号 code span——首个 ≥3 连跑才是开栏",
			in:   "用 `fmt` 打印```json\n{\"a\":1}\n```",
			want: `{"a":1}`,
		},
		{
			name: "散文提及围栏 + 后随真块——inline 候选弃权、真块成块",
			in:   "请用 ```json 语法输出\n```json\n{\"a\":1}\n```",
			want: `{"a":1}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripCodeFence(tt.in); got != tt.want {
				t.Fatalf("StripCodeFence = %q, want %q", got, tt.want)
			}
		})
	}
}

// 负对照：inline 开栏找不到闭栏时按散文处理——返回原输入，绝不返回空串
// （空串正是 #293 的退化形态）。v0.37.1 的正则同样要求闭栏才成块。
func TestStripCodeFence_InlineOpening_NoCloseReturnsOriginal(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{
			name: "散文提及 ``` 无真围栏",
			in:   "请用 ```json 语法输出结果",
		},
		{
			name: "真 inline 开栏但漏写闭栏",
			in:   "审查完成。```json\n[{\"a\":1}]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripCodeFence(tt.in)
			want := tt.in
			if want == "" {
				want = "（非空）"
			}
			if got != want {
				t.Fatalf("无闭栏的 inline 开栏应原样返回，got %q, want %q", got, want)
			}
			if got == "" {
				t.Fatalf("不得退化成空串（#293 的原始缺陷形态）")
			}
		})
	}
}

// 行首围栏既有语义不回退（#241 嵌套围栏硬化 + 漏闭栏宽容只属于行首形态）。
func TestStripCodeFence_LineStartSemanticsUnchanged(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "行首开栏照常剥除",
			in:   "```json\n[{\"a\":1}]\n```",
			want: `[{"a":1}]`,
		},
		{
			name: "行首开栏漏闭栏：返回其后全部（行首形态保留宽容）",
			in:   "```json\n[{\"a\":1}]",
			want: `[{"a":1}]`,
		},
		{
			name: "4 反引号外栏包 3 反引号内栏（#241）：内栏是内容",
			in:   "````markdown\n```json\n{\"a\":1}\n```\n````",
			want: "```json\n{\"a\":1}\n```",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripCodeFence(tt.in); got != tt.want {
				t.Fatalf("StripCodeFence = %q, want %q", got, tt.want)
			}
		})
	}
}
