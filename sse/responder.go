package sse

import (
	"context"
	"net/http"
	"time"
)

// ReplayFunc 游标窗口取数：返回所有 ID 大于 after 的事件，**按 ID 升序**
// （契约见包文档「订阅先于回放」）。取数是宿主 DB，本包不带存储假设。
//
// 本类型是无 ctx 的兼容形态；宿主需要感知客户端断开（回放查询可取消）
// 时用 [ReplayFuncCtx] + [NewResponderCtx]。
type ReplayFunc func(after uint64) []Event

// ReplayFuncCtx 游标窗口取数的 ctx 感知形态（#564）：语义与 [ReplayFunc]
// 一致，额外收到调用方请求的 ctx——客户端断开/请求取消时 ctx.Done()
// 关闭，宿主可据此中止昂贵的取数。
//
// 典型实现：把 ctx 透传给 DB/SDK 查询（`db.EventsAfter(ctx, after)`），
// 或返回前先查 ctx.Err()。回放本身仍在 ServeHTTP 的 goroutine 内同步
// 执行，本包不替宿主并发化。
type ReplayFuncCtx func(ctx context.Context, after uint64) []Event

// Responder SSE 游标回放应答器：一条连接承担「响应头四件套 → 订阅先于
// 回放 → 按事件 ID 幂等跳帧 → 心跳注释帧保活 → 断开即退订」全套语义，
// 契约细节见包文档。
type Responder struct {
	src       Source
	replay    ReplayFuncCtx
	heartbeat time.Duration
	comment   string
}

// ResponderOption Responder 的构造选项（仓规禁裸 Option）。
type ResponderOption func(*Responder)

// WithHeartbeat 覆盖心跳间隔（缺省 [DefaultHeartbeat] 15s）。非正数
// panic（构造期 fail-fast，仓规总则①）。
func WithHeartbeat(d time.Duration) ResponderOption {
	return func(r *Responder) {
		if d <= 0 {
			panic("sse: WithHeartbeat 间隔必须为正数（fail-fast，仓规总则①）")
		}
		r.heartbeat = d
	}
}

// WithHeartbeatComment 覆盖心跳注释帧文案（缺省 `: keepalive`）。必须是
// 冒号开头的单行注释——SSE 里冒号开头才是客户端忽略的注释帧，非注释
// 形态会被 EventSource 当事件解析。非法值 panic（构造期 fail-fast）。
func WithHeartbeatComment(comment string) ResponderOption {
	return func(r *Responder) {
		if !validComment(comment) {
			panic("sse: WithHeartbeatComment 必须是冒号开头且不含换行的注释（fail-fast，仓规总则①）")
		}
		r.comment = comment
	}
}

// NewResponder 建立应答器（无 ctx 的兼容入口）。src 为实时面（[Hub] 或
// 宿主自有 [Source] 实现），replay 为取数面；二者 nil panic（构造期
// fail-fast，仓规总则①）。
//
// 需要客户端断开可取消回放时用 [NewResponderCtx]。
func NewResponder(src Source, replay ReplayFunc, opts ...ResponderOption) *Responder {
	if src == nil {
		panic("sse: NewResponder 缺实时面 Source（fail-fast，仓规总则①）")
	}
	if replay == nil {
		panic("sse: NewResponder 缺游标回放 ReplayFunc（fail-fast，仓规总则①）")
	}
	return newResponder(src, func(_ context.Context, after uint64) []Event {
		return replay(after)
	}, opts...)
}

// NewResponderCtx 建立 ctx 感知回放的应答器（#564）：replay 收到本次
// HTTP 请求的 ctx（[http.Request.Context]），客户端断开/请求取消时
// ctx.Done() 关闭——回放查询可随连接生命周期中止，不必空跑完整个窗口。
//
// src/replay 均 nil panic（构造期 fail-fast，仓规总则①）。
func NewResponderCtx(src Source, replay ReplayFuncCtx, opts ...ResponderOption) *Responder {
	if src == nil {
		panic("sse: NewResponderCtx 缺实时面 Source（fail-fast，仓规总则①）")
	}
	if replay == nil {
		panic("sse: NewResponderCtx 缺游标回放 ReplayFuncCtx（fail-fast，仓规总则①）")
	}
	return newResponder(src, replay, opts...)
}

// newResponder 是两个导出构造器的共同装配体（replay 已非 nil 且已归一为
// ctx 形态）。
func newResponder(src Source, replay ReplayFuncCtx, opts ...ResponderOption) *Responder {
	r := &Responder{
		src:       src,
		replay:    replay,
		heartbeat: DefaultHeartbeat,
		comment:   defaultHeartbeatComment,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// ServeHTTP 应答一条 SSE 流。游标取 `Last-Event-ID` 请求头（EventSource
// 重连自动携带；缺省 0 = 全量回放；非法值 400）。游标判定与回放/去重
// 内核分别由 [ParseCursor] / [ReplaySinceCtx] 提供——其它传输（Hertz/
// WebSocket 网关等）复用同一导出面即可保持与本文档逐字一致的语义。
func (r *Responder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	cursor, ok := ParseCursor(req.Header.Get("Last-Event-ID"))
	if !ok {
		http.Error(w, "Last-Event-ID 必须是无符号整数", http.StatusBadRequest)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "当前 ResponseWriter 不支持流式 flush", http.StatusInternalServerError)
		return
	}

	setHeaders(w)
	fl.Flush() // 头先出：客户端 EventSource 尽早进入 OPEN 态

	// 订阅先于回放（核心不变量）：先拿实时面通道，再查窗口——缝隙归零，
	// 重叠部分靠 [Cursor.Accept] 的幂等跳帧去重。契约细节见包文档。
	frames, cancel := r.src.Subscribe()
	defer cancel()

	// 回放/去重内核与 Hertz 等其它传输共用同一导出面（[ReplaySinceCtx]），
	// 本方法只剩 net/http 的写帧与心跳适配。req.Context() 交给回放：
	// 客户端断开时宿主取数可随连接取消（#564）。
	backlog, cur := ReplaySinceCtx(req.Context(), cursor, r.replay)
	for _, e := range backlog {
		if !writeFrame(w, fl, e.frame()) {
			return
		}
	}

	// 心跳注释帧：空闲保活，防中间层掐流（语义见包文档）。
	hb := []byte(r.comment + "\n\n")
	ticker := time.NewTicker(r.heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-req.Context().Done():
			return // 客户端断开：defer cancel 退订，Hub 不留死订阅
		case <-ticker.C:
			if !writeFrame(w, fl, hb) {
				return
			}
		case e, ok := <-frames:
			if !ok {
				return // Hub Close 或掉队断流：客户端带游标重连补窗
			}
			if !cur.Accept(e) {
				continue // 订阅/回放重叠去重：同 ID 只写一次
			}
			if !writeFrame(w, fl, e.frame()) {
				return
			}
		}
	}
}

// validComment 心跳注释合法性：冒号开头、单行（SSE 注释帧形态约束）。
func validComment(s string) bool {
	return len(s) > 0 && s[0] == ':' && !containsNewline(s)
}

func containsNewline(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			return true
		}
	}
	return false
}
