package sse

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultHeartbeat 心跳注释帧缺省间隔（15s，理由见包文档「心跳注释帧」）。
const DefaultHeartbeat = 15 * time.Second

// defaultHeartbeatComment 心跳注释帧缺省文案（SSE 注释行：冒号开头）。
const defaultHeartbeatComment = ": keepalive"

// Event 单个流事件。ID 是宿主分配的单调递增游标：既是 SSE `id:` 帧
// （EventSource 重连时自动回传为 Last-Event-ID 头），也是「订阅先于
// 回放」的重叠去重键——见包文档「订阅先于回放」。
type Event struct {
	ID   uint64
	Type string // SSE `event:` 帧；空则不发该帧
	Data string // SSE `data:` 帧；含 \n 时按行拆成多条 data 行
}

// frame 按 SSE 文本协议序列化：id/event/data 各一行、数据多行拆分、
// 空行结尾（事件分帧符）。
func (e Event) frame() []byte {
	b := make([]byte, 0, 32+len(e.Data))
	b = append(b, "id: "...)
	b = strconv.AppendUint(b, e.ID, 10)
	b = append(b, '\n')
	if e.Type != "" {
		b = append(b, "event: "...)
		b = append(b, e.Type...)
		b = append(b, '\n')
	}
	for _, line := range strings.Split(e.Data, "\n") {
		b = append(b, "data: "...)
		b = append(b, line...)
		b = append(b, '\n')
	}
	return append(b, '\n')
}

// setHeaders SSE 应答头四件套：流媒体类型 + 禁缓存 + 保活 + 关闭 Nginx
// 类中间层的响应缓冲（X-Accel-Buffering: no）——四件齐备才能防止中间层
// 掐流/攒包，少一件都会在某个部署形态下断流。
func setHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
}

// writeFrame 写一帧并立即 flush。写失败（客户端已断开）返回 false，
// 调用方随即收尾——SSE 长循环里「写失败即退出」是断开清理的触发点。
func writeFrame(w http.ResponseWriter, fl http.Flusher, frame []byte) bool {
	// 帧是 text/event-stream 协议数据（EventSource 解析，不进 HTML 渲染
	// 上下文），G705 的 XSS 模型不适用。
	if _, err := w.Write(frame); err != nil { //nolint:gosec // G705 误报：text/event-stream 非 HTML 渲染上下文
		return false
	}
	fl.Flush()
	return true
}

// ParseCursor 解析 Last-Event-ID 游标：空串（含全空白）= (0, true)，
// 表示客户端没有游标、从头全量回放；合法 ASCII 十进制无符号 64 位整数
// = (id, true)，两侧空白容忍；其它（负数、正号、小数、十六进制、溢出、
// 非 ASCII 数字）= (0, false)，调用方按 400 拒绝（fail-closed）。
//
// 语义与历史 Responder 内部的 parseCursor 逐字一致。net/http [Responder]
// 与 Hertz/WebSocket 等其它传输的响应端必须共用本函数——非法游标的判定
// 口径只有这一份，适配层不得另写一套。
func ParseCursor(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, true
	}
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}
