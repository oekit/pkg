package jsonrepair

import (
	"encoding/json"
	"testing"
)

// UnmarshalLenient/UnmarshalWithOpts 的场景表直接对齐 issue#7 的真机实测
// 表（TR-069 光猫抓取适配器：厂商固件 AJAX 响应，截断是最高频损坏）。
func TestUnmarshalLenient_IssueScenarios(t *testing.T) {
	type payload struct {
		List []struct {
			HostName string `json:"HostName"`
		} `json:"list"`
	}
	tests := []struct {
		name        string
		in          string
		wantErr     bool
		wantPartial bool
		wantHosts   int
		wantFirst   string
	}{
		{
			name:      "完整 JSON",
			in:        `{"list":[{"HostName":"cta1000a"}]}`,
			wantHosts: 1, wantFirst: "cta1000a",
		},
		{
			name:      "markdown 围栏",
			in:        "```json\n{\"list\":[{\"HostName\":\"cta1000a\"}]}\n```",
			wantHosts: 1, wantFirst: "cta1000a",
		},
		{
			name:      "数组尾逗号",
			in:        `{"list":[{"HostName":"a"},]}`,
			wantHosts: 1, wantFirst: "a",
		},
		{
			name:      "前后夹带说明文字",
			in:        `设备如下 {"list":[{"HostName":"a"}]} 请查收`,
			wantHosts: 1, wantFirst: "a",
		},
		{
			name:        "截断在字符串值中段（issue 主案例）",
			in:          `{"list":[{"HostName":"trunc`,
			wantPartial: true, wantHosts: 1, wantFirst: "trunc",
		},
		{
			// 键位截断：闭引号后补 :null，解码为该字段零值——
			// 「宁可少显示（字段缺失）也不能什么都不显示」。
			name:        "截断在键名中段",
			in:          `{"list":[{"HostName"`,
			wantPartial: true, wantHosts: 1, wantFirst: "",
		},
		{
			// 尾逗号在 EOF 处悬空 = 截断迹象（逗号后可能有被切掉的元素）：
			// 修复链（ExtractObject+Repair 括号闭合）救活也必须 partial=true
			// （#323：partial 挂在原文是否截断上，与由哪条链救活无关）。
			name:        "截断在悬空逗号",
			in:          `{"list":[{"HostName":"a"},`,
			wantPartial: true, wantHosts: 1, wantFirst: "a",
		},
		{
			name:        "截断在悬挂转义",
			in:          `{"list":[{"HostName":"a\`,
			wantPartial: true, wantHosts: 1, wantFirst: "a",
		},
		{
			// 文档化能力边界：截断在字面量中段（tru/fal/nul）不编造。
			name:    "截断在字面量中段",
			in:      `{"a":tru`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got payload
			partial, err := UnmarshalLenient(tt.in, &got)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got partial=%v payload=%+v", partial, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if partial != tt.wantPartial {
				t.Errorf("partial = %v, want %v", partial, tt.wantPartial)
			}
			if len(got.List) != tt.wantHosts {
				t.Fatalf("hosts = %d, want %d", len(got.List), tt.wantHosts)
			}
			if got.List[0].HostName != tt.wantFirst {
				t.Errorf("HostName = %q, want %q", got.List[0].HostName, tt.wantFirst)
			}
		})
	}
}

// TestUnmarshalLenient_ConservativeDefault 保守入口对截断报错——默认语义
// 不变（结果要么完整要么失败），宽松只走显式入口。
func TestUnmarshalLenient_ConservativeDefault(t *testing.T) {
	// 结尾无引号：真·字符串中段截断（带结尾引号的形态会被 Repair 括号
	// 闭合抢先修复，值完整、partial=false）。
	truncated := `{"list":[{"HostName":"trunc`
	var m map[string]any
	if _, err := UnmarshalLenient(truncated, &m); err != nil {
		t.Fatalf("UnmarshalLenient should repair truncation: %v", err)
	}
	if err := UnmarshalWithOpts(truncated, &m); err == nil {
		t.Fatal("UnmarshalWithOpts without opts must stay conservative")
	}
	if err := UnmarshalWithOpts(truncated, &m, WithAllowPartialArray()); err != nil {
		t.Fatalf("UnmarshalWithOpts with WithAllowPartialArray: %v", err)
	}
}

// TestUnmarshalLenient_GarbageStaysError 纯垃圾在宽松模式下仍报错：
// 闭合能力不得变成编造数据的通道。
func TestUnmarshalLenient_GarbageStaysError(t *testing.T) {
	garbage := []string{
		"<html>login expired</html>",
		"会话已过期，请重新登录",
		"```json\n```\n",
		"",
	}
	for _, g := range garbage {
		var m map[string]any
		if _, err := UnmarshalLenient(g, &m); err == nil {
			t.Errorf("garbage %q must stay an error", g)
		}
	}
}

// TestUnmarshalLenient_PartialObjectAndArray 截断形态矩阵：字符串中段闭合正确；
// 悬空冒号/悬空逗号自 #262/#268 起由保守链的 Repair 确定性修复（连续尾逗号一次
// 清空、EOF 悬空逗号删除、缺值补 null）。#323 起 partial 口径统一为「原文在
// EOF 处不完整」：修复链救活的截断形态同样是 partial=true——调用方要区分
// 「完整 JSON」与「截断后被修复的 JSON」，与结果由哪条链救活无关。
func TestUnmarshalLenient_PartialObjectAndArray(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string // 解码后重新 marshal 的 JSON
		wantPar bool
	}{
		{"对象截断在字符串", `{"a":"b","c":"tr`, `{"a":"b","c":"tr"}`, true},
		{"数组尾逗号", `[1,2,`, `[1,2]`, true},
		{"数组尾空白逗号", "[1,2, \n", "[1,2]", true},
		{"冒号截断", `{"a":`, `{"a":null}`, true},
		{"键位截断", `{"a":"b","c"`, `{"a":"b","c":null}`, true},
		{"纯括号截断", `{"a":{"b":[1`, `{"a":{"b":[1]}}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var v any
			partial, err := UnmarshalLenient(tt.in, &v)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if partial != tt.wantPar {
				t.Errorf("partial = %v, want %v", partial, tt.wantPar)
			}
			out, merr := json.Marshal(v)
			if merr != nil {
				t.Fatalf("remarshal: %v", merr)
			}
			if string(out) != tt.want {
				t.Errorf("got %s, want %s", out, tt.want)
			}
		})
	}
}

// TestClosePartial_NoTruncationIsNoop 均衡文本不做闭合：闭合一个完整文本
// 只会掩盖中段损坏（错误成功比失败更贵）。
func TestClosePartial_NoTruncationIsNoop(t *testing.T) {
	for _, s := range []string{`{"a":1}`, `[1,[2],3]`, "plain text", ""} {
		got, partial := closePartial(s)
		if partial {
			t.Errorf("closePartial(%q) claims partial", s)
		}
		if got != s {
			t.Errorf("closePartial(%q) = %q, want unchanged", s, got)
		}
	}
}

// TestUnmarshalLenient_BareStringRejected 顶层散串不救（宽松闭合只对
// 数组/对象）：防止把任意悬空引号文本「修」成一个字符串值。
func TestUnmarshalLenient_BareStringRejected(t *testing.T) {
	var s string
	if _, err := UnmarshalLenient(`"unterminated`, &s); err == nil {
		t.Fatal("bare unterminated string must stay an error")
	}
}

// TestUnmarshalLenient_PartialSurvivesRepairChain 钉住 #323：partial 判定挂在
// 「原文是否在 EOF 截断」上，与「修复链是否失败」解耦——Unmarshal 三级链自己
// 就会把截断 JSON 修成合法 JSON，此前「修复链救活 ⇒ return false」让 partial
// 在最常见的截断形态上恒为 false：调用方无法区分「完整 JSON」与「截断后被
// 修复的 JSON」（截断数组被闭合为合法单元素数组时元素数量被静默改变）。
// 对照组：完整内容（围栏/散文包裹/完整文本中的尾逗号）不算截断——内容无
// 缺失，partial=false 的「数据完整」信号必须留给它们。
func TestUnmarshalLenient_PartialSurvivesRepairChain(t *testing.T) {
	truncated := []string{
		`{"id":1,"name":"a","tags":["x"`,     // 数组未闭合
		`{"id":1,"name":"a","tags":["x","y"`, // 数组未闭合（多元素）
		`{"id":1,"name":"a"`,                 // 对象未闭合
		`{"id":1,"name":"a",`,                // 尾部逗号
		`{"id":1,"name":"a","tags":[`,        // 空数组截断
		`{"id":1,"name":`,                    // 悬空冒号（值缺失）
	}
	for _, in := range truncated {
		var v any
		partial, err := UnmarshalLenient(in, &v)
		if err != nil {
			t.Fatalf("UnmarshalLenient(%q): %v", in, err)
		}
		if !partial {
			t.Errorf("截断输入 %q 被修复链救活后 partial = false, want true", in)
		}
	}
	complete := []string{
		`{"id":1,"name":"a","tags":["x"]}`, // 完整 JSON
		"```json\n{\"id\":1}\n```",         // 围栏包裹（语法噪声，内容完整）
		`说明 {"id":1} 结束`,                   // 散文包裹
		`{"id":1,}`,                        // 完整文本中的尾逗号
	}
	for _, in := range complete {
		var v any
		partial, err := UnmarshalLenient(in, &v)
		if err != nil {
			t.Fatalf("UnmarshalLenient(%q): %v", in, err)
		}
		if partial {
			t.Errorf("完整输入 %q partial = true, want false（数据完整信号不得误报）", in)
		}
	}
}
