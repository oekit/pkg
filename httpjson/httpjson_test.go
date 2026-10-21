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
