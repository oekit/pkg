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
			// 尾逗号被保守链（ExtractObject+Repair 括号闭合）抢先修好，
			// 且无内容缺失 → partial=false。
			name:      "截断在悬空逗号",
			in:        `{"list":[{"HostName":"a"},`,
			wantHosts: 1, wantFirst: "a",
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

// TestUnmarshalLenient_PartialObjectAndArray 截断形态矩阵：字符串中段 /
// 悬空冒号 / 悬空逗号闭合正确。纯括号截断（{"a":{"b":[1）由保守链的
// Repair 括号闭合抢先修复，partial=false（无内容缺失）。
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
		{"纯括号截断", `{"a":{"b":[1`, `{"a":{"b":[1]}}`, false},
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
