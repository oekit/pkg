package sse

import (
	"net/http"
	"time"
)

// ReplayFunc 游标窗口取数：返回所有 ID 大于 after 的事件，**按 ID 升序**
// （契约见包文档「订阅先于回放」）。取数是宿主 DB，本包不带存储假设。
type ReplayFunc func(after uint64) []Event

// Responder SSE 游标回放应答器：一条连接承担「响应头四件套 → 订阅先于
// 回放 → 按事件 ID 幂等跳帧 → 心跳注释帧保活 → 断开即退订」全套语义，
// 契约细节见包文档。
type Responder struct {
	src       Source
	replay    ReplayFunc
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

// NewResponder 建立应答器。src 为实时面（[Hub] 或宿主自有 [Source] 实现），
// replay 为取数面；二者 nil panic（构造期 fail-fast，仓规总则①）。
func NewResponder(src Source, replay ReplayFunc, opts ...ResponderOption) *Responder {
	if src == nil {
		panic("sse: NewResponder 缺实时面 Source（fail-fast，仓规总则①）")
	}
	if replay == nil {
		panic("sse: NewResponder 缺游标回放 ReplayFunc（fail-fast，仓规总则①）")
	}
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
// 重连自动携带；缺省 0 = 全量回放；非法值 400）。
func (r *Responder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	cursor, ok := parseCursor(req.Header.Get("Last-Event-ID"))
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
	// 重叠部分靠下面的幂等跳帧去重。契约细节见包文档。
	frames, cancel := r.src.Subscribe()
	defer cancel()

	last := cursor
	for _, e := range r.replay(last) {
		if e.ID <= last {
			continue // 回放窗口内的乱序/重叠帧跳过（宿主契约的防御性兜底）
		}
		if !writeFrame(w, fl, e.frame()) {
			return
		}
		last = e.ID
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
			if e.ID <= last {
				continue // 订阅/回放重叠去重：同 ID 只写一次
			}
			if !writeFrame(w, fl, e.frame()) {
				return
			}
			last = e.ID
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
