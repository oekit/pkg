// Package httpjson stdlib-only 的 HTTP+JSON 调用纪律单源：ctx 感知构造请求 →
// 执行 → 限容读体（读错误不吞）→ 状态码检查（错误体 rune 安全截断，不腰斩
// UTF-8）→ JSON 解码，外加可选的指数退避重试与服务端 Retry-After 建议捕获。
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
}

// DoJSON 执行请求并把 2xx 响应体解码进 out。非 2xx 返回带状态码与响应体摘要
// （rune 安全截断到 200 字符）的错误；解码失败返回包装后的解析错误。
// 错误不带包前缀返回，调用方以自己的包前缀包装一次。
func DoJSON(ctx context.Context, client *http.Client, req Request, out any) error {
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
		return fmt.Errorf("构造请求失败: %w", err)
	}
	if req.Header != nil {
		req.Header(httpReq.Header)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &StatusError{Code: resp.StatusCode, Body: string(raw)}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("响应解析失败: %w", err)
	}
	return nil
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
// 参与退避取舍（调用方在 wait 前经 SelectRetryDelay(RetryAfterFrom(ctx)) 采信）。
func DoJSONWithRetry(ctx context.Context, client *http.Client, reqFn func() (Request, error), cfg RetryConfig, out any) error {
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
		err = DoJSON(ctx, client, req, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == attempts || !retryable(err, cfg.Retryable) {
			break
		}
		delay := exponentialDelay(base, attempt, maxDelay)
		if hint := RetryAfterFrom(ctx); hint > 0 {
			delay = SelectRetryDelay(delay, hint)
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
