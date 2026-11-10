package webhook

// 事件信封（#328 可选层）：事件类型 + 幂等 ID + 时间戳的标准外层。签名保
// 的是「报文没被改」，信封补的是「报文是什么、哪一次投递」——消费端在验
// 签之前就能按 event 路由、按 id 去重（重复投递在解包/落库之前丢弃）。
//
// 用法（与签名器正交组合）：
//
//	env := webhook.NewEventEnvelope("sync_run", id, payload) // id 用 NewEventID
//	body, _ := env.Bytes()
//	hdr, _ := signer.Sign(body) // 签的是信封字节，不是裸 payload
//
// 信封是推荐形态而非强制：已有的顶层事件字段约定不必迁——标准化的收益在
// 「同一消费端接多生产方」时才兑现。

import (
	"encoding/json"
	"fmt"
	"time"
)

// EventEnvelope 事件信封：Data 内嵌原始 payload（json.RawMessage，字节原
// 样嵌入不重排序）。字段导出——调用方可直接构造（时间戳可注入固定值做
// 确定性测试），NewEventEnvelope 是 fail-fast 快捷口。
type EventEnvelope struct {
	Event     string          `json:"event"`
	ID        string          `json:"id"`
	Timestamp int64           `json:"timestamp"` // Unix 秒
	Data      json.RawMessage `json:"data"`
}

// NewEventEnvelope 构造事件信封（Timestamp 取当前时刻）。
//
// event/id 空串、payload 空、payload 不是合法 JSON 均为构造期配置错误，
// panic fail-fast（信封的 event/id 是消费端路由与幂等去重的键，缺了等于
// 没信封；payload 非法 JSON 会在消费端炸出难排查的解码错，不如产出期就死）。
func NewEventEnvelope(event, id string, payload []byte) *EventEnvelope {
	if event == "" {
		panic("webhook: NewEventEnvelope 事件类型为空串（fail-fast，仓规总则①）")
	}
	if id == "" {
		panic("webhook: NewEventEnvelope 幂等 ID 为空串（fail-fast，仓规总则①）")
	}
	if len(payload) == 0 {
		panic("webhook: NewEventEnvelope payload 为空（fail-fast，仓规总则①）")
	}
	if !json.Valid(payload) {
		panic(fmt.Sprintf("webhook: NewEventEnvelope payload 不是合法 JSON（fail-fast，仓规总则①）: %.64s", payload))
	}
	return &EventEnvelope{
		Event:     event,
		ID:        id,
		Timestamp: time.Now().Unix(),
		Data:      json.RawMessage(append([]byte(nil), payload...)),
	}
}

// Bytes 信封的线格式字节（json.Marshal）——签名器与 HTTP 发送吃的就是它。
func (e *EventEnvelope) Bytes() ([]byte, error) {
	return json.Marshal(e)
}
