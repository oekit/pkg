package httpjson

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDoRawReturnsStatusAndBody DoRaw 返回**原始**状态码与报文（不判 2xx）。
func TestDoRawReturnsStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":"XY","ttl":30}`))
		case "/conflict":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"他人占用"}`))
		case "/long":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("0123456789"))
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	client := srv.Client()

	status, body, err := DoRaw(ctx, client, Request{Method: http.MethodGet, URL: srv.URL + "/ok"}, 0)
	if err != nil || status != http.StatusOK || string(body) != `{"code":"XY","ttl":30}` {
		t.Fatalf("DoRaw(ok) = (%d, %q, %v)", status, body, err)
	}

	// 非 2xx：状态码与**原文**都要拿到（调用方自己映射语义）
	status, body, err = DoRaw(ctx, client, Request{Method: http.MethodGet, URL: srv.URL + "/conflict"}, 0)
	if err != nil || status != http.StatusConflict || string(body) != `{"error":"他人占用"}` {
		t.Fatalf("DoRaw(conflict) = (%d, %q, %v)", status, body, err)
	}
}

// TestDoRawRespectsLimit 读体上限生效（防异常服务端刷屏放大内存）。
func TestDoRawRespectsLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer srv.Close()

	_, body, err := DoRaw(context.Background(), srv.Client(), Request{URL: srv.URL}, 4)
	if err != nil {
		t.Fatalf("DoRaw: %v", err)
	}
	if len(body) != 4 {
		t.Fatalf("读体上限未生效: 期望 4 字节，实得 %d", len(body))
	}
}
