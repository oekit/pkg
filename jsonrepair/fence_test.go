package jsonrepair

import "testing"

// TestStripCodeFence 锁定导出原语 StripCodeFence 的行为（pkg#74）。
//
// 该函数是围栏语义的单源——下游 oagentkit/agentkit、agentkit-bisect、textx
// 各自的 textutil.StripCodeFence 是它的副本。本用例是副本漂移的哨兵：
// 语义一旦要改（多块取哪块、是否识别 json 变体等），副本方必须跟着改。
func TestStripCodeFence(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no fence", `{"a":1}`, `{"a":1}`},
		{"plain fence", "```\n{\"a\":1}\n```", `{"a":1}`},
		{"json fence", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"fence without newline", "```{\"a\":1}```", `{"a":1}`},
		{"no fence trims space", "  {\"a\":1}\n  ", `{"a":1}`},
		{"empty string", "", ""},
		{"fence only", "```\n```", ""},
		// 多围栏块取第一块——与 ExtractJSON/宽容解析链一致
		{"multi fence takes first", "```\nfirst\n```\n```\nsecond\n```", "first"},
		// 围栏外有散文：只取围栏内容
		{"prose around fence", "好的，结果如下：\n```json\n{\"a\":1}\n```\n希望有用", `{"a":1}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StripCodeFence(c.in); got != c.want {
				t.Errorf("StripCodeFence(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestStripCodeFence_MatchesPackageInternal 验证导出原语与包内既有用法
// （ExtractJSON / ExtractObject / ExtractArray 的前置剥围栏）语义一致——
// 包内别名指向同一实现，不存在两份口径。
func TestStripCodeFence_MatchesPackageInternal(t *testing.T) {
	inputs := []string{
		"```json\n{\"a\":1}\n```",
		"```\n[1,2,3]\n```",
		"plain text",
		"```\nfirst\n```\n```\nsecond\n```",
	}
	for _, in := range inputs {
		if got, want := stripCodeFence(in), StripCodeFence(in); got != want {
			t.Errorf("stripCodeFence(%q) = %q, StripCodeFence = %q, want equal", in, got, want)
		}
	}
}
