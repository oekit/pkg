package sse

// Cursor 事件去重游标：记录已接受的最大 Event.ID。回放窗口内乱序/重叠、
// 订阅与回放重叠、同 ID 重复广播，统一用 [Cursor.Accept] 跳帧。零值等价
// 于 after=0（全量回放起点）。
//
// Cursor 不是并发安全的：每条流持有自己的副本，只在响应端单 goroutine
// 内推进。
type Cursor struct {
	last uint64
}

// NewCursor 以游标值建立去重游标（通常来自 [ParseCursor]，0 = 从头）。
func NewCursor(after uint64) Cursor { return Cursor{last: after} }

// Last 已接受的最大事件 ID：可作为 [ReplayFunc] 的 after，也是 SSE id
// 帧/断线重连游标的来源。
func (c Cursor) Last() uint64 { return c.last }

// Accept 判断事件是否应写入传输层并推进游标：只有 ID 严格大于已见最大
// ID 才返回 true（回放与实时面共用同一游标，重叠帧只写一次）。返回
// false 时游标不变。
func (c *Cursor) Accept(e Event) bool {
	if e.ID <= c.last {
		return false
	}
	c.last = e.ID
	return true
}

// ReplaySince 取游标窗口并做内核级去重：调用 replay(after)，按 ID 严格
// 递增过滤（跳过 <= after 的越界帧、乱序帧与同 ID 重复帧），返回可写的
// 回放事件与推进后的游标。返回事件保持宿主给出的相对顺序；宿主契约仍
// 要求 [ReplayFunc] 按 ID 升序返回（本函数只防守乱序，不替宿主排序）。
//
// 装配纪律：**必须先建立 [Source.Subscribe]，再调用本函数**——否则
// 「回放结束」到「订阅生效」之间的实时帧会永久丢失（见包文档「订阅先于
// 回放」）。订阅侧实时帧用返回的 [Cursor.Accept] 去重。
//
// replay 为 nil panic（构造期 fail-fast，仓规总则①）。
func ReplaySince(after uint64, replay ReplayFunc) ([]Event, Cursor) {
	if replay == nil {
		panic("sse: ReplaySince 缺游标回放 ReplayFunc（fail-fast，仓规总则①）")
	}
	cur := NewCursor(after)
	var events []Event
	for _, e := range replay(after) {
		if cur.Accept(e) {
			events = append(events, e)
		}
	}
	return events, cur
}
