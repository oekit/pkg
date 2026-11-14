package sse

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// connect 建立一条 SSE 连接，返回响应与可取消的请求上下文。
// ctx 超时是读护栏：测试逻辑卡死时读取报错而非挂死整个测试。
func connect(t *testing.T, url string, header http.Header) (*http.Response, context.CancelFunc, *bufio.Reader) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		t.Fatalf("构造请求: %v", err)
	}
	if header == nil {
		header = http.Header{}
	}
	req.Header = header
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("发起请求: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		t.Fatalf("状态码 = %d，body = %q", resp.StatusCode, body)
	}
	return resp, cancel, bufio.NewReader(resp.Body)
}

// readFrame 读完一帧（到空行）。
func readFrame(t *testing.T, br *bufio.Reader) string {
	t.Helper()
	var sb strings.Builder
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("读帧: %v（已收 %q）", err, sb.String())
		}
		sb.WriteString(line)
		if line == "\n" {
			return sb.String()
		}
	}
}

// eventFrame 期望的事件帧形态（与 Event.frame 序列化对齐）。
func eventFrame(id uint64, typ, data string) string {
	var sb strings.Builder
	sb.WriteString("id: " + strconv.FormatUint(id, 10) + "\n")
	if typ != "" {
		sb.WriteString("event: " + typ + "\n")
	}
	for _, line := range strings.Split(data, "\n") {
		sb.WriteString("data: " + line + "\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

// TestReplayWindowOrderAndDedup 契约：游标窗口回放序 + 订阅后不重复。
// 「订阅先于回放」下，回放窗口与实时广播的重叠帧（ID 6）恰好写一次；
// 重复广播的同 ID 帧与旧游标帧跳过；订阅前的广播不进流（客户端已报
// 游标 2 持有它）。
func TestReplayWindowOrderAndDedup(t *testing.T) {
	hub := NewHub()
	afterCh := make(chan uint64, 1)
	replay := func(after uint64) []Event {
		afterCh <- after
		hub.Emit(Event{ID: 6, Data: "during"}) // 模拟回放进行中的实时广播（与窗口尾重叠）
		return []Event{
			{ID: 3, Type: "run.step", Data: "a"},
			{ID: 4, Data: "b1\nb2"},
			{ID: 5, Data: "c"},
			{ID: 6, Data: "during"},
		}
	}
	srv := httptest.NewServer(NewResponder(hub, replay))
	defer srv.Close()

	hub.Emit(Event{ID: 2, Data: "before"}) // 订阅建立前广播：不进任何流

	resp, cancel, br := connect(t, srv.URL, http.Header{"Last-Event-ID": {"2"}})
	defer func() {
		cancel()
		_ = resp.Body.Close()
	}()

	if got := <-afterCh; got != 2 {
		t.Fatalf("replay 收到 after = %d，应为游标 2", got)
	}

	want := []string{
		eventFrame(3, "run.step", "a"),
		eventFrame(4, "", "b1\nb2"),
		eventFrame(5, "", "c"),
		eventFrame(6, "", "during"), // 回放写入一次
	}
	for i, w := range want {
		if got := readFrame(t, br); got != w {
			t.Fatalf("第 %d 帧 = %q，应为 %q", i+1, got, w)
		}
	}

	hub.Emit(Event{ID: 7, Data: "live"})  // 回放结束后的实时帧
	hub.Emit(Event{ID: 6, Data: "dup"})   // 同 ID 重复广播：必须跳过
	hub.Emit(Event{ID: 8, Data: "live2"}) // 后续实时帧不受影响
	hub.Emit(Event{ID: 2, Data: "stale"}) // 旧游标重放：必须跳过
	if got := readFrame(t, br); got != eventFrame(7, "", "live") {
		t.Fatalf("实时帧 = %q，应为 %q", got, eventFrame(7, "", "live"))
	}
	if got := readFrame(t, br); got != eventFrame(8, "", "live2") {
		t.Fatalf("跳帧后的实时帧 = %q，应为 %q", got, eventFrame(8, "", "live2"))
	}
}

// TestHeartbeatFrameShape 契约：心跳注释帧形态——冒号开头注释行 + 空行
// 分帧（EventSource 忽略），空闲期按间隔持续发出、不夹带事件帧。
func TestHeartbeatFrameShape(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(NewResponder(hub, func(uint64) []Event { return nil },
		WithHeartbeat(20*time.Millisecond)))
	defer srv.Close()

	resp, cancel, br := connect(t, srv.URL, nil)
	defer func() {
		cancel()
		_ = resp.Body.Close()
	}()

	for i := 0; i < 3; i++ {
		if got := readFrame(t, br); got != ": keepalive\n\n" {
			t.Fatalf("第 %d 个心跳帧 = %q，应为 \": keepalive\\n\\n\"", i+1, got)
		}
	}
}

// TestHeartbeatCommentOverride 心跳文案可换（`: ping` 形态等价合法）。
func TestHeartbeatCommentOverride(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(NewResponder(hub, func(uint64) []Event { return nil },
		WithHeartbeat(15*time.Millisecond), WithHeartbeatComment(": ping")))
	defer srv.Close()

	resp, cancel, br := connect(t, srv.URL, nil)
	defer func() {
		cancel()
		_ = resp.Body.Close()
	}()

	if got := readFrame(t, br); got != ": ping\n\n" {
		t.Fatalf("心跳帧 = %q，应为 \": ping\\n\\n\"", got)
	}
}

// TestResponseHeaders 契约：SSE 应答头四件套齐备且状态 200。
func TestResponseHeaders(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(NewResponder(hub, func(uint64) []Event { return nil },
		WithHeartbeat(time.Hour)))
	defer srv.Close()

	resp, cancel, _ := connect(t, srv.URL, nil)
	defer func() {
		cancel()
		_ = resp.Body.Close()
	}()

	want := map[string]string{
		"Content-Type":      "text/event-stream; charset=utf-8",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"X-Accel-Buffering": "no",
	}
	for k, v := range want {
		if got := resp.Header.Get(k); got != v {
			t.Fatalf("响应头 %s = %q，应为 %q", k, got, v)
		}
	}
}

// TestClientDisconnectCleanup 契约：客户端断开后订阅被清理（Hub 不留
// 死订阅），断开后的广播安全无害。
func TestClientDisconnectCleanup(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(NewResponder(hub, func(uint64) []Event { return nil },
		WithHeartbeat(15*time.Millisecond)))
	defer srv.Close()

	resp, cancel, br := connect(t, srv.URL, nil)
	if got := readFrame(t, br); got != ": keepalive\n\n" {
		t.Fatalf("心跳帧 = %q", got)
	}
	cancel() // 客户端断开
	_ = resp.Body.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.Subscribers() == 0 {
			hub.Emit(Event{ID: 1, Data: "x"}) // 断开后广播：不得 panic
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("断开后订阅未清理，Subscribers = %d", hub.Subscribers())
}

// TestHubCloseEndsStreams 契约：Hub.Close 使在订流有序结束、Emit 变为
// 无操作、Close 幂等、Close 后 Subscribe 返回已关闭通道。
func TestHubCloseEndsStreams(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(NewResponder(hub, func(uint64) []Event { return nil },
		WithHeartbeat(time.Hour)))
	defer srv.Close()

	resp, cancel, br := connect(t, srv.URL, nil)
	defer func() {
		cancel()
		_ = resp.Body.Close()
	}()

	if err := hub.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := hub.Close(context.Background()); err != nil {
		t.Fatalf("Close 幂等: %v", err)
	}
	if _, err := br.Peek(1); err != io.EOF {
		t.Fatalf("Close 后流应结束（io.EOF），got err = %v", err)
	}
	hub.Emit(Event{ID: 1, Data: "x"}) // Close 后 Emit：不得 panic
	if n := hub.Subscribers(); n != 0 {
		t.Fatalf("Close 后 Subscribers = %d，应为 0", n)
	}
	ch, cancelSub := hub.Subscribe()
	defer cancelSub()
	if _, ok := <-ch; ok {
		t.Fatal("Close 后 Subscribe 应返回已关闭通道")
	}
}

// TestHubLaggardDisconnect 契约：慢消费者缓冲满即断流，缓冲内帧仍可读
// 完，掉队者从在订表摘除——客户端带游标重连由回放补窗（自愈闭环）。
func TestHubLaggardDisconnect(t *testing.T) {
	hub := NewHub(WithSubscriberBuffer(1))
	ch, cancel := hub.Subscribe()
	defer cancel()

	hub.Emit(Event{ID: 1})
	hub.Emit(Event{ID: 2}) // 缓冲满 = 掉队：断流

	if e := <-ch; e.ID != 1 {
		t.Fatalf("缓冲内帧 = %+v，应为 ID 1", e)
	}
	if _, ok := <-ch; ok {
		t.Fatal("掉队后流应关闭")
	}
	if n := hub.Subscribers(); n != 0 {
		t.Fatalf("掉队者应被摘除，Subscribers = %d", n)
	}
	hub.Emit(Event{ID: 3}) // 对已断流广播：不得 panic
}

// TestCursorParsing 游标解析：缺省 0 全量回放、空白容忍、非法拒绝。
func TestCursorParsing(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
		ok   bool
	}{
		{"", 0, true},
		{"  7 ", 7, true},
		{"0", 0, true},
		{"18446744073709551615", 18446744073709551615, true},
		{"abc", 0, false},
		{"-1", 0, false},
		{"1.5", 0, false},
		{"18446744073709551616", 0, false},
	}
	for _, c := range cases {
		got, ok := parseCursor(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Fatalf("parseCursor(%q) = (%d, %v)，应为 (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestBadCursorRejected 非法 Last-Event-ID 按 400 拒绝（fail-closed），
// 拒绝路径不留订阅。
func TestBadCursorRejected(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(NewResponder(hub, func(uint64) []Event { return nil }))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	req.Header.Set("Last-Event-ID", "abc")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法游标应 400，got %d", resp.StatusCode)
	}
	if n := hub.Subscribers(); n != 0 {
		t.Fatalf("拒绝路径不应留下订阅，Subscribers = %d", n)
	}
}

// TestFullReplayWithoutCursor 无 Last-Event-ID 视为游标 0（全量回放）。
// 读到回放帧即证明 replay 已执行、after 取值已定。
func TestFullReplayWithoutCursor(t *testing.T) {
	hub := NewHub()
	afterCh := make(chan uint64, 1)
	srv := httptest.NewServer(NewResponder(hub, func(a uint64) []Event {
		afterCh <- a
		return []Event{{ID: 1, Data: "x"}}
	}, WithHeartbeat(time.Hour)))
	defer srv.Close()

	resp, cancel, br := connect(t, srv.URL, nil)
	defer func() {
		cancel()
		_ = resp.Body.Close()
	}()
	if got := readFrame(t, br); got != eventFrame(1, "", "x") {
		t.Fatalf("回放帧 = %q，应为 %q", got, eventFrame(1, "", "x"))
	}
	if got := <-afterCh; got != 0 {
		t.Fatalf("无游标应全量回放（after=0），got %d", got)
	}
}

// TestEventFrameFormat 帧序列化：id/event/data 形态与多行 data 拆分。
func TestEventFrameFormat(t *testing.T) {
	got := string(Event{ID: 42, Type: "run.step", Data: "l1\nl2"}.frame())
	want := "id: 42\nevent: run.step\ndata: l1\ndata: l2\n\n"
	if got != want {
		t.Fatalf("frame = %q，应为 %q", got, want)
	}
	got = string(Event{ID: 1}.frame())
	want = "id: 1\ndata: \n\n"
	if got != want {
		t.Fatalf("空数据帧 = %q，应为 %q", got, want)
	}
}

// TestConstructorFailFast 构造期非法值 fail-fast（仓规总则①）。
func TestConstructorFailFast(t *testing.T) {
	hub := NewHub()
	replay := ReplayFunc(func(uint64) []Event { return nil })
	mustPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s 应 panic（fail-fast）", name)
			}
		}()
		f()
	}
	mustPanic("nil Source", func() { _ = NewResponder(nil, replay) })
	mustPanic("nil ReplayFunc", func() { _ = NewResponder(hub, nil) })
	mustPanic("心跳非正数", func() { _ = NewResponder(hub, replay, WithHeartbeat(0)) })
	mustPanic("心跳负数", func() { _ = NewResponder(hub, replay, WithHeartbeat(-time.Second)) })
	mustPanic("注释无冒号", func() { _ = NewResponder(hub, replay, WithHeartbeatComment("ping")) })
	mustPanic("注释含换行", func() { _ = NewResponder(hub, replay, WithHeartbeatComment(": a\nb")) })
	mustPanic("缓冲非正数", func() { _ = NewHub(WithSubscriberBuffer(0)) })
}
