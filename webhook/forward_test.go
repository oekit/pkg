package webhook

// 转发器契约测试（#482）：游标只在全部 sink 成功后推进（失败不推进）、
// 重投形态（下轮重发同一批）、批序与毒批 fail-loud、webhook sink 的
// HMAC 签名可验与 body 形态钉住、构造期 fail-fast。

import (
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// stubSink 记录每次收到的批次（拷贝防别名）并按配置报错。
type stubSink struct {
	mu    sync.Mutex
	err   error
	calls [][]ForwardEvent
}

func (s *stubSink) Forward(_ context.Context, batch []ForwardEvent) error {
	cp := append([]ForwardEvent(nil), batch...)
	s.mu.Lock()
	s.calls = append(s.calls, cp)
	s.mu.Unlock()
	return s.err
}

func (s *stubSink) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func batchOf(seqs ...uint64) []ForwardEvent {
	b := make([]ForwardEvent, 0, len(seqs))
	for _, q := range seqs {
		b = append(b, ForwardEvent{Seq: q, Payload: []byte(fmt.Sprintf(`{"id":"ev-%d"}`, q))})
	}
	return b
}

// cursorSource 内存批源：按游标过滤升序返回，模拟宿主查询。
type cursorSource struct {
	events []ForwardEvent
}

func (s *cursorSource) FetchAfter(_ context.Context, cursor uint64, limit int) ([]ForwardEvent, error) {
	out := make([]ForwardEvent, 0, limit)
	for _, ev := range s.events {
		if ev.Seq > cursor && len(out) < limit {
			out = append(out, ev)
		}
	}
	return out, nil
}

// failSaveStore 游标落库必失败（at-least-once 的「已投递未记账」形态）。
type failSaveStore struct{ MemoryCursorStore }

func (s *failSaveStore) SaveCursor(_ context.Context, _ uint64) error {
	return errors.New("db down")
}

func TestForwardOnce_AdvancesCursorOnlyOnAllSinkSuccess(t *testing.T) {
	store := NewMemoryCursorStore()
	src := &cursorSource{events: batchOf(1, 2, 3)}
	a, b := &stubSink{}, &stubSink{}
	f := NewForwarder(store, src, []ForwardSink{a, b})

	n, err := f.ForwardOnce(context.Background())
	if err != nil {
		t.Fatalf("全部成功不应出错, got %v", err)
	}
	if n != 3 {
		t.Fatalf("应外发 3 条, got %d", n)
	}
	cur, _ := store.LoadCursor(context.Background())
	if cur != 3 {
		t.Fatalf("游标应推进到最大 Seq 3, got %d", cur)
	}
	if a.callCount() != 1 || b.callCount() != 1 {
		t.Fatalf("两 sink 各投递一轮, got %d/%d", a.callCount(), b.callCount())
	}

	// 再一轮：空批 (0, nil)，游标不动
	n, err = f.ForwardOnce(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("空批应 (0, nil), got (%d, %v)", n, err)
	}
	cur, _ = store.LoadCursor(context.Background())
	if cur != 3 {
		t.Fatalf("空批后游标不动, got %d", cur)
	}
}

func TestForwardOnce_FailingSinkKeepsCursorAndRedelivers(t *testing.T) {
	store := NewMemoryCursorStore()
	src := &cursorSource{events: batchOf(1, 2)}
	a := &stubSink{}
	b := &stubSink{err: ErrDeliveryFailed}
	f := NewForwarder(store, src, []ForwardSink{a, b})

	// 第一轮：B 失败——A 已投递但游标不动
	_, err := f.ForwardOnce(context.Background())
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("sink 失败应包 ErrDeliveryFailed, got %v", err)
	}
	cur, _ := store.LoadCursor(context.Background())
	if cur != 0 {
		t.Fatalf("任一 sink 失败游标不动, got %d", cur)
	}

	// 第二轮：B 恢复——A 收到**重复**整批（at-least-once，宁重不丢）
	b.err = nil
	n, err := f.ForwardOnce(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("恢复后应成功外发 2 条, got (%d, %v)", n, err)
	}
	if a.callCount() != 2 {
		t.Fatalf("A 应被重投, got %d 轮", a.callCount())
	}
	if len(a.calls[0]) != 2 || len(a.calls[1]) != 2 {
		t.Fatalf("重投单位是整批: 第一轮 %d 条 第二轮 %d 条", len(a.calls[0]), len(a.calls[1]))
	}
	for i, ev := range a.calls[1] {
		if ev.Seq != a.calls[0][i].Seq {
			t.Fatalf("重投应重复同窗: 第 %d 条 Seq %d != %d", i, a.calls[1][i].Seq, a.calls[0][i].Seq)
		}
	}
	cur, _ = store.LoadCursor(context.Background())
	if cur != 2 {
		t.Fatalf("恢复后游标推进到 2, got %d", cur)
	}
}

func TestForwardOnce_SaveCursorFailureRedelivers(t *testing.T) {
	store := &failSaveStore{MemoryCursorStore: *NewMemoryCursorStore()}
	src := &cursorSource{events: batchOf(1)}
	a := &stubSink{}
	f := NewForwarder(store, src, []ForwardSink{a})

	_, err := f.ForwardOnce(context.Background())
	if err == nil {
		t.Fatal("游标落库失败应上抛")
	}
	if a.callCount() != 1 {
		t.Fatalf("sink 已投递, got %d 轮", a.callCount())
	}
	cur, _ := store.LoadCursor(context.Background())
	if cur != 0 {
		t.Fatalf("落库失败游标不动, got %d", cur)
	}

	// 修复存储后：同一批重发（先投递后记账 ⇒ 「丢」只可能表现为「重」）
	src2 := &cursorSource{events: batchOf(1)}
	store2 := NewMemoryCursorStore()
	f2 := NewForwarder(store2, src2, []ForwardSink{&stubSink{}})
	n, err := f2.ForwardOnce(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("应成功外发, got (%d, %v)", n, err)
	}
	cur, _ = store2.LoadCursor(context.Background())
	if cur != 1 {
		t.Fatalf("游标应推进, got %d", cur)
	}
}

func TestForwardOnce_RejectsNonMonotonicBatch(t *testing.T) {
	store := NewMemoryCursorStore()
	// 源侧序坏：未越过游标 + 回退序
	src := BatchSourceFunc(func(_ context.Context, cursor uint64, _ int) ([]ForwardEvent, error) {
		return []ForwardEvent{
			{Seq: cursor, Payload: []byte(`{"id":"old"}`)}, // 未越过
			{Seq: cursor + 1, Payload: []byte(`{"id":"a"}`)},
		}, nil
	})
	sink := &stubSink{}
	f := NewForwarder(store, src, []ForwardSink{sink})

	_, err := f.ForwardOnce(context.Background())
	if !errors.Is(err, ErrBadForwardBatch) {
		t.Fatalf("批序非法应包 ErrBadForwardBatch, got %v", err)
	}
	if sink.callCount() != 0 {
		t.Fatal("批序非法不得进 sink")
	}
	cur, _ := store.LoadCursor(context.Background())
	if cur != 0 {
		t.Fatalf("批序非法游标不动, got %d", cur)
	}

	// 回退序（升序但中间一条更小）
	src2 := BatchSourceFunc(func(_ context.Context, _ uint64, _ int) ([]ForwardEvent, error) {
		return []ForwardEvent{
			{Seq: 5, Payload: []byte(`{"id":"a"}`)},
			{Seq: 3, Payload: []byte(`{"id":"b"}`)},
		}, nil
	})
	f2 := NewForwarder(NewMemoryCursorStore(), src2, []ForwardSink{&stubSink{}})
	if _, err := f2.ForwardOnce(context.Background()); !errors.Is(err, ErrBadForwardBatch) {
		t.Fatalf("回退序应 ErrBadForwardBatch, got %v", err)
	}
}

func TestForwardOnce_RejectsPoisonPayloadBeforeSinks(t *testing.T) {
	store := NewMemoryCursorStore()
	src := BatchSourceFunc(func(_ context.Context, _ uint64, _ int) ([]ForwardEvent, error) {
		return []ForwardEvent{
			{Seq: 1, Payload: []byte(`{"id":"ok"}`)},
			{Seq: 2, Payload: []byte(`{"id":`)}, // 毒批
		}, nil
	})
	sink := &stubSink{}
	f := NewForwarder(store, src, []ForwardSink{sink})

	_, err := f.ForwardOnce(context.Background())
	if !errors.Is(err, ErrInvalidForwardPayload) {
		t.Fatalf("毒批应包 ErrInvalidForwardPayload, got %v", err)
	}
	if sink.callCount() != 0 {
		t.Fatal("毒批不得进任何 sink（fail-loud 卡游标是有意设计）")
	}
	cur, _ := store.LoadCursor(context.Background())
	if cur != 0 {
		t.Fatalf("毒批游标不动, got %d", cur)
	}
}

func TestForwardOnce_EmptyPayloadInSource(t *testing.T) {
	store := NewMemoryCursorStore()
	src := BatchSourceFunc(func(_ context.Context, _ uint64, _ int) ([]ForwardEvent, error) {
		return []ForwardEvent{{Seq: 1, Payload: nil}}, nil
	})
	f := NewForwarder(store, src, []ForwardSink{&stubSink{}})
	if _, err := f.ForwardOnce(context.Background()); !errors.Is(err, ErrInvalidForwardPayload) {
		t.Fatalf("空载荷应包 ErrInvalidForwardPayload, got %v", err)
	}
}

func TestMemoryCursorStore_Roundtrip(t *testing.T) {
	s := NewMemoryCursorStore()
	cur, err := s.LoadCursor(context.Background())
	if err != nil || cur != 0 {
		t.Fatalf("无游标应返回 0 而非 error, got (%d, %v)", cur, err)
	}
	if err := s.SaveCursor(context.Background(), 42); err != nil {
		t.Fatalf("保存不应出错, got %v", err)
	}
	cur, _ = s.LoadCursor(context.Background())
	if cur != 42 {
		t.Fatalf("游标应回读 42, got %d", cur)
	}
}

func TestNewForwarder_FailFast(t *testing.T) {
	sink := &stubSink{}
	cases := []struct {
		name string
		fn   func()
	}{
		{"nil存储", func() { NewForwarder(nil, &cursorSource{}, []ForwardSink{sink}) }},
		{"nil批源", func() { NewForwarder(NewMemoryCursorStore(), nil, []ForwardSink{sink}) }},
		{"无sink", func() { NewForwarder(NewMemoryCursorStore(), &cursorSource{}, nil) }},
		{"空sink", func() { NewForwarder(NewMemoryCursorStore(), &cursorSource{}, []ForwardSink{}) }},
		{"nilsink", func() { NewForwarder(NewMemoryCursorStore(), &cursorSource{}, []ForwardSink{nil}) }},
		{"niloption", func() {
			NewForwarder(NewMemoryCursorStore(), &cursorSource{}, []ForwardSink{sink}, nil)
		}},
		{"非法批上限", func() {
			NewForwarder(NewMemoryCursorStore(), &cursorSource{}, []ForwardSink{sink}, WithForwardBatchLimit(0))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("构造期非法值应 panic（仓规总则①）")
				}
			}()
			tc.fn()
		})
	}
}

func TestWebhookSink_SignatureVerifiableAndBodyPinned(t *testing.T) {
	var (
		gotBody   []byte
		gotHeader http.Header
		gotType   string
		mu        sync.Mutex
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = body
		gotHeader = r.Header.Clone()
		gotType = r.Header.Get("Content-Type")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	signer := NewHMACSigner([]byte(testSecret), crypto.SHA256, "X-Test-Signature")
	sink := NewWebhookSink(srv.URL, WithWebhookSigner(signer))
	batch := []ForwardEvent{
		{Seq: 1, Payload: []byte(`{"id":"a1","act":"login"}`)},
		{Seq: 2, Payload: []byte(`{"id":"a2","act":"logout"}`)},
	}
	if err := sink.Forward(context.Background(), batch); err != nil {
		t.Fatalf("投递不应出错, got %v", err)
	}

	// body 形态钉住：载荷 JSON 数组、元素字节原样嵌入（不重排序不重编码）
	wantBody := `[{"id":"a1","act":"login"},{"id":"a2","act":"logout"}]`
	if string(gotBody) != wantBody {
		t.Fatalf("body 形态漂移:\n want %s\n got  %s", wantBody, gotBody)
	}
	if gotType != "application/json" {
		t.Fatalf("Content-Type 应 application/json, got %q", gotType)
	}

	// HMAC 签名可验：收端用同 secret 的验证器（包内发出侧↔收侧配对约定）
	v := NewHMACValidator([]byte(testSecret), crypto.SHA256, "X-Test-Signature")
	if err := v.Validate(mkReqWithHeader(gotHeader), gotBody); err != nil {
		t.Fatalf("收端验签应通过, got %v", err)
	}
	// 篡改 body 即验签失败（签名覆盖的是发出的 body 字节）
	if err := v.Validate(mkReqWithHeader(gotHeader), []byte(`{"tampered":1}`)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("篡改 body 应 ErrBadSignature, got %v", err)
	}
}

// mkReqWithHeader 用收到的 header 集构造验签请求（只读 header）。
func mkReqWithHeader(h http.Header) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://sink.example/forward", nil)
	r.Header = h
	return r
}

func TestWebhookSink_Non2xxFailsDelivery(t *testing.T) {
	for _, code := range []int{400, 404, 500} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
		}))
		sink := NewWebhookSink(srv.URL)
		err := sink.Forward(context.Background(), []ForwardEvent{{Seq: 1, Payload: []byte(`{}`)}})
		srv.Close()
		if !errors.Is(err, ErrDeliveryFailed) {
			t.Fatalf("HTTP %d 应包 ErrDeliveryFailed, got %v", code, err)
		}
	}
}

func TestWebhookSink_NetworkErrorFailsDelivery(t *testing.T) {
	// 起服务再立刻关掉：地址确定不可连（无固定端口竞争）
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close()
	sink := NewWebhookSink(url)
	err := sink.Forward(context.Background(), []ForwardEvent{{Seq: 1, Payload: []byte(`{}`)}})
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("连接失败应包 ErrDeliveryFailed, got %v", err)
	}
}

func TestWebhookSink_InvalidPayload(t *testing.T) {
	sink := NewWebhookSink("http://127.0.0.1:1/unused")
	cases := []struct {
		name    string
		payload []byte
	}{
		{"空载荷", nil},
		{"非法JSON", []byte(`{"id":`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := sink.Forward(context.Background(), []ForwardEvent{{Seq: 1, Payload: tc.payload}})
			if !errors.Is(err, ErrInvalidForwardPayload) {
				t.Fatalf("应包 ErrInvalidForwardPayload, got %v", err)
			}
		})
	}
}

func TestWebhookSink_ForwarderIntegration(t *testing.T) {
	// 端到端：Forwarder + 真 HTTP sink——签名可验 + 游标推进到批尾
	var (
		mu       sync.Mutex
		received [][]byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := NewMemoryCursorStore()
	src := &cursorSource{events: batchOf(1, 2)}
	sink := NewWebhookSink(srv.URL, WithWebhookSigner(NewHMACSigner([]byte(testSecret), crypto.SHA256, "X-Sig")))
	f := NewForwarder(store, src, []ForwardSink{sink})

	if _, err := f.ForwardOnce(context.Background()); err != nil {
		t.Fatalf("外发不应出错, got %v", err)
	}
	mu.Lock()
	bodies := append([][]byte(nil), received...)
	mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("应收到 1 批, got %d", len(bodies))
	}
	var decoded []map[string]any
	if err := json.Unmarshal(bodies[0], &decoded); err != nil {
		t.Fatalf("收端应解出 JSON 数组, got %v", err)
	}
	if len(decoded) != 2 || decoded[0]["id"] != "ev-1" || decoded[1]["id"] != "ev-2" {
		t.Fatalf("载荷数组形态不符: %v", decoded)
	}
	cur, _ := store.LoadCursor(context.Background())
	if cur != 2 {
		t.Fatalf("游标应推进到 2, got %d", cur)
	}
}

func TestWebhookSink_FailFast(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"空URL", func() { NewWebhookSink("") }},
		{"nil签名器", func() { NewWebhookSink("http://x", WithWebhookSigner(nil)) }},
		{"nil客户端", func() { NewWebhookSink("http://x", WithWebhookClient(nil)) }},
		{"niloption", func() { NewWebhookSink("http://x", nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("构造期非法值应 panic（仓规总则①）")
				}
			}()
			tc.fn()
		})
	}
}
