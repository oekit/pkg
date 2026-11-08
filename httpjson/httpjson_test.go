package httpjson

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoJSONFastPathAndStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte(`{"v":1}`))
		case "/bad":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte("参数不完整"))
		}
	}))
	defer srv.Close()

	var out struct {
		V int `json:"v"`
	}
	if err := DoJSON(context.Background(), srv.Client(), Request{URL: srv.URL + "/ok"}, &out); err != nil {
		t.Fatalf("fast path: %v", err)
	}
	if out.V != 1 {
		t.Fatalf("v = %d", out.V)
	}

	err := DoJSON(context.Background(), srv.Client(), Request{URL: srv.URL + "/bad"}, &out)
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 422 {
		t.Fatalf("应返回 StatusError(422): %v", err)
	}
	// Error() 文案与历史格式一致（HTTP %d: 响应体摘要）。
	if got := se.Error(); got != "HTTP 422: 参数不完整" {
		t.Fatalf("Error() = %q", got)
	}
}

func TestDoJSONWithRetry(t *testing.T) {
	t.Run("5xx 两次后成功", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) <= 2 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer srv.Close()

		var out struct {
			Ok bool `json:"ok"`
		}
		cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
		if err := DoJSONWithRetry(context.Background(), srv.Client(),
			func() (Request, error) { return Request{URL: srv.URL}, nil }, cfg, &out); err != nil {
			t.Fatalf("重试后应成功: %v", err)
		}
		if calls.Load() != 3 {
			t.Fatalf("应尝试 3 次, got %d", calls.Load())
		}
	})

	t.Run("4xx 不重试", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer srv.Close()

		var out struct{}
		err := DoJSONWithRetry(context.Background(), srv.Client(),
			func() (Request, error) { return Request{URL: srv.URL}, nil },
			RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond}, &out)
		var se *StatusError
		if !errors.As(err, &se) || se.Code != 400 {
			t.Fatalf("应透传 StatusError(400): %v", err)
		}
		if calls.Load() != 1 {
			t.Fatalf("4xx 不得重试, got %d 次", calls.Load())
		}
	})

	t.Run("自定义 Retryable 判定", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusTeapot) // 418：默认不可重试
				return
			}
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()

		var out struct{}
		cfg := RetryConfig{MaxAttempts: 2, BaseDelay: time.Millisecond,
			Retryable: func(err error) bool {
				var se *StatusError
				return errors.As(err, &se) && se.Code == http.StatusTeapot
			}}
		if err := DoJSONWithRetry(context.Background(), srv.Client(),
			func() (Request, error) { return Request{URL: srv.URL}, nil }, cfg, &out); err != nil {
			t.Fatalf("自定义判定应允许重试 418: %v", err)
		}
		if calls.Load() != 2 {
			t.Fatalf("应尝试 2 次, got %d", calls.Load())
		}
	})

	t.Run("reqFn 构造失败不重试", func(t *testing.T) {
		called := false
		err := DoJSONWithRetry(context.Background(), http.DefaultClient,
			func() (Request, error) { called = true; return Request{}, errors.New("bad req") },
			RetryConfig{MaxAttempts: 3}, &struct{}{})
		if err == nil || !called || called == false {
			t.Fatalf("构造错误应立即返回: err=%v called=%v", err, called)
		}
	})
}

func TestDoJSONSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		body := make([]byte, r.ContentLength)
		if _, err := r.Body.Read(body); err != nil && len(body) == 0 {
			t.Errorf("读请求体: %v", err)
		}
		if string(body) != `"hi"` {
			t.Errorf("Body = %q", body)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	var out struct {
		OK bool `json:"ok"`
	}
	err := DoJSON(context.Background(), srv.Client(), Request{
		Method: http.MethodPost,
		URL:    srv.URL,
		Body:   []byte(`"hi"`),
		Header: func(h http.Header) {
			h.Set("Authorization", "Bearer tok")
			h.Set("Content-Type", "application/json")
		},
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatal("out.OK = false")
	}
}

func TestDoJSONErrorStatusCarriesRuneSafeSnippet(t *testing.T) {
	// 错误体摘要须 rune 安全（此前 embedder 按字节截断会腰斩 UTF-8），且超长截到 200。
	multibyte := strings.Repeat("错", 150) // 300 字节、150 rune
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(multibyte))
	}))
	defer srv.Close()

	var out map[string]any
	err := DoJSON(context.Background(), srv.Client(), Request{URL: srv.URL}, &out)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("非 2xx 应带状态码报错: %v", err)
	}
	if got := err.Error(); strings.Count(got, "错") > 200 {
		t.Fatalf("错误体摘要应截断: %d", strings.Count(got, "错"))
	}
}

func TestDoJSONDecodeAndTransportErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	var out map[string]any
	if err := DoJSON(context.Background(), srv.Client(), Request{URL: srv.URL}, &out); err == nil ||
		!strings.Contains(err.Error(), "响应解析失败") {
		t.Fatalf("坏 JSON 应报解析错误: %v", err)
	}

	// 不可达端点 → 传输错误
	if err := DoJSON(context.Background(), srv.Client(), Request{URL: "http://127.0.0.1:1/x"}, &out); err == nil {
		t.Fatal("传输错误应上抛")
	}
}

// ---- #158：out==nil 恒报 Unmarshal(nil)，2xx 成功请求被误报失败 ----

// TestDoJSONNilOutSkipsDecode 钉住 #158：out 为 nil 是 fire-and-forget 语义
// ——只关心请求送达与状态码，不关心响应体。此前无条件 json.Unmarshal(raw, nil)
// 恒定拿到 `json: Unmarshal(nil)` 的 InvalidUnmarshalError ⇒ 服务端已返回 2xx
// 的成功请求被误报为失败。
//
// 判据（每条都必须在「去掉 out==nil 守卫」时转红）：
//   - 2xx + out==nil ⇒ err 为 nil（去掉守卫 ⇒ Unmarshal(nil) 错误）；
//   - 2xx + out==nil + **响应体根本不是 JSON** ⇒ 仍为 nil（证明是「跳过解码」
//     而不是「对坏 JSON 宽容」）；
//   - 请求确实送达了服务端（fire-and-forget 的前半截语义没被牺牲）。
func TestDoJSONNilOutSkipsDecode(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// 故意回非 JSON：out==nil 时不该有任何解码动作。
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	err := DoJSON(context.Background(), srv.Client(), Request{URL: srv.URL}, nil)
	if err != nil {
		t.Fatalf("out==nil 时 2xx 应返回 nil，实际报错: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("请求应送达服务端一次，got %d", hits.Load())
	}
}

// TestDoJSONNilOutKeepsStatusError 钉住「跳过解码」不得吞掉非 2xx：
// 守卫若被挪到状态码检查之前，2xx 判定就会连错误一起放过 ⇒ 真失败被静默。
func TestDoJSONNilOutKeepsStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("参数不完整"))
	}))
	defer srv.Close()

	err := DoJSON(context.Background(), srv.Client(), Request{URL: srv.URL}, nil)
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusBadGateway {
		t.Fatalf("out==nil 也不得放过非 2xx: %v", err)
	}
	if got := se.Error(); got != "HTTP 502: 参数不完整" {
		t.Fatalf("Error() = %q", got)
	}
}

// TestDoJSONWithRetryNilOutSkipsDecode 钉住 DoJSONWithRetry 的同款语义：
// out==nil 时 2xx 即成功（不重试、不报错）。
func TestDoJSONWithRetryNilOutSkipsDecode(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
	err := DoJSONWithRetry(context.Background(), srv.Client(),
		func() (Request, error) { return Request{URL: srv.URL}, nil }, cfg, nil)
	if err != nil {
		t.Fatalf("out==nil 时 2xx 应返回 nil，实际报错: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("成功不应重试，got %d 次", calls.Load())
	}
}

// TestDoJSONWithRetryNilOutStillRetries 钉住「跳过解码」不得影响重试语义：
// 5xx 仍要退避重试，out==nil 只是不解析响应体。
func TestDoJSONWithRetryNilOutStillRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
	err := DoJSONWithRetry(context.Background(), srv.Client(),
		func() (Request, error) { return Request{URL: srv.URL}, nil }, cfg, nil)
	if err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("应尝试 3 次, got %d", calls.Load())
	}
}

// TestDoJSONNilOutMatrix 表驱动钉住 out==nil 的完整口径：2xx 成功、非 2xx 报错，
// 且 out 非 nil 时解码行为**不变**（防止把守卫写成「out 存在就不解码」）。
func TestDoJSONNilOutMatrix(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		outNil     bool
		wantErr    bool
		wantStatus int
	}{
		{name: "2xx 且 out==nil ⇒ 成功", status: http.StatusOK, body: `{"ok":true}`, outNil: true},
		{name: "2xx 且 out==nil 且响应体非 JSON ⇒ 成功", status: http.StatusOK, body: `plain text`, outNil: true},
		{name: "204 且 out==nil ⇒ 成功", status: http.StatusNoContent, body: ``, outNil: true},
		{name: "5xx 且 out==nil ⇒ 报错", status: http.StatusInternalServerError, body: `boom`, outNil: true, wantErr: true, wantStatus: http.StatusInternalServerError},
		{name: "2xx 且 out 非 nil ⇒ 照常解码", status: http.StatusOK, body: `{"ok":true}`},
		{name: "2xx 且 out 非 nil 且响应体非 JSON ⇒ 照常报解析错", status: http.StatusOK, body: `plain text`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			var out map[string]any
			var target any = &out
			if tc.outNil {
				target = nil
			}
			err := DoJSON(context.Background(), srv.Client(), Request{URL: srv.URL}, target)

			if tc.wantErr {
				if err == nil {
					t.Fatal("应报错")
				}
				if tc.wantStatus != 0 {
					var se *StatusError
					if !errors.As(err, &se) || se.Code != tc.wantStatus {
						t.Fatalf("应返回 StatusError(%d): %v", tc.wantStatus, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("不应报错: %v", err)
			}
			if !tc.outNil {
				ok, _ := out["ok"].(bool)
				if !ok {
					t.Fatalf("out 未被解码: %v", out)
				}
			}
		})
	}
}

// ---- #45：指数退避移位溢出产出负延迟 ----

// TestExponentialDelay_NeverNegativeAndAlwaysCapped 回归 #45：移位写法
// （base << (attempt-1)）在 attempt 较大时 int64 溢出成负数，负数不满足
// 「> maxDelay」于是绕过封顶，time.After(负数) 立刻返回——该轮退避消失。
// 判据：任意 attempt 下退避非负、且恒 ≤ MaxDelay。
func TestExponentialDelay_NeverNegativeAndAlwaysCapped(t *testing.T) {
	base := 200 * time.Millisecond
	maxDelay := 2 * time.Second
	for attempt := 1; attempt <= 200; attempt++ {
		d := exponentialDelay(base, attempt, maxDelay)
		if d < 0 {
			t.Fatalf("attempt=%d 退避为负：%v", attempt, d)
		}
		if d > maxDelay {
			t.Fatalf("attempt=%d 越过 MaxDelay 封顶：%v > %v", attempt, d, maxDelay)
		}
	}
}

// TestExponentialDelay_OverflowPointPinned 钉住 issue 复现的溢出点：
// BaseDelay=200ms 时移位写法在 attempt=37/39/40 产出负延迟。
func TestExponentialDelay_OverflowPointPinned(t *testing.T) {
	base := 200 * time.Millisecond
	maxDelay := 2 * time.Second
	for _, attempt := range []int{36, 37, 38, 39, 40, 64, 100} {
		d := exponentialDelay(base, attempt, maxDelay)
		if d <= 0 {
			t.Fatalf("attempt=%d 退避消失：%v", attempt, d)
		}
		if d != maxDelay {
			t.Fatalf("attempt=%d 应封顶到 %v，实得 %v", attempt, maxDelay, d)
		}
	}
}

// TestExponentialDelay_MonotonicUpToCap 封顶前必须单调递增（指数语义）。
func TestExponentialDelay_MonotonicUpToCap(t *testing.T) {
	base := 50 * time.Millisecond
	maxDelay := 10 * time.Second
	prev := time.Duration(0)
	for attempt := 1; attempt <= 20; attempt++ {
		d := exponentialDelay(base, attempt, maxDelay)
		if d < prev {
			t.Fatalf("attempt=%d 退避回退：%v < %v", attempt, d, prev)
		}
		if d > maxDelay {
			t.Fatalf("attempt=%d 越过封顶：%v", attempt, d)
		}
		prev = d
	}
}

// TestAddJitter_SaturatesInsteadOfOverflowing 退避 + 抖动不得回绕成负数：
// 负 duration 传给 time.After 会立刻返回，退避形同虚设。
func TestAddJitter_SaturatesInsteadOfOverflowing(t *testing.T) {
	d := addJitter(time.Duration(math.MaxInt64), int64(math.MaxInt64))
	if d < 0 {
		t.Fatalf("抖动后不应为负：%v", d)
	}
	if d != time.Duration(math.MaxInt64) {
		t.Fatalf("应饱和到 MaxInt64：%v", d)
	}
}

// TestAddJitter_StaysInRange 抖动幅度口径不变：结果落在 [delay, delay+n)。
func TestAddJitter_StaysInRange(t *testing.T) {
	base := 100 * time.Millisecond
	n := int64(20 * time.Millisecond)
	for i := 0; i < 50; i++ {
		d := addJitter(base, n)
		if d < base || d >= base+time.Duration(n) {
			t.Fatalf("抖动越界：%v 不在 [%v, %v)", d, base, base+time.Duration(n))
		}
	}
}

// TestDo_EmptyBody2xx_ZeroOut 钉住 #189：2xx 空 body（204/200-no-content）+
// out!=nil 时跳过解码——此前 json.Unmarshal([]byte{}) 必报「响应解析失败」，
// 把成功误报为失败（重试即重复施加副作用）。
func TestDo_EmptyBody2xx_ZeroOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent) // 204 无 body
	}))
	defer srv.Close()

	var resp map[string]any
	err := DoJSON(context.Background(), srv.Client(), Request{
		Method: http.MethodDelete,
		URL:    srv.URL + "/x",
	}, &resp)
	if err != nil {
		t.Fatalf("204 + out!=nil 应成功跳过解码, got %v", err)
	}
}

// retryAfterClient 组装装了 sink + RetryAfterTransport 的调用面（429 建议捕获
// 与重试环消费以此相遇）。
func retryAfterClient(t *testing.T, srv *httptest.Server) (context.Context, *http.Client) {
	t.Helper()
	ctx := WithRetryAfterSink(context.Background())
	client := srv.Client()
	client.Transport = RetryAfterTransport{Base: client.Transport}
	return ctx, client
}

// TestDoJSONWithRetry_StaleHintNotAppliedToNon429 钉住 #302：sink 不随尝试清除，
// 重试环曾对任何 >0 的 RetryAfterFrom(ctx) 建议照单全收——一次 429 之后同 key
// 的任何可重试错误（5xx/网络超时）都被「限流化」，本地指数曲线被陈旧建议顶到
// 秒级。修复后仅当**本次失败**确为限流（StatusError.Code==429）才采信，
// 与 RetryAfterFrom 文档自述纪律（「陈旧建议不得套用到非限流错误上」）一致。
func TestDoJSONWithRetry_StaleHintNotAppliedToNon429(t *testing.T) {
	var calls atomic.Int32
	var hits [4]time.Time // 服务端记录每次到达时刻（含首答 429 的建议回写）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		if n <= len(hits) {
			hits[n-1] = time.Now()
		}
		switch n {
		case 1: // 429 + Retry-After: 1（sink 写入 1s）
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2: // 瞬时性 502（服务已恢复）——陈旧建议不得套到这次重试上
			w.WriteHeader(http.StatusBadGateway)
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()

	ctx, client := retryAfterClient(t, srv)
	var out struct {
		Ok bool `json:"ok"`
	}
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: 10 * time.Millisecond, MaxDelay: 20 * time.Millisecond}
	if err := DoJSONWithRetry(ctx, client,
		func() (Request, error) { return Request{URL: srv.URL}, nil }, cfg, &out); err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("应尝试 3 次, got %d", calls.Load())
	}
	// 第 1→2 跳（429 后）：建议属本次限流，照常采信——延迟 ≈ 1s。
	if gap := hits[1].Sub(hits[0]); gap < 900*time.Millisecond {
		t.Errorf("429 后的重试应采信 Retry-After:1（gap = %v, want >= 900ms）", gap)
	}
	// 第 2→3 跳（502 后）：陈旧建议不得采信——本地曲线 10-20ms；
	// 若被限流化 gap ≈ 1s。
	if gap := hits[2].Sub(hits[1]); gap >= 900*time.Millisecond {
		t.Errorf("502 后的重试被陈旧建议限流化（gap = %v, want < 900ms）——#302 回归", gap)
	}
}

// TestDoJSONWithRetry_HintStillAppliedOnConsecutive429 正向对照（#302 门控
// 不得矫枉过正）：连续 429 时建议照常采信——门控只过滤「非限流错误」，不砍
// 限流场景本身的服务端退避。
func TestDoJSONWithRetry_HintStillAppliedOnConsecutive429(t *testing.T) {
	var calls atomic.Int32
	var hits [3]time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		if n <= len(hits) {
			hits[n-1] = time.Now()
		}
		if n <= 2 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	ctx, client := retryAfterClient(t, srv)
	var out struct {
		Ok bool `json:"ok"`
	}
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: 10 * time.Millisecond, MaxDelay: 20 * time.Millisecond}
	if err := DoJSONWithRetry(ctx, client,
		func() (Request, error) { return Request{URL: srv.URL}, nil }, cfg, &out); err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if gap := hits[1].Sub(hits[0]); gap < 900*time.Millisecond {
		t.Errorf("第 1 次 429 后应采信建议（gap = %v, want >= 900ms）", gap)
	}
	if gap := hits[2].Sub(hits[1]); gap < 900*time.Millisecond {
		t.Errorf("第 2 次 429（本次失败确为限流）后仍应采信建议（gap = %v, want >= 900ms）", gap)
	}
}
