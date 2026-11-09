// Package httpjson stdlib-only 的 HTTP+JSON 调用纪律单源：ctx 感知构造请求 →
// 执行 → 限容读体（读错误不吞）→ 状态码检查（错误体 rune 安全截断，不腰斩
// UTF-8）→ JSON 解码（out 非 nil 时；out==nil 跳过解码，只判状态码），外加
// 可选的指数退避重试与服务端 Retry-After 建议捕获。
//
// 与 network/httpx 的分工（同名不同物）：那侧是 Hertz 底座的服务间调用基础
// 设施（服务发现/熔断/指标/追踪，编译期即拉 hertz+prometheus+gobreaker+otel
// 依赖链）；本包纯函数、调用方注入 *http.Client——被嵌入的 SDK 与外部 API
// 调用方（LLM 网关、第三方开放平台）复用，不把微服务全家桶拖进依赖树。
// 收编自 agentkit/httpx（v0.10.41 起单源在此，agentkit 侧消费本包）。
package httpjson

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/oekit/pkg/stringx"
)

// maxBody 响应体采集上限（异常服务端刷屏防内存放大）。
const maxBody = 64 << 20 // 64MB

// Request 一次 HTTP+JSON 调用的描述。
type Request struct {
	// Method HTTP 方法（空 = GET）。
	Method string
	// URL 完整地址（含 query）。
	URL string
	// Body 请求体（nil = 无请求体；JSON 调用方自行 Marshal）。
	Body []byte
	// Header 鉴权/Content-Type 等请求头注入钩子（可 nil）。
	Header func(h http.Header)

	// etagStore/etagKey/etagVary 由 WithETag/WithETagVary 选项注入（条件
	// 请求面，#269/#290），刻意不做成公开字段：条件请求是**行为注入**而非
	// 请求描述的一部分，且同一 Request 字面量可以配不同 key 复用。
	etagStore ETagStore
	etagKey   string
	etagVary  []string
}

// DoJSON 执行请求并把 2xx 响应体解码进 out。非 2xx 返回带状态码与响应体摘要
// （rune 安全截断到 200 字符）的错误；解码失败返回包装后的解析错误。
// 错误不带包前缀返回，调用方以自己的包前缀包装一次。
//
// out 为 nil 时**跳过响应体解码**：2xx 即返回 nil。这是 fire-and-forget 调用方
// 的语义——只关心请求送达与状态码，不关心响应体。此前无条件 json.Unmarshal(raw,
// nil) 恒定拿到 `json: Unmarshal(nil)` 的 InvalidUnmarshalError ⇒ **服务端已
// 返回 2xx 的成功请求被误报为失败**，真失败反而淹没在假失败里（#158）。
// 非 2xx 的判定与 out 无关：照样返回 StatusError（错误体摘要照旧带上）。
//
// out 非 nil 时行为不变：解码失败仍返回「响应解析失败」错误。
//
// opts 是单次请求的可选行为注入（RequestOption；变参零选项时调用面与旧
// 签名逐字节一致）。目前支持 WithETag / WithETagVary（条件请求：If-None-
// Match/304 重放为 200 形响应，#269；vary 头分区键与 no-store 门禁见
// #290/#288）——304 在函数内部消化，调用方拿到的 out/err 与 200 路径完全
// 同形，无需感知条件请求的存在。
func DoJSON(ctx context.Context, client *http.Client, req Request, out any, opts ...RequestOption) error {
	applyOptions(&req, opts)
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	// 条件请求只对 GET/HEAD 注入（写方法的 304 语义不同，见 etag.go 包文档
	// 第 3 条）；其余方法带选项执行时按无条件请求处理。
	conditional := req.etagStore != nil && req.etagKey != "" &&
		(method == http.MethodGet || method == http.MethodHead)

	// vary 头派生键（#290）：在**本次请求实际生效的请求头**上取 vary 头值
	// 并入键——凭据轮换/版本协商按头值自然分区。Header 钩子为此多调一次
	// （独立临时 http.Header，应无副作用）。
	if conditional && len(req.etagVary) > 0 {
		h := make(http.Header)
		if req.Header != nil {
			req.Header(h)
		}
		vals := make([]string, len(req.etagVary))
		for i, name := range req.etagVary {
			vals[i] = h.Get(name)
		}
		req.etagKey = CanonicalETagKey(method, req.etagKey, vals...)
	}

	var ifNoneMatch string
	if conditional {
		if etag, _, ok := req.etagStore.Get(req.etagKey); ok && etag != "" {
			ifNoneMatch = etag
		}
	}

	for attempt := 0; ; attempt++ {
		code, raw, etag, noStore, err := doSend(ctx, client, req, method, ifNoneMatch)
		if err != nil {
			return err
		}
		if code == http.StatusNotModified && ifNoneMatch != "" {
			// 304×驱逐竞态纪律（v0.100.2 教训，#31）：**回查存储**而非复用
			// 发请求前的旧值——条目已被并发驱逐（LRU/清理）时降级为无条件
			// 重发，绝不硬失败；条目还在则重放缓存体为 200 形响应。
			// 驱逐判定看 ok 而非体是否为空（#319）：空体条目是合法缓存
			// （204/HEAD 的 2xx 没有体、照常入库）——按 `cached != ""` 判驱逐
			// 会让空体条目永远走降级无条件重发、重发成功又把空体存回去，
			// 条件请求对这类端点永不生效、限流预算每轮翻倍。
			if _, cached, ok := req.etagStore.Get(req.etagKey); ok {
				return decodeOK([]byte(cached), out)
			}
			if attempt == 0 {
				ifNoneMatch = "" // 降级：重发不带条件头（至多一轮，不再循环）
				continue
			}
			// 防御分支：无条件请求仍被回 304（服务端/代理行为异常）——
			// 按 StatusError 如实上抛，不再重试。
			return &StatusError{Code: code, Body: string(raw)}
		}
		// 2xx 且带 ETag 头：回写存储（ETag 缺席则不写——下次请求无从注入）。
		// Cache-Control: no-store 不写（#288，RFC 9111 §5.2.2.5：任何 cache
		// MUST NOT store）——服务端要求每次新鲜取/禁止持有的端点，缓存条目
		// 的存活期不得超过服务端意图。
		if conditional && etag != "" && !noStore && code >= 200 && code < 300 {
			req.etagStore.Put(req.etagKey, etag, string(raw))
		}
		if code < 200 || code >= 300 {
			return &StatusError{Code: code, Body: string(raw)}
		}
		return decodeOK(raw, out)
	}
}

// applyOptions 依次套用选项（nil 选项跳过——不因某个选项为 nil 中断整批）。
func applyOptions(req *Request, opts []RequestOption) {
	for _, opt := range opts {
		if opt != nil {
			opt(req)
		}
	}
}

// doSend 构造并发送一次请求，返回 (状态码, 响应体, ETag 响应头, 响应是否
// 带 Cache-Control: no-store, 错误)。ifNoneMatch 非空时注入 If-None-Match。
// 响应体已读完并排空（连接可复用）。
func doSend(ctx context.Context, client *http.Client, req Request, method, ifNoneMatch string) (int, []byte, string, bool, error) {
	var body io.Reader
	if req.Body != nil {
		body = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, req.URL, body)
	if err != nil {
		return 0, nil, "", false, fmt.Errorf("构造请求失败: %w", err)
	}
	if req.Header != nil {
		req.Header(httpReq.Header)
	}
	if ifNoneMatch != "" {
		httpReq.Header.Set("If-None-Match", ifNoneMatch)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return 0, nil, "", false, fmt.Errorf("请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	noStore := cacheControlNoStore(resp.Header.Get("Cache-Control"))
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	// 排空剩余响应体，连接才能被复用（见 drainBody）。
	drainBody(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, "", noStore, fmt.Errorf("读取响应失败: %w", err)
	}
	return resp.StatusCode, raw, resp.Header.Get("ETag"), noStore, nil
}

// cacheControlNoStore 判定 Cache-Control 头是否含 no-store 指令（#288）。
// 头是多指令逗号分隔列表（"no-store, max-age=0"），按指令逐段比对，
// 大小写不敏感（RFC 9111 §5.2 指令名不区分大小写）。
func cacheControlNoStore(cc string) bool {
	for _, dir := range strings.Split(cc, ",") {
		if strings.EqualFold(strings.TrimSpace(dir), "no-store") {
			return true
		}
	}
	return false
}

// decodeOK 2xx 响应体解码进 out：out==nil 跳过（fire-and-forget，#158）、
// 空 body 跳过（204 语义上没有 body，#189）。304 重放路径与 200 路径共用
// 同一条解码线——重放语义「与 200 完全同形」由此保证（#269）。
func decodeOK(raw []byte, out any) error {
	if out == nil {
		return nil
	}
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("响应解析失败: %w", err)
	}
	return nil
}

// DoRaw 执行请求并返回**原始**状态码与响应体：不判 2xx、不解 JSON。
//
// 适用面（DoJSON 不适用的那些）：
//   - 调用方要**按状态码映射自己的语义**（如 404/409 → 各自的哨兵错误）；
//   - 调用方要把**原文**报文带进错误文案（DoJSON 的错误体是 rune 安全截断到
//     200 字符的**摘要**，不是原文）。
//
// limit 是读体上限（字节）：<=0 时用包级 maxBody。调用方应按自己的报文体量
// 给一个够用又防内存放大的值（如 8KB）。**超过 limit 的部分会被截断**，
// 且截断**在 RawResponse.Truncated 里如实标记**——调用方据此判断拿到的是
// 完整报文还是半截（截断对调用方不可见是最容易埋雷的形态：拿半截 JSON 去
// 解析会得到难以定位的解析错误，拿半截报文进错误文案会丢服务端说明）。
//
// 传输层错误（连不上/超时）返回 err，status/body 无意义；HTTP 层错误（任何状态码）
// 一律返回 nil err，由调用方按状态码自行判定。
func DoRaw(ctx context.Context, client *http.Client, req Request, limit int64) (RawResponse, error) {
	if limit <= 0 {
		limit = maxBody
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if req.Body != nil {
		body = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, req.URL, body)
	if err != nil {
		return RawResponse{}, fmt.Errorf("构造请求失败: %w", err)
	}
	if req.Header != nil {
		req.Header(httpReq.Header)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return RawResponse{}, fmt.Errorf("请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 读 limit+1 字节：多读的那一个字节用来判断报文是否被截断。
	readLimit := limit
	if limit < math.MaxInt64 {
		readLimit = limit + 1
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, readLimit))
	if err != nil {
		drainBody(resp.Body)
		return RawResponse{StatusCode: resp.StatusCode}, fmt.Errorf("读取响应失败: %w", err)
	}
	truncated := int64(len(raw)) > limit
	if truncated {
		raw = raw[:int(limit)] // len(raw) > limit ⇒ limit 必然落在 int 范围内
	}
	// 排空剩余响应体，连接才能被复用（见 drainBody）。
	drainBody(resp.Body)
	return RawResponse{StatusCode: resp.StatusCode, Body: raw, Truncated: truncated}, nil
}

// RawResponse 是 DoRaw 的原始响应结果。
type RawResponse struct {
	// StatusCode 是 HTTP 状态码（不判 2xx，由调用方按自己的语义映射）。
	StatusCode int
	// Body 是响应体原始字节（最多 limit 字节）。
	Body []byte
	// Truncated 报告 Body 是否因超过 limit 而被截断。
	//
	// 截断本身是刻意的（limit 防内存放大），但**必须可观测**——否则调用方
	// 会把「半截报文」当成完整报文用。
	Truncated bool
}

// drainLimit 是「读完后额外排空」的字节上限。
//
// Go 的 transport 只有在响应体被读完时才把连接放回空闲池；只读到 limit 就
// Close ⇒ 连接被丢弃 ⇒ 下次调用要重建 TCP（+ TLS 握手）。排空量有上限是
// 为了防「对端用超大响应体拖住本次调用」：排不干净就放弃复用、让连接被
// 关闭即可——不为了复用把时间与内存赌上去。
const drainLimit = 64 << 10

// drainBody 排空响应体剩余部分，让连接能被 transport 复用。
func drainBody(r io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(r, drainLimit))
}

// StatusError 非 2xx 响应错误：供重试判定等按状态码分类（errors.As 提取）。
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Code, stringx.TruncEllipsis(e.Body, 200))
}

// RetryConfig 一次重试策略（零值 = 仅执行一次，不重试）。
type RetryConfig struct {
	// MaxAttempts 总尝试次数（含首次；<1 视为 1）。
	MaxAttempts int
	// BaseDelay 首次重试延迟（<=0 取 200ms），按尝试轮次指数退避。
	BaseDelay time.Duration
	// MaxDelay 单次重试延迟上限（<=0 取 2s）。
	MaxDelay time.Duration
	// Jitter 随机抖动幅度（防惊群；<0 取 0）。
	Jitter time.Duration
	// Retryable 自定义可重试判定（nil = 默认：网络超时或 HTTP 5xx/429 可重试；
	// 4xx 参数错、响应解析失败不重试）。
	Retryable func(err error) bool
}

// DoJSONWithRetry 带重试的 HTTP+JSON 调用。
//
// reqFn 每次尝试重新构造 Request（请求体流不可重读，且调用方可能希望每轮
// 刷新签名头）；reqFn 自身报错视为调用方参数问题，立即返回不重试。
// 重试间指数退避 + Jitter 抖动；ctx 取消/超时立即返回 ctx.Err()。
// 配合 WithRetryAfterSink + RetryAfterTransport 可让 429 的服务端建议
// 参与退避取舍——仅当**本次失败**确为限流（StatusError.Code==429）才采信
// （#302 门控）：sink 不随尝试清除，一次 429 的建议不得套到后续 5xx/网络
// 超时的重试上。
//
// out 的语义与 DoJSON 完全一致：out==nil 时跳过响应体解码，2xx 即成功返回
// （fire-and-forget）；非 2xx 仍按可重试判定退避并最终返回 StatusError（#158）。
//
// opts 逐轮透传给 DoJSON（每次尝试都套用）——轮询消费方把 WithETag 与
// 重试组合：条件命中的 304 重放对重试层透明，限流预算敏感的场景两件套
// 各管各的（#269）。
func DoJSONWithRetry(ctx context.Context, client *http.Client, reqFn func() (Request, error), cfg RetryConfig, out any, opts ...RequestOption) error {
	attempts := cfg.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	base, maxDelay, jitter := cfg.BaseDelay, cfg.MaxDelay, cfg.Jitter
	if base <= 0 {
		base = 200 * time.Millisecond
	}
	if maxDelay <= 0 {
		maxDelay = 2 * time.Second
	}
	if jitter < 0 {
		jitter = 0
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		req, err := reqFn()
		if err != nil {
			return err
		}
		err = DoJSON(ctx, client, req, out, opts...)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == attempts || !retryable(err, cfg.Retryable) {
			break
		}
		delay := exponentialDelay(base, attempt, maxDelay)
		// Retry-After 采信门控（#302）：仅当**本次失败**确为限流（429）才采信
		// sink 里的服务端建议——sink 不随尝试清除，陈旧建议若套到后续 5xx/网络
		// 超时的重试上，会把本地指数曲线顶到分钟级（RetryAfterFrom 文档自述
		// 纪律：「陈旧建议不得套用到非限流错误上」；本重试环即该纪律的参考实现，
		// 不得自违）。
		var se *StatusError
		if errors.As(lastErr, &se) && se.Code == http.StatusTooManyRequests {
			if hint := RetryAfterFrom(ctx); hint > 0 {
				delay = SelectRetryDelay(delay, hint)
			}
		}
		if jitter > 0 {
			delay = addJitter(delay, int64(jitter))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return lastErr
}

// exponentialDelay 算指数退避 base*2^(attempt-1)，**先封顶再放大**，饱和而非回绕。
//
// 移位写法（base << (attempt-1)）在 attempt 较大时 int64 溢出成负数——
// 负数不满足「> maxDelay」，于是绕过 MaxDelay 封顶，time.After(负数) 立刻
// 返回，该轮退避彻底消失（#45）。改用乘法 + 饱和封顶后，MaxDelay 才是
// 真正的上界。
//
// 与 concurrency/retry 的指数退避同口径（同一家族两处退避行为一致）。
func exponentialDelay(base time.Duration, attempt int, maxDelay time.Duration) time.Duration {
	delay := base
	for i := 1; i < attempt; i++ {
		if delay >= maxDelay {
			break
		}
		if delay > time.Duration(1<<62) { // 饱和而非回绕为负
			delay = time.Duration(1<<63 - 1)
			break
		}
		delay *= 2
	}
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
}

// addJitter 给 delay 加一段 [0, n) 的随机抖动，**饱和**而非回绕成负数。
//
// 「delay + 抖动」本身也会溢出：delay 接近 MaxInt64 时相加得到负 duration
// → time.After(负数) 立刻返回 → 退避形同虚设（与 #45 的溢出路径同源）。
func addJitter(delay time.Duration, n int64) time.Duration {
	if n <= 0 {
		return delay
	}
	if delay > time.Duration(math.MaxInt64)-time.Duration(n-1) {
		return time.Duration(math.MaxInt64)
	}
	return delay + time.Duration(rand.Int64N(n))
}

// retryable 判定一次错误是否值得重试：自定义判定优先；缺省网络超时/5xx/429
// 可重试（网络非超时错误按不可重试保守处理，避免对断言式失败空转）。
func retryable(err error, custom func(error) bool) bool {
	if custom != nil {
		return custom(err)
	}
	var se *StatusError
	if errors.As(err, &se) {
		return se.Code >= 500 || se.Code == http.StatusTooManyRequests
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
