package httpjson

// Retry-After 捕获与消费：配额型端点在 429 响应里携带 Retry-After（秒数或
// HTTP-date），服务端建议比本地指数退避更准——限流窗口还剩多久只有端点知道。
//
// 管线三段：
//   1. capture：RetryAfterTransport 包装 RoundTripper，429 响应读取 Retry-After
//      写入请求 ctx 中的 sink（透明；非 429 原样透传）；
//   2. 安装：WithRetryAfterSink 在重试环上游注入 sink（消费方与传输层以此相遇）；
//   3. 消费：RetryAfterFrom 读取建议，重试环按 SelectRetryDelay 的钳制规则
//      取舍——建议 >0 且（≤5min 或 < 本地曲线值）才取代本地退避。
//
// 隐私与内存：sink 只存一个时长；未安装 sink 的请求零开销。
// 收编自 agentkit llm/retryafter.go（v0.10.41 起单源在此）。

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MaxServerRetryAfter 服务端退避建议的可信上限：超过 5 分钟的建议不直接采信
// （长时间限流应交由上层 failover/切换处置，而非原地等）。
const MaxServerRetryAfter = 5 * time.Minute

type retryAfterHolder struct {
	mu sync.Mutex
	d  time.Duration
}

func (h *retryAfterHolder) set(d time.Duration) {
	h.mu.Lock()
	h.d = d
	h.mu.Unlock()
}

func (h *retryAfterHolder) get() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.d
}

type retryAfterKey struct{}

// WithRetryAfterSink 在 ctx 注入 sink（重试环上游调用一次；传输层据此回写）。
func WithRetryAfterSink(ctx context.Context) context.Context {
	return context.WithValue(ctx, retryAfterKey{}, &retryAfterHolder{})
}

// RetryAfterFrom 读取最近一次 429 的服务端建议（0 = 无可表示的建议——未捕获/
// 解析失败/数值荒谬；超可信上限的建议原样返回，采信与否由 SelectRetryDelay 判定，
// #363）。重试环在计算退避时调用。
// sink 不随尝试清除：仅对本次失败确为限流的场景采信（陈旧建议不得套用到
// 非限流错误上），门控归调用方的重试策略。跨尝试复用同一 sink 的自建重试
// 环应在每次物理调用前 ClearRetryAfter（#496）。
func RetryAfterFrom(ctx context.Context) time.Duration {
	if h, ok := ctx.Value(retryAfterKey{}).(*retryAfterHolder); ok {
		return h.get()
	}
	return 0
}

// ClearRetryAfter 作废 sink 中已捕获的建议并返回作废前的值（诊断用；未安装
// sink 返回 0）。
//
// 给第三方自建重试环的按尝试清除原语（#496）：sink 此前只写不清——跨尝试
// 复用同一 sink 时，某一轮 429 不带 Retry-After 头，RetryAfterFrom 仍返回
// 上一轮的旧窗口，陈旧建议被当成「本轮也是限流」的证据采信。正确姿势是
// **每次物理调用前 Clear**（或读完建议后立即 Clear）：本轮确为限流且带
// 响应头时传输层会重新写入；不带则读到 0，退避回落本地曲线。
// agentkit/llm 的重试环（每轮注入全新 sink）不受影响；本 API 是同一纪律
// 在「复用 sink」形态下的替身。
func ClearRetryAfter(ctx context.Context) time.Duration {
	if h, ok := ctx.Value(retryAfterKey{}).(*retryAfterHolder); ok {
		h.mu.Lock()
		defer h.mu.Unlock()
		d := h.d
		h.d = 0
		return d
	}
	return 0
}

// SelectRetryDelay 退避取舍（「合理性钳制」）：服务端建议 >0 且
// （≤MaxServerRetryAfter 或 < 本地曲线值）→ 采信建议；否则本地曲线。
func SelectRetryDelay(local, hint time.Duration) time.Duration {
	if hint <= 0 {
		return local
	}
	if hint <= MaxServerRetryAfter || hint < local {
		return hint
	}
	return local
}

// RetryAfterTransport 透明捕获 429 的 Retry-After 头。响应体不做任何改写——
// 上层错误构造不受影响。Base 为 nil 时用 http.DefaultTransport。
type RetryAfterTransport struct {
	Base http.RoundTripper
}

func (t RetryAfterTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err == nil && resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		if h, ok := req.Context().Value(retryAfterKey{}).(*retryAfterHolder); ok {
			if d := parseRetryAfter(resp.Header.Get("Retry-After")); d > 0 {
				h.set(d)
			}
		}
	}
	return resp, err
}

// parseRetryAfter 解析 Retry-After：延迟秒数（整数）或 HTTP-date。无法解析
// 或数值荒谬（乘进 time.Duration 必然溢出）返回 0。
//
// 秒数形态在乘进 time.Duration 之前先封死溢出入口：secs 来自 Atoi 无上界，
// 直接乘 time.Second 会让 int64 纳秒回绕——超大建议可能溢出成毫秒级小正值，
// 恰好穿透 SelectRetryDelay 的「≤5min 才采信」闸（#343：建议「等 5.8 亿年」
// 被算成「等 0.29 秒」反被采信）。
//
// 但**可信上界的判定不在这里**（#363）：MaxServerRetryAfter 的声明是「超过
// 5 分钟的建议不直接采信」、SelectRetryDelay 的规则是「≤5min 或 < 本地曲线
// 才采信，否则本地曲线」——两者的前提都是「超钳制的建议值存在、在取舍层
// 被降级」。parse 层若把超钳制值折叠成 0，秒数形态的超钳制建议永远到不了
// 取舍层（SelectRetryDelay 超钳制分支不可达），且下游无法区分「无建议」与
// 「建议超钳制」（oagentkit/agentrun 的 Retry-After 门控实测被此盲掉）。
// 故这里只拦「无法表示的荒谬值」，可表示的值一律原样捕获，采信与否交回
// SelectRetryDelay（捕获不改写、取舍才钳制）。
const maxSafeParseSecs = 9223372036 // int64 纳秒上界 9223372036854775807 ÷ 1e9 向下取整；更大的秒数必然回绕

func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		// 溢出防护（#343）：只拦乘 time.Second 必回绕的荒谬值；
		// 可信上界（MaxServerRetryAfter）由 SelectRetryDelay 在取舍层执行。
		if secs <= 0 || secs > maxSafeParseSecs {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if at, err := http.ParseTime(v); err == nil {
		d := time.Until(at)
		if d > 0 {
			return d
		}
	}
	return 0
}
