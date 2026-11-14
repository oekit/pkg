package webhook

// IM 机器人载荷构造器测试（#485）：三家载荷 JSON 形态按字节钉住（结构体
// 字段序固定，输出确定）、转义正确性（内容含引号/反斜杠/换行可无损回读）、
// 必填内容 fail-closed、渠道分发与未知渠道哨兵。

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestWecomMarkdownPayload_WirePinned(t *testing.T) {
	raw, err := WecomMarkdownPayload("审批通过 **单据 #7**")
	if err != nil {
		t.Fatalf("构造不应出错, got %v", err)
	}
	want := `{"msgtype":"markdown","markdown":{"content":"审批通过 **单据 #7**"}}`
	if !bytes.Equal(raw, []byte(want)) {
		t.Fatalf("企微载荷线格式漂移:\n want %s\n got  %s", want, raw)
	}
}

func TestWecomMarkdownPayload_EscapeRoundtrip(t *testing.T) {
	// 内容含引号/反斜杠/换行/unicode：JSON 转义后必须无损回读
	md := "line1\n\"quoted\" \\path\t中文𝕌"
	raw, err := WecomMarkdownPayload(md)
	if err != nil {
		t.Fatalf("构造不应出错, got %v", err)
	}
	var dec struct {
		MsgType  string `json:"msgtype"`
		Markdown struct {
			Content string `json:"content"`
		} `json:"markdown"`
	}
	if err := json.Unmarshal(raw, &dec); err != nil {
		t.Fatalf("解码不应出错, got %v", err)
	}
	if dec.MsgType != "markdown" || dec.Markdown.Content != md {
		t.Fatalf("转义回读不符: msgtype=%q content=%q", dec.MsgType, dec.Markdown.Content)
	}
}

func TestDingtalkMarkdownPayload_WirePinned(t *testing.T) {
	raw, err := DingtalkMarkdownPayload("扁鹊审批", "单据 **#9** 已通过")
	if err != nil {
		t.Fatalf("构造不应出错, got %v", err)
	}
	want := `{"msgtype":"markdown","markdown":{"title":"扁鹊审批","text":"单据 **#9** 已通过"}}`
	if !bytes.Equal(raw, []byte(want)) {
		t.Fatalf("钉钉载荷线格式漂移:\n want %s\n got  %s", want, raw)
	}
}

func TestDingtalkMarkdownPayload_TitleIsHostContent(t *testing.T) {
	// #485 (e)：title 必须是参数而非写死——不同 title 产出不同载荷
	a, errA := DingtalkMarkdownPayload("宿主A", "x")
	b, errB := DingtalkMarkdownPayload("宿主B", "x")
	if errA != nil || errB != nil {
		t.Fatalf("构造不应出错: %v / %v", errA, errB)
	}
	if bytes.Equal(a, b) {
		t.Fatal("不同 title 应产出不同载荷（title 是宿主内容，不得写死）")
	}
	// 空标题按平台约定放行
	empty, err := DingtalkMarkdownPayload("", "x")
	if err != nil {
		t.Fatalf("空 title 应放行, got %v", err)
	}
	want := `{"msgtype":"markdown","markdown":{"title":"","text":"x"}}`
	if !bytes.Equal(empty, []byte(want)) {
		t.Fatalf("空 title 载荷不符:\n want %s\n got  %s", want, empty)
	}
}

func TestFeishuTextPayload_WirePinned(t *testing.T) {
	raw, err := FeishuTextPayload("审批已通过：单据 #7")
	if err != nil {
		t.Fatalf("构造不应出错, got %v", err)
	}
	want := `{"msg_type":"text","content":{"text":"审批已通过：单据 #7"}}`
	if !bytes.Equal(raw, []byte(want)) {
		t.Fatalf("飞书载荷线格式漂移:\n want %s\n got  %s", want, raw)
	}
}

func TestIMPayloadBuilders_FailClosedOnEmptyContent(t *testing.T) {
	cases := []struct {
		name string
		fn   func() ([]byte, error)
	}{
		{"企微空content", func() ([]byte, error) { return WecomMarkdownPayload("") }},
		{"钉钉空text", func() ([]byte, error) { return DingtalkMarkdownPayload("t", "") }},
		{"飞书空text", func() ([]byte, error) { return FeishuTextPayload("") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := tc.fn()
			if raw != nil {
				t.Fatalf("失败时不得产出载荷字节, got %s", raw)
			}
			if !errors.Is(err, ErrEmptyIMPayload) {
				t.Fatalf("空内容应包 ErrEmptyIMPayload, got %v", err)
			}
		})
	}
}

func TestBuildIMPayload_Dispatch(t *testing.T) {
	// 分发结果与直调构造器逐字节一致（策略表语义单源）
	wecom, err := BuildIMPayload(ChannelWecom, "t", "md", "text")
	if err != nil {
		t.Fatalf("wecom 不应出错, got %v", err)
	}
	wantWecom, _ := WecomMarkdownPayload("md")
	if !bytes.Equal(wecom, wantWecom) {
		t.Fatalf("wecom 分发不符:\n want %s\n got  %s", wantWecom, wecom)
	}

	ding, err := BuildIMPayload(ChannelDingtalk, "标题", "md", "text")
	if err != nil {
		t.Fatalf("dingtalk 不应出错, got %v", err)
	}
	wantDing, _ := DingtalkMarkdownPayload("标题", "md")
	if !bytes.Equal(ding, wantDing) {
		t.Fatalf("dingtalk 分发不符:\n want %s\n got  %s", wantDing, ding)
	}

	feishu, err := BuildIMPayload(ChannelFeishu, "t", "md", "text")
	if err != nil {
		t.Fatalf("feishu 不应出错, got %v", err)
	}
	wantFeishu, _ := FeishuTextPayload("text")
	if !bytes.Equal(feishu, wantFeishu) {
		t.Fatalf("feishu 分发不符:\n want %s\n got  %s", wantFeishu, feishu)
	}
}

func TestBuildIMPayload_UnknownChannel(t *testing.T) {
	cases := []struct {
		name    string
		channel string
	}{
		{"空渠道名", ""},
		{"未知渠道", "slack"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := BuildIMPayload(tc.channel, "t", "md", "text")
			if raw != nil {
				t.Fatalf("失败时不得产出载荷字节, got %s", raw)
			}
			if !errors.Is(err, ErrUnknownIMChannel) {
				t.Fatalf("未知渠道应包 ErrUnknownIMChannel, got %v", err)
			}
		})
	}
}
