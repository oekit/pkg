package httpjson

// 条件请求测试（#269）：If-None-Match 注入、304 重放为 200 形响应、ETag
// 回写、304×驱逐竞态降级重发、非 GET/HEAD 不注入、构造期 fail-fast。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// etagServer 按 If-None-Match 决定 200/304 的测试服务端；记录每轮请求头。
type etagServer struct {
	t    *testing.T
	srv  *httptest.Server
	etag string // 当前资源 ETag（空 = 服务端不支持 ETag）
	body string

	mu   sync.Mutex
	reqs []string // 每轮请求的 If-None-Match 值（"" = 未注入）
}

func newETagServer(t *testing.T, etag, body string) *etagServer {
	s := &etagServer{t: t, etag: etag, body: body}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.reqs = append(s.reqs, r.Header.Get("If-None-Match"))
		s.mu.Unlock()
		if s.etag != "" && r.Header.Get("If-None-Match") == s.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if s.etag != "" {
			w.Header().Set("ETag", s.etag)
		}
		_, _ = w.Write([]byte(s.body))
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func (s *etagServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reqs)
}

func (s *etagServer) lastIfNoneMatch() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.reqs) == 0 {
		return "(no requests)"
	}
	return s.reqs[len(s.reqs)-1]
}

func TestWithETag_FirstRequestPopulatesStore(t *testing.T) {
	srv := newETagServer(t, `"v1"`, `{"n":1}`)
	store := NewMemETagStore()
	var out struct{ N int }

	req := Request{URL: srv.srv.URL}
	key := "GET " + srv.srv.URL
	if err := DoJSON(context.Background(), srv.srv.Client(), req, &out, WithETag(store, key)); err != nil {
		t.Fatalf("DoJSON: %v", err)
	}
	if out.N != 1 {
		t.Fatalf("out.N = %d, want 1", out.N)
	}
	etag, body, ok := store.Get(key)
	if !ok || etag != `"v1"` || body != `{"n":1}` {
		t.Fatalf("store 应被回写 (etag=%q body=%q ok=%v), got %q %q %v", `"v1"`, `{"n":1}`, true, etag, body, ok)
	}
}

func TestWithETag_304ReplayAs200(t *testing.T) {
	srv := newETagServer(t, `"v1"`, `{"n":1}`)
	store := NewMemETagStore()
	key := "GET " + srv.srv.URL
	req := Request{URL: srv.srv.URL}

	// 第一次：200 + 回写
	var first struct{ N int }
	if err := DoJSON(context.Background(), srv.srv.Client(), req, &first, WithETag(store, key)); err != nil {
		t.Fatalf("DoJSON#1: %v", err)
	}
	// 第二次：注入 If-None-Match → 服务端 304 → 本地重放，网络请求仍发生
	// （一轮）但响应体来自缓存。
	var second struct{ N int }
	if err := DoJSON(context.Background(), srv.srv.Client(), req, &second, WithETag(store, key)); err != nil {
		t.Fatalf("DoJSON#2: %v", err)
	}
	if second.N != 1 {
		t.Fatalf("304 重放应把缓存体解码进 out, got %+v", second)
	}
	if srv.count() != 2 {
		t.Fatalf("应恰 2 轮网络请求（304 不跳过请求）, got %d", srv.count())
	}
	if got := srv.lastIfNoneMatch(); got != `"v1"` {
		t.Fatalf("第二次请求应注入 If-None-Match: %q, got %q", `"v1"`, got)
	}
	// 304 重放不回写（ETag 未变）
	if _, body, _ := store.Get(key); body != `{"n":1}` {
		t.Fatalf("304 重放后 store 应保持原缓存体, got %q", body)
	}
}

func TestWithETag_ETagChangeUpdatesStore(t *testing.T) {
	srv := newETagServer(t, `"v1"`, `{"n":1}`)
	store := NewMemETagStore()
	key := "GET " + srv.srv.URL
	req := Request{URL: srv.srv.URL}
	var out struct{ N int }

	if err := DoJSON(context.Background(), srv.srv.Client(), req, &out, WithETag(store, key)); err != nil {
		t.Fatalf("DoJSON#1: %v", err)
	}
	// 服务端内容变更 + 新 ETag
	srv.mu.Lock()
	srv.etag = `"v2"`
	srv.body = `{"n":2}`
	srv.mu.Unlock()
	// 客户端缓存里还是 v1：注入 If-None-Match: "v1" → 服务端 200 + v2
	if err := DoJSON(context.Background(), srv.srv.Client(), req, &out, WithETag(store, key)); err != nil {
		t.Fatalf("DoJSON#2: %v", err)
	}
	if out.N != 2 {
		t.Fatalf("ETag 变更后应拿到新内容, out.N=%d", out.N)
	}
	etag, body, _ := store.Get(key)
	if etag != `"v2"` || body != `{"n":2}` {
		t.Fatalf("store 应更新为新 ETag/新体, got (%q,%q)", etag, body)
	}
	// 第三次：v2 命中 → 304 重放
	if err := DoJSON(context.Background(), srv.srv.Client(), req, &out, WithETag(store, key)); err != nil {
		t.Fatalf("DoJSON#3: %v", err)
	}
	if out.N != 2 {
		t.Fatalf("304 重放应解出新内容, out.N=%d", out.N)
	}
}

// TestWithETag_EvictedDuring304 304×驱逐竞态：发请求前有缓存，服务端 304
// 后回查存储时条目已被驱逐 ⇒ 降级为无条件重发而非硬失败（现场纪律，#31）。
func TestWithETag_EvictedDuring304(t *testing.T) {
	srv := newETagServer(t, `"v1"`, `{"n":1}`)
	key := "GET " + srv.srv.URL
	// 包装：首次 Get 命中后立刻掏空存储（模拟 LRU 在请求在途期间回收条目）
	inner := NewMemETagStore()
	inner.Put(key, `"v1"`, `{"n":1}`)
	store := &evictingStore{inner: inner}

	var out struct{ N int }
	if err := DoJSON(context.Background(), srv.srv.Client(), Request{URL: srv.srv.URL}, &out, WithETag(store, key)); err != nil {
		t.Fatalf("驱逐竞态应降级重发而非硬失败: %v", err)
	}
	if out.N != 1 {
		t.Fatalf("降级重发应解出 200 内容, out.N=%d", out.N)
	}
	if srv.count() != 2 {
		t.Fatalf("304+驱逐应重发一次（共 2 轮）, got %d", srv.count())
	}
	// 第二轮是无条件重发
	srv.mu.Lock()
	second := srv.reqs[1]
	srv.mu.Unlock()
	if second != "" {
		t.Fatalf("降级重发不应再带 If-None-Match, got %q", second)
	}
}

// evictingStore 测试用包装：首次 Get 命中后把内层存储换成空表（模拟驱逐）。
type evictingStore struct {
	inner ETagStore
	once  sync.Once
}

func (s *evictingStore) Get(key string) (string, string, bool) {
	etag, body, ok := s.inner.Get(key)
	s.once.Do(func() { s.inner = NewMemETagStore() })
	return etag, body, ok
}

func (s *evictingStore) Put(key, etag, body string) {
	s.inner.Put(key, etag, body)
}

func TestWithETag_OnlyGetHead(t *testing.T) {
	srv := newETagServer(t, `"v1"`, `{"n":1}`)
	store := NewMemETagStore()
	key := "POST " + srv.srv.URL
	// 预填存储：若 POST 错误地注入条件头，服务端会 304
	if err := DoJSON(context.Background(), srv.srv.Client(), Request{URL: srv.srv.URL, Method: http.MethodPost, Body: []byte(`{}`)}, nil, WithETag(store, key)); err != nil {
		t.Fatalf("DoJSON POST: %v", err)
	}
	if got := srv.lastIfNoneMatch(); got != "" {
		t.Fatalf("写方法不应注入 If-None-Match, got %q", got)
	}
	if _, _, ok := store.Get(key); ok {
		t.Fatal("写方法不应回写存储")
	}
	if srv.count() != 1 {
		t.Fatalf("POST 应恰 1 轮, got %d", srv.count())
	}
}

func TestWithETag_200WithoutETagNoStoreWrite(t *testing.T) {
	srv := newETagServer(t, "", `{"n":1}`) // 服务端不发 ETag
	store := NewMemETagStore()
	key := "GET " + srv.srv.URL
	var out struct{ N int }
	if err := DoJSON(context.Background(), srv.srv.Client(), Request{URL: srv.srv.URL}, &out, WithETag(store, key)); err != nil {
		t.Fatalf("DoJSON: %v", err)
	}
	if _, _, ok := store.Get(key); ok {
		t.Fatal("响应无 ETag 头不应回写存储")
	}
	// 第二次请求不注入（存储为空）
	if err := DoJSON(context.Background(), srv.srv.Client(), Request{URL: srv.srv.URL}, &out, WithETag(store, key)); err != nil {
		t.Fatalf("DoJSON#2: %v", err)
	}
	if got := srv.lastIfNoneMatch(); got != "" {
		t.Fatalf("存储为空不应注入 If-None-Match, got %q", got)
	}
}

func TestWithETag_ReplayOutNil(t *testing.T) {
	srv := newETagServer(t, `"v1"`, `{"n":1}`)
	store := NewMemETagStore()
	key := "GET " + srv.srv.URL
	req := Request{URL: srv.srv.URL}
	if err := DoJSON(context.Background(), srv.srv.Client(), req, nil, WithETag(store, key)); err != nil {
		t.Fatalf("DoJSON#1(out=nil): %v", err)
	}
	// out=nil 的 304 重放：同样返回 nil（fire-and-forget 的条件轮询形态）
	if err := DoJSON(context.Background(), srv.srv.Client(), req, nil, WithETag(store, key)); err != nil {
		t.Fatalf("DoJSON#2(out=nil): %v", err)
	}
}

func TestWithETag_FailFast(t *testing.T) {
	cases := []struct {
		name  string
		store ETagStore
		key   string
	}{
		{"nil store", nil, "k"},
		{"空 key", NewMemETagStore(), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("构造期非法值应 panic（仓规总则①）")
				}
			}()
			_ = WithETag(tc.store, tc.key)
		})
	}
}

func TestWithETag_ReplayBrokenCacheReportsParseError(t *testing.T) {
	srv := newETagServer(t, `"v1"`, `{"n":1}`)
	store := NewMemETagStore()
	key := "GET " + srv.srv.URL
	req := Request{URL: srv.srv.URL}
	var out struct{ N int }
	if err := DoJSON(context.Background(), srv.srv.Client(), req, &out, WithETag(store, key)); err != nil {
		t.Fatalf("DoJSON#1: %v", err)
	}
	// 缓存体被外部污染：重放走与 200 相同的解码线 ⇒ 明确报「响应解析失败」
	// （不是 StatusError——重放的是 200 形响应，不是 304 形错误）
	store.Put(key, `"v1"`, "not-json")
	err := DoJSON(context.Background(), srv.srv.Client(), req, &out, WithETag(store, key))
	var se *StatusError
	if err == nil || errors.As(err, &se) || !strings.Contains(err.Error(), "响应解析失败") {
		t.Fatalf("坏缓存重放应报解析错误, got %v (%T)", err, err)
	}
}

// TestWithETag_ConcurrentAccess 并发同 key 轮询：store 与 DoJSON 全程无竞态。
func TestWithETag_ConcurrentAccess(t *testing.T) {
	srv := newETagServer(t, `"v1"`, `{"n":1}`)
	store := NewMemETagStore()
	key := "GET " + srv.srv.URL
	req := Request{URL: srv.srv.URL}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out struct{ N int }
			for j := 0; j < 10; j++ {
				if err := DoJSON(context.Background(), srv.srv.Client(), req, &out, WithETag(store, key)); err != nil {
					t.Errorf("DoJSON: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestWithETag_WithRetry DoJSONWithRetry 逐轮透传 opts：条件请求在重试层
// 内透明工作（304 重放成功即返回，不触发重试）。
func TestWithETag_WithRetry(t *testing.T) {
	srv := newETagServer(t, `"v1"`, `{"n":1}`)
	store := NewMemETagStore()
	key := "GET " + srv.srv.URL
	var out struct{ N int }
	reqFn := func() (Request, error) { return Request{URL: srv.srv.URL}, nil }
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond}
	if err := DoJSONWithRetry(context.Background(), srv.srv.Client(), reqFn, cfg, &out, WithETag(store, key)); err != nil {
		t.Fatalf("DoJSONWithRetry: %v", err)
	}
	if err := DoJSONWithRetry(context.Background(), srv.srv.Client(), reqFn, cfg, &out, WithETag(store, key)); err != nil {
		t.Fatalf("DoJSONWithRetry#2（304 重放）: %v", err)
	}
	if out.N != 1 {
		t.Fatalf("out.N = %d", out.N)
	}
	if got := srv.lastIfNoneMatch(); got != `"v1"` {
		t.Fatalf("第二轮应注入 If-None-Match, got %q", got)
	}
}
