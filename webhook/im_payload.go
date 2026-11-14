package webhook

// IM 机器人载荷构造器（#485）：企业微信群机器人 / 钉钉自定义机器人 /
// 飞书自定义机器人的出向 JSON 载荷单源。msgtype/markdown/text 三家的字段
// 约定与 platforms.go 的 header 名同属「平台历史约定」——「谁在用谁」的
// 消歧知识写死在具名构造器里，下游不再各自拼 map[string]any（平台字段
// 漂移由本包统一跟进，#485 (c)）。
//
// 边界哲学与发侧签名器一致（#485 明确：只做载荷构造，发送留给调用方）：
// 纯函数、零网络、零重试——HTTP 发送、鉴权（钉钉的 secret 签名挂在 URL
// query、飞书的签名在 payload 外层拼接，两者都属「发送面」，随宿主的
// webhook 客户端走）、超长截断都不在本包。长度上限（企微 markdown
// content ~4096 字节等）随平台漂移且本包不做有损截断——静默截断会把
// 「没发全」伪装成「发完了」，调用方按自己的审计需求显式决策。
//
// 变体范围（#485 (a) 原样收编）：企微 markdown、钉钉 markdown、飞书 text
// 三种形态；text/markdown 交叉变体与卡片结构（飞书 interactive card 等
// 深水区）按需再进——公开稳定的平台约定才进单源。

import (
	"encoding/json"
	"errors"
	"fmt"
)

// IM 载荷错误哨兵（errors.Is 判定）。
var (
	// ErrUnknownIMChannel BuildIMPayload 查无此渠道（含空渠道名）——
	// 渠道路由是配置问题，与载荷内容问题分属两类。
	ErrUnknownIMChannel = errors.New("webhook: 未知 IM 渠道")
	// ErrEmptyIMPayload 必填内容为空串——空内容通知没有意义，构造期
	// 拒绝（fail-fast，仓规总则①；标题不受此限，见各构造器说明）。
	ErrEmptyIMPayload = errors.New("webhook: IM 载荷内容为空")
)

// IM 渠道名常量（配置驱动的下游按名路由；与宿主被收编的策略表键名同源）。
const (
	ChannelWecom    = "wecom"    // 企业微信群机器人
	ChannelDingtalk = "dingtalk" // 钉钉自定义机器人
	ChannelFeishu   = "feishu"   // 飞书自定义机器人
)

// 企微 markdown 载荷线格式（字段序即字节序，契约测试按此钉住）。
type wecomMarkdownPayload struct {
	MsgType  string `json:"msgtype"`
	Markdown struct {
		Content string `json:"content"`
	} `json:"markdown"`
}

// 钉钉 markdown 载荷线格式。
type dingtalkMarkdownPayload struct {
	MsgType  string `json:"msgtype"`
	Markdown struct {
		Title string `json:"title"`
		Text  string `json:"text"`
	} `json:"markdown"`
}

// 飞书 text 载荷线格式（注意 msg_type 是下划线——与企微/钉钉的 msgtype
// 是两种历史拼写，正是本单源要消歧的「谁在用谁」）。
type feishuTextPayload struct {
	MsgType string `json:"msg_type"`
	Content struct {
		Text string `json:"text"`
	} `json:"content"`
}

// WecomMarkdownPayload 企业微信群机器人 markdown 载荷：
//
//	{"msgtype":"markdown","markdown":{"content":"<md>"}}
//
// md 为空串返回包 ErrEmptyIMPayload 的 error。title 形态缺位是企微平台
// 约定——markdown 卡片没有标题字段，会话列表预览取 content 首行，需要
// 标题的调用方自行放进 md 开头。
func WecomMarkdownPayload(md string) ([]byte, error) {
	if md == "" {
		return nil, fmt.Errorf("%w: 企业微信 markdown content", ErrEmptyIMPayload)
	}
	var p wecomMarkdownPayload
	p.MsgType = "markdown"
	p.Markdown.Content = md
	b, err := json.Marshal(&p)
	if err != nil {
		return nil, fmt.Errorf("webhook: 企业微信载荷序列化失败: %w", err)
	}
	return b, nil
}

// DingtalkMarkdownPayload 钉钉自定义机器人 markdown 载荷：
//
//	{"msgtype":"markdown","markdown":{"title":"<title>","text":"<md>"}}
//
// title 是会话列表/通知预览的**宿主内容**（卡片文案属宿主，构造器不写
// 死——#485 (e) 的硬要求），空串按平台约定放行（钉钉收空标题、预览显示
// 空），md 为空串返回包 ErrEmptyIMPayload 的 error。
func DingtalkMarkdownPayload(title, md string) ([]byte, error) {
	if md == "" {
		return nil, fmt.Errorf("%w: 钉钉 markdown text", ErrEmptyIMPayload)
	}
	var p dingtalkMarkdownPayload
	p.MsgType = "markdown"
	p.Markdown.Title = title
	p.Markdown.Text = md
	b, err := json.Marshal(&p)
	if err != nil {
		return nil, fmt.Errorf("webhook: 钉钉载荷序列化失败: %w", err)
	}
	return b, nil
}

// FeishuTextPayload 飞书自定义机器人 text 载荷：
//
//	{"msg_type":"text","content":{"text":"<text>"}}
//
// text 为空串返回包 ErrEmptyIMPayload 的 error。飞书的 markdown 走
// interactive card 深水区（卡片结构随版本漂移，无稳定公开契约），首版
// 只钉 text 形态（#485 (a) 原样）。
func FeishuTextPayload(text string) ([]byte, error) {
	if text == "" {
		return nil, fmt.Errorf("%w: 飞书 text content", ErrEmptyIMPayload)
	}
	var p feishuTextPayload
	p.MsgType = "text"
	p.Content.Text = text
	b, err := json.Marshal(&p)
	if err != nil {
		return nil, fmt.Errorf("webhook: 飞书载荷序列化失败: %w", err)
	}
	return b, nil
}

// BuildIMPayload 按渠道名分发到对应构造器——配置驱动（渠道名来自配置
// 而非代码分支）的下游用一次调用替代各自的策略表（#485 (d)：宿主删
// NotifyPayloadBuilders 表与渠道分支）。
//
// 参数语义随渠道而异（与宿主被收编的策略表同口径）：wecom 只用 md；
// dingtalk 用 title+md；feishu 只用 text——非该渠道用的参数原样忽略，
// 调用方不必按渠道预先拆参数。渠道名未知返回包 ErrUnknownIMChannel 的
// error（配置错配在构造期暴露，不静默产出「猜一个」的载荷）。
func BuildIMPayload(channel, title, md, text string) ([]byte, error) {
	switch channel {
	case ChannelWecom:
		return WecomMarkdownPayload(md)
	case ChannelDingtalk:
		return DingtalkMarkdownPayload(title, md)
	case ChannelFeishu:
		return FeishuTextPayload(text)
	case "":
		return nil, fmt.Errorf("%w: 渠道名为空串", ErrUnknownIMChannel)
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownIMChannel, channel)
}
