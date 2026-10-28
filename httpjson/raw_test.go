package httpjson

import (
	"bytes"
	"context"
	"net"
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

	res, err := DoRaw(ctx, client, Request{Method: http.MethodGet, URL: srv.URL + "/ok"}, 0)
	if err != nil || res.StatusCode != http.StatusOK || string(res.Body) != `{"code":"XY","ttl":30}` {
		t.Fatalf("DoRaw(ok) = (%+v, %v)", res, err)
	}
	if res.Truncated {
		t.Fatal("完整报文不应标记 Truncated")
	}

	// 非 2xx：状态码与**原文**都要拿到（调用方自己映射语义）
	res, err = DoRaw(ctx, client, Request{Method: http.MethodGet, URL: srv.URL + "/conflict"}, 0)
	if err != nil || res.StatusCode != http.StatusConflict || string(res.Body) != `{"error":"他人占用"}` {
		t.Fatalf("DoRaw(conflict) = (%+v, %v)", res, err)
	}
}

// TestDoRawRespectsLimit 读体上限生效（防异常服务端刷屏放大内存）。
func TestDoRawRespectsLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer srv.Close()

	res, err := DoRaw(context.Background(), srv.Client(), Request{URL: srv.URL}, 4)
	if err != nil {
		t.Fatalf("DoRaw: %v", err)
	}
	if len(res.Body) != 4 {
		t.Fatalf("读体上限未生效: 期望 4 字节，实得 %d", len(res.Body))
	}
}

// TestDoRawReportsTruncation 回归测试：报文被截断时必须**如实标记**。
//
// 缺陷成因：DoRaw 用 io.LimitReader(resp.Body, limit) 读体，超限部分直接
// 丢弃，调用方拿到的 body 与完整报文**无法区分**——拿半截 JSON 去解析会得到
// 难以定位的解析错误，拿半截报文进错误文案会丢服务端的说明文字。
func TestDoRawReportsTruncation(t *testing.T) {
	big := bytes.Repeat([]byte("A"), 20000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(big)
	}))
	defer srv.Close()

	res, err := DoRaw(context.Background(), srv.Client(), Request{URL: srv.URL}, 1024)
	if err != nil {
		t.Fatalf("DoRaw: %v", err)
	}
	if len(res.Body) != 1024 {
		t.Fatalf("截断长度不对：%d", len(res.Body))
	}
	if !res.Truncated {
		t.Fatal("报文被截断但 Truncated 未标记：调用方无法区分截断与完整")
	}
}

// TestDoRawNotTruncatedWhenWithinLimit 未超限时不标记 Truncated。
func TestDoRawNotTruncatedWhenWithinLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer srv.Close()

	res, err := DoRaw(context.Background(), srv.Client(), Request{URL: srv.URL}, 1024)
	if err != nil {
		t.Fatalf("DoRaw: %v", err)
	}
	if res.Truncated {
		t.Fatal("报文未超限却标记了 Truncated")
	}
	if string(res.Body) != "0123456789" {
		t.Fatalf("报文内容不对：%q", res.Body)
	}
}

// TestDoRawBodyExactlyAtLimit 报文长度**正好等于** limit 时不算截断。
func TestDoRawBodyExactlyAtLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer srv.Close()

	res, err := DoRaw(context.Background(), srv.Client(), Request{URL: srv.URL}, 10)
	if err != nil {
		t.Fatalf("DoRaw: %v", err)
	}
	if res.Truncated {
		t.Fatal("报文长度正好等于 limit，不应标记 Truncated")
	}
}

// TestDoRawConnectionReused 服务端侧统计新建连接数：连续多次请求若响应体被
// 读走（含排空），连接应当被复用（新建连接数不随请求数增长）。
//
// 缺陷成因：此前只读到 limit 就 Close，响应体剩余部分没人读——Go 的 transport
// 只有在 body 被读完时才把连接放回空闲池 ⇒ 连接被丢弃 ⇒ 每次调用都要重建
// TCP（+ TLS 握手）。
//
// 报文必须**大于** limit 才会触发：body 比 limit 小时 LimitReader 已经把它
// 读完了，压根不需要排空。
func TestDoRawConnectionReused(t *testing.T) {
	big := bytes.Repeat([]byte("A"), 10*1024)
	var newConns int
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(big)
	}))
	srv.Config.ConnState = func(c net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConns++
		}
	}
	srv.Start()
	defer srv.Close()

	client := srv.Client()
	for i := 0; i < 5; i++ {
		res, err := DoRaw(context.Background(), client, Request{URL: srv.URL}, 1024)
		if err != nil {
			t.Fatalf("DoRaw #%d: %v", i, err)
		}
		if !res.Truncated {
			t.Fatalf("第 %d 次应被截断", i)
		}
	}
	if newConns > 1 {
		t.Fatalf("连接未被复用：5 次请求新建了 %d 条连接（应为 1 条）", newConns)
	}
}
