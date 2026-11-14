package httpjson

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// #496：sink 只写不清——第三方自建重试环复用同一 sink 跨两次请求时，
// 第二次 429 无 Retry-After 头不会采信第一次的窗口。
func TestClearRetryAfterPerAttempt(t *testing.T) {
	ctx := WithRetryAfterSink(context.Background())

	// 第一轮：429 带 Retry-After: 30 → sink 捕获 30s。
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer first.Close()
	req1, _ := http.NewRequestWithContext(ctx, http.MethodGet, first.URL, nil)
	resp1, err := RetryAfterTransport{}.RoundTrip(req1)
	if err != nil {
		t.Fatal(err)
	}
	resp1.Body.Close()
	if got := RetryAfterFrom(ctx); got != 30*time.Second {
		t.Fatalf("第一轮应捕获 30s: %v", got)
	}

	// 按尝试清除：返回作废前的值。
	if got := ClearRetryAfter(ctx); got != 30*time.Second {
		t.Fatalf("ClearRetryAfter 应返回作废前的值: %v", got)
	}

	// 第二轮：429 不带 Retry-After → 不得采信第一轮的旧窗口。
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer second.Close()
	req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, second.URL, nil)
	resp2, err := RetryAfterTransport{}.RoundTrip(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if got := RetryAfterFrom(ctx); got != 0 {
		t.Fatalf("清除后第二轮无响应头必须读到 0（不得采信旧窗口）: %v", got)
	}

	// 未安装 sink 的 ctx：Clear 为 no-op。
	if got := ClearRetryAfter(context.Background()); got != 0 {
		t.Fatalf("未安装 sink Clear 应返回 0: %v", got)
	}
}
