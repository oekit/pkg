package httpjson

// #288/#289/#290 回归钉：no-store 门禁、有界存储（LRU + 条目体积上限）、
// vary 头分区键。

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ccServer 可配 Cache-Control 响应头、记录每轮 Authorization/If-None-Match
// 的测试服务端（不判条件请求：注入与否由记录面断言）。
type ccServer struct {
	srv  *httptest.Server
	etag string
	body string
	cc   string // Cache-Control 响应头（"" = 不带）

	mu   sync.Mutex
	auth []string // 每轮 Authorization 头
	inm  []string // 每轮 If-None-Match（"" = 未注入）
}

func newCCServer(t *testing.T, etag, body, cacheControl string) *ccServer {
	s := &ccServer{etag: etag, body: body, cc: cacheControl}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		s.inm = append(s.inm, r.Header.Get("If-None-Match"))
		s.mu.Unlock()
		if s.cc != "" {
			w.Header().Set("Cache-Control", s.cc)
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

func (s *ccServer) records() (auth, inm []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.auth...), append([]string(nil), s.inm...)
}

// #288：响应带 Cache-Control: no-store 时不得回写存储——RFC 9111 §5.2.2.5
// 的 MUST NOT store（凭据/审计/一次性 token 端点是典型）。正对照：同形
// 服务端不带 no-store 时照常回写，证明拦的确实是 no-store 而不是误伤。
func TestWithETag_NoStoreNotCached(t *testing.T) {
	tests := []struct {
		name string
		cc   string
		want bool // 是否应入存储
	}{
		{name: "no-store 单指令", cc: "no-store", want: false},
		{name: "no-store 与其他指令并列", cc: "max-age=0, no-store, must-revalidate", want: false},
		{name: "NO-STORE 大小写不敏感", cc: "NO-STORE", want: false},
		{name: "无 Cache-Control（正对照）", cc: "", want: true},
		{name: "no-cache 不受 gate（可存但须验证——WithETag 本身就是验证）", cc: "no-cache", want: true},
		{name: "private 不受 gate（进程内 store 是 private cache）", cc: "private, max-age=60", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newCCServer(t, `"v1"`, `{"n":1}`, tt.cc)
			store := NewMemETagStore()
			key := CanonicalETagKey("GET", srv.srv.URL)
			var out struct{ N int }
			if err := DoJSON(context.Background(), srv.srv.Client(), Request{URL: srv.srv.URL}, &out, WithETag(store, key)); err != nil {
				t.Fatalf("DoJSON: %v", err)
			}
			_, _, ok := store.Get(key)
			if ok != tt.want {
				t.Fatalf("Cache-Control %q: 入存储 = %v, want %v", tt.cc, ok, tt.want)
			}
		})
	}
}

// #289：NewMemETagStore 有界化——LRU 条目淘汰 + 超限条目不入存储。
func TestMemETagStore_Bounded(t *testing.T) {
	t.Run("条目数 LRU 淘汰", func(t *testing.T) {
		s := NewMemETagStore(WithETagStoreCapacity(2)).(*memETagStore)
		s.Put("a", `"1"`, "a")
		s.Put("b", `"2"`, "b")
		// 读命中提升 a：最旧变成 b
		if _, _, ok := s.Get("a"); !ok {
			t.Fatalf("a 应命中")
		}
		s.Put("c", `"3"`, "c") // 淘汰 b
		if _, _, ok := s.Get("b"); ok {
			t.Fatalf("b 应被 LRU 淘汰")
		}
		if etag, _, ok := s.Get("a"); !ok || etag != `"1"` {
			t.Fatalf("a 应仍在（读命中提升）, ok=%v", ok)
		}
		if _, _, ok := s.Get("c"); !ok {
			t.Fatalf("c 应命中")
		}
	})
	t.Run("超限响应不入存储（每次全量取）", func(t *testing.T) {
		s := NewMemETagStore(WithETagStoreMaxBodySize(16)).(*memETagStore)
		s.Put("small", `"1"`, "tiny")
		s.Put("big", `"2"`, strings.Repeat("x", 17))
		if _, _, ok := s.Get("big"); ok {
			t.Fatalf("超限条目不得入存储（#289：大响应永久驻留是无界面）")
		}
		if _, body, ok := s.Get("small"); !ok || body != "tiny" {
			t.Fatalf("未超限条目应正常入存, ok=%v body=%q", ok, body)
		}
	})
	t.Run("缺省上限生效（DefaultETagStoreEntries/DefaultMaxETagBodySize）", func(t *testing.T) {
		s := NewMemETagStore().(*memETagStore)
		if s.cfg.entries != DefaultETagStoreEntries || s.cfg.maxBody != DefaultMaxETagBodySize {
			t.Fatalf("缺省上限 = %d/%d, want %d/%d", s.cfg.entries, s.cfg.maxBody, DefaultETagStoreEntries, DefaultMaxETagBodySize)
		}
		for i := 0; i < DefaultETagStoreEntries+1; i++ {
			s.Put(fmt.Sprintf("k%d", i), `"e"`, "b")
		}
		if len(s.m) != DefaultETagStoreEntries {
			t.Fatalf("条目数应封顶在 %d, got %d", DefaultETagStoreEntries, len(s.m))
		}
		if _, _, ok := s.Get("k0"); ok {
			t.Fatalf("最旧条目 k0 应被淘汰")
		}
		if _, _, ok := s.Get(fmt.Sprintf("k%d", DefaultETagStoreEntries)); !ok {
			t.Fatalf("最新条目应仍在")
		}
	})
	t.Run("非法容量 fail-fast（仓规总则①）", func(t *testing.T) {
		for _, f := range []func(){
			func() { NewMemETagStore(WithETagStoreCapacity(0)) },
			func() { NewMemETagStore(WithETagStoreCapacity(-1)) },
			func() { NewMemETagStore(WithETagStoreMaxBodySize(0)) },
			func() { NewMemETagStore(WithETagStoreMaxBodySize(-1)) },
		} {
			func() {
				defer func() {
					if recover() == nil {
						t.Fatalf("非法容量应 panic（fail-fast）")
					}
				}()
				f()
			}()
		}
	})
}

// #290：CanonicalETagKey 规范键 + WithETagVary 头分区。
func TestCanonicalETagKey(t *testing.T) {
	base := CanonicalETagKey("GET", "https://api/x")
	if got := CanonicalETagKey("get", "https://api/x"); got != base {
		t.Fatalf("method 大小写应归一: %q vs %q", got, base)
	}
	if got := CanonicalETagKey("", "https://api/x"); got != base {
		t.Fatalf("空 method 按 GET: %q vs %q", got, base)
	}
	// vary 值参与分区：凭据 A/B 不同键
	kA := CanonicalETagKey("GET", "https://api/x", "Bearer a")
	kB := CanonicalETagKey("GET", "https://api/x", "Bearer b")
	if kA == kB || kA == base {
		t.Fatalf("vary 值必须改变键: %q %q %q", kA, kB, base)
	}
	// 分段无歧义："a"+"bc" 与 "ab"+"c" 不撞键
	if CanonicalETagKey("GET", "u", "a", "bc") == CanonicalETagKey("GET", "u", "ab", "c") {
		t.Fatalf("\\n 分隔应保证分段边界无歧义")
	}
}

// #290 端到端：Authorization 值轮换天然分区——凭据 B 不重放缓存给凭据 A 的
// 条目（无 vary 键时会串缓存），同凭据复访注入 If-None-Match（304 面）。
func TestWithETagVary_AuthorizationPartitionsCache(t *testing.T) {
	srv := newCCServer(t, `"v1"`, `{"n":1}`, "")
	store := NewMemETagStore()
	key := "GET " + srv.srv.URL

	doWithToken := func(token string) {
		t.Helper()
		var out struct{ N int }
		req := Request{URL: srv.srv.URL, Header: func(h http.Header) {
			h.Set("Authorization", "Bearer "+token)
		}}
		if err := DoJSON(context.Background(), srv.srv.Client(), req, &out,
			WithETagVary(store, key, "Accept", "Authorization")); err != nil {
			t.Fatalf("DoJSON(token=%s): %v", token, err)
		}
	}

	doWithToken("a") // 凭据 A：200 回写（A 分区）
	doWithToken("b") // 凭据 B：键不同 → 不得注入 If-None-Match
	doWithToken("a") // 凭据 A 复访：命中 A 分区 → 注入 If-None-Match

	auth, inm := srv.records()
	if len(auth) != 3 {
		t.Fatalf("应 3 轮请求, got %d", len(auth))
	}
	if inm[0] != "" {
		t.Fatalf("首轮不应注入 If-None-Match, got %q", inm[0])
	}
	if inm[1] != "" {
		t.Fatalf("凭据轮换后不得注入（键分区生效）: If-None-Match=%q", inm[1])
	}
	if inm[2] != `"v1"` {
		t.Fatalf("同凭据复访应注入 If-None-Match, got %q", inm[2])
	}
	// A/B 两个分区条目并存
	kA := CanonicalETagKey("GET", key, "", "Bearer a") // Accept 空 + Authorization
	kB := CanonicalETagKey("GET", key, "", "Bearer b")
	for _, k := range []string{kA, kB} {
		if _, body, ok := store.Get(k); !ok || body == "" {
			t.Fatalf("分区 %q 应有缓存体, ok=%v", k, ok)
		}
	}
}

func TestWithETagVary_FailFast(t *testing.T) {
	store := NewMemETagStore()
	for _, f := range []func(){
		func() { WithETagVary(nil, "k") },
		func() { WithETagVary(store, "") },
		func() { WithETagVary(store, "k", " ") },
		func() { WithETagVary(store, "k", "Authorization", "") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("非法参数应 panic（fail-fast）")
				}
			}()
			f()
		}()
	}
}
