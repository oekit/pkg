package httpjson

// 条件请求面（If-None-Match/ETag/304 重放，#269）：限流预算敏感消费方的
// 通用形态——GitHub 对 304 不计 rate-limit 预算（文档明示），GitLab/Gitee
// 同类端点亦有 ETag 支持；带 ETag 的列表/详情轮询，预算消耗可差一个数量级。
//
// 语义（下游 go-git-platform transport 层两年现场硬化出的三条纪律，逐条搬）：
//
//  1. **304 重放为 200 形响应**：命中缓存时注入 If-None-Match，服务端 304
//     时把缓存的响应体按 200 同形解码交给调用方——调用方完全不感知条件
//     请求的存在（Response 语义与 200 一致，无需特判 304）；
//  2. **304×驱逐竞态不得硬失败**：查存储有值 → 发请求 → 服务端 304 →
//     回查缓存体时条目已被并发驱逐（LRU 后端等）⇒ 降级为**无条件重发**
//     一次而非报错（v0.100.2 真实竞态教训，#31）；
//  3. **只对 GET/HEAD 注入**：写方法的 304 语义完全不同（If-None-Match
//     在写路径是乐观并发控制，命中意味着 412 而非「没变」）——其他方法
//     带着本选项执行时按无条件请求处理，不注入。

import (
	"sync"
)

// ETagStore 按请求键存取响应 ETag 与缓存体（#269）。
//
// 存储与客户端分离：进程内用 NewMemETagStore，跨进程/带淘汰策略（LRU）
// 的调用方自行实现本接口接 Redis 等后端——Get/Put 都可能被并发调用，
// 实现须并发安全。key 由调用方给（通常是 method+URL+影响响应的 query
// 组合）；同一个 store 允许多 key 并存。
type ETagStore interface {
	// Get 返回 key 对应的 (etag, 缓存体, 是否命中)。
	Get(key string) (etag, body string, ok bool)
	// Put 写入 key 的 (etag, 缓存体)。
	Put(key, etag, body string)
}

// RequestOption 单次请求的可选行为注入（仓规禁裸 Option：多包 co-import
// 冲突）。DoJSON/DoJSONWithRetry 以变参收下，零选项调用面与旧签名一致。
type RequestOption func(*Request)

// WithETag 为本次请求启用条件请求（#269）：命中缓存（store.Get(key) 有值）
// 时注入 If-None-Match；服务端 304 时重放缓存体为 200 形响应（out 按正常
// 200 路径解码）；响应带 ETag 头时把 (etag, body) 回写存储；ETag 头缺席
// 则不回写（下次请求也就无从注入）。
//
// 边界：
//   - store 为 nil 或 key 为空串是调用方配置错误，构造期 panic（仓规总则
//     ①：fail-fast，与 cron 构造期校验同口径）——静默按无条件执行会把
//     「配错了但没生效」藏到限流预算烧穿才发现；
//   - 只对 GET/HEAD 生效（写方法的 304 语义不同，见包文档第 3 条）；
//   - 304 时回查存储若已被驱逐，降级为无条件重发一次（竞态纪律第 2 条）；
//   - 来源标记（X-From-Cache 之类）刻意未做：DoJSON 的出参面只有 out 与
//     err，调用方要区分来源可在 store 实现侧埋点。
//
// 用法：
//
//	store := httpjson.NewMemETagStore()
//	err := httpjson.DoJSON(ctx, client, req, &resp,
//	    httpjson.WithETag(store, "GET "+url))
func WithETag(store ETagStore, key string) RequestOption {
	if store == nil {
		panic("httpjson: WithETag 的 store 为 nil——配了条件请求却不给存储是配置错误（fail-fast，仓规总则①）")
	}
	if key == "" {
		panic("httpjson: WithETag 的 key 为空串——跨请求命中需要稳定 key（fail-fast，仓规总则①）")
	}
	return func(r *Request) {
		r.etagStore = store
		r.etagKey = key
	}
}

// NewMemETagStore 进程内 ETagStore（RWMutex 保护的 map，无淘汰策略——
// 需要容量上限/LRU/跨进程的调用方实现 ETagStore 自接后端）。body 以
// string 持有（Go string 不可变，Get 返回的副本与存储内部天然隔离）。
func NewMemETagStore() ETagStore {
	return &memETagStore{m: make(map[string]etagEntry)}
}

type etagEntry struct {
	etag string
	body string
}

type memETagStore struct {
	mu sync.RWMutex
	m  map[string]etagEntry
}

func (s *memETagStore) Get(key string) (etag, body string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.m[key]
	if !ok {
		return "", "", false
	}
	return e.etag, e.body, true
}

func (s *memETagStore) Put(key, etag, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = etagEntry{etag: etag, body: body}
}
