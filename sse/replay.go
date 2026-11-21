package sse

import (
	"context"

	"github.com/oekit/pkg/contextx"
)

// Cursor 事件去重游标：记录已接受的最大 Event.ID。回放窗口内乱序/重叠、
// 订阅与回放重叠、同 ID 重复广播，统一用 [Cursor.Accept] 跳帧。零值等价
// 于 after=0（无游标 / 全量回放起点）。
//
// 事件 ID 必须从 1 开始单调递增分配（#564）：0 是「无游标」哨兵，不是
// 合法事件 ID——Accept 恒拒绝 ID=0，回放与实时面都不会写出它。
//
// Cursor 不是并发安全的：每条流持有自己的副本，只在响应端单 goroutine
// 内推进。
type Cursor struct {
	last uint64
}

// NewCursor 以游标值建立去重游标（通常来自 [ParseCursor]，0 = 从头）。
func NewCursor(after uint64) Cursor { return Cursor{last: after} }

// Last 已接受的最大事件 ID：可作为 [ReplayFunc]/[ReplayFuncCtx] 的 after，
// 也是 SSE id 帧/断线重连游标的来源。
func (c Cursor) Last() uint64 { return c.last }

// Accept 判断事件是否应写入传输层并推进游标：只有 ID 严格大于已见最大
// ID 才返回 true（回放与实时面共用同一游标，重叠帧只写一次）。返回
// false 时游标不变。
//
// ID=0 恒不接受（#564：0 是「无游标」哨兵，宿主事件 ID 从 1 起——
// after=0 时若不显式拒绝，ID=0 与「无游标」的歧义会让首个事件永久静默
// 丢失，且重连仍丢）。
func (c *Cursor) Accept(e Event) bool {
	if e.ID == 0 || e.ID <= c.last {
		return false
	}
	c.last = e.ID
	return true
}

// ReplaySince 取游标窗口并做内核级去重：调用 replay(after)，按 ID 严格
// 递增过滤（跳过 ID=0 的哨兵帧、<= after 的越界帧、乱序帧与同 ID 重复
// 帧），返回可写的回放事件与推进后的游标。返回事件保持宿主给出的相对
// 顺序；宿主契约仍要求 [ReplayFunc] 按 ID 升序返回（本函数只防守乱序，
// 不替宿主排序）。
//
// 装配纪律：**必须先建立 [Source.Subscribe]，再调用本函数**——否则
// 「回放结束」到「订阅生效」之间的实时帧会永久丢失（见包文档「订阅先于
// 回放」）。订阅侧实时帧用返回的 [Cursor.Accept] 去重。
//
// 本函数是无 ctx 的兼容入口；新代码（尤其是 net/http 请求内）用
// [ReplaySinceCtx]，客户端断开时回放查询可被取消。
//
// replay 为 nil panic（构造期 fail-fast，仓规总则①）。
func ReplaySince(after uint64, replay ReplayFunc) ([]Event, Cursor) {
	if replay == nil {
		panic("sse: ReplaySince 缺游标回放 ReplayFunc（fail-fast，仓规总则①）")
	}
	return replaySince(context.Background(), after, func(_ context.Context, after uint64) []Event {
		return replay(after)
	})
}

// ReplaySinceCtx 是 [ReplaySince] 的 ctx 感知形态（#564）：replay 收到
// 调用方 ctx，客户端断开/请求取消时 ctx.Done() 关闭，宿主可据此中止
// 昂贵的 DB 查询。
//
// ctx 已取消时直接返回空窗口（游标停在 after），不再调用 replay——
// 调用方已经走了，查出来的结果也没人写。ctx 传 nil 视同
// context.Background()（仓内 nil-ctx 兜底总则）。
//
// 去重口径与 ReplaySince 完全一致：ID=0 的哨兵帧、<= after 的越界帧、
// 乱序帧与同 ID 重复帧全部跳过。
func ReplaySinceCtx(ctx context.Context, after uint64, replay ReplayFuncCtx) ([]Event, Cursor) {
	if replay == nil {
		panic("sse: ReplaySinceCtx 缺游标回放 ReplayFuncCtx（fail-fast，仓规总则①）")
	}
	return replaySince(contextx.OrBackground(ctx), after, replay)
}

// replaySince 是两个导出入口的共同内核。replay 非 nil（由入口校验）。
func replaySince(ctx context.Context, after uint64, replay ReplayFuncCtx) ([]Event, Cursor) {
	cur := NewCursor(after)
	if ctx.Err() != nil {
		return nil, cur
	}
	var events []Event
	for _, e := range replay(ctx, after) {
		if e.ID == 0 {
			continue // 0 是「无游标」哨兵，不是事件 ID（#564）
		}
		if cur.Accept(e) {
			events = append(events, e)
		}
	}
	return events, cur
}
