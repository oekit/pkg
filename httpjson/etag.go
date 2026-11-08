package httpjson

// 条件请求面（If-None-Match/ETag/304 重放，#269）：限流预算敏感消费方的
// 通用形态——GitHub 对 304 不计 rate-limit 预算（文档明示），GitLab/Gitee
// 同类端点亦有 ETag 支持；带 ETag 的列表/详情轮询，预算消耗可差一个数量级。
//
// 语义（下游 go-git-platform transport 层两年现场硬化出的纪律，逐条搬）：
//
//  1. **304 重放为 200 形响应**：命中缓存时注入 If-None-Match，服务端 304
//     时把缓存的响应体按 200 同形解码交给调用方——调用方完全不感知条件
//     请求的存在（Response 语义与 200 一致，无需特判 304）；
//  2. **304×驱逐竞态不得硬失败**：查存储有值 → 发请求 → 服务端 304 →
//     回查缓存体时条目已被并发驱逐（LRU 后端等）⇒ 降级为**无条件重发**
//     一次而非报错（v0.100.2 真实竞态教训，#31）；
//  3. **只对 GET/HEAD 注入**：写方法的 304 语义完全不同（If-None-Match
//     在写路径是乐观并发控制，命中意味着 412 而非「没变」）——其他方法
//     带着本选项执行时按无条件请求处理，不注入；
//  4. **Cache-Control: no-store 不落存储**（#288）：RFC 9111 §5.2.2.5，
//     no-store 指示任何 cache MUST NOT store——凭据端点、审计端点、一次性
//     token 端点是典型。只 gate no-store：no-cache（可存但须验证）与条件
//     请求面天然兼容；private 只约束共享缓存，进程内 store 是 private
//     cache 可存。
//
// **缓存键纪律**（#290）：影响响应的请求头必须进键——key 只含 method+URL
// 时，Authorization 轮换/换发会让凭据 A 与凭据 B 共享缓存条目（跨凭据缓存
// 串味），Accept 变体互相交叉污染。用 WithETagVary 把 vary 头声明进键
// （Authorization/Accept 是最小集），或用 CanonicalETagKey 自行构造。

import (
	"container/list"
	"net/textproto"
	"strings"
	"sync"
)

// ETagStore 按请求键存取响应 ETag 与缓存体（#269）。
//
// 存储与客户端分离：进程内用 NewMemETagStore（有界：LRU 条目上限 + 单条
// 体积上限），跨进程的调用方自行实现本接口接 Redis 等后端——Get/Put 都
// 可能被并发调用，实现须并发安全。key 由调用方给（推荐经 CanonicalETagKey
// / WithETagVary 构造，把「影响响应的头必须进键」变成构造期保证）；同一个
// store 允许多 key 并存。
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
// 200 路径解码）；响应带 ETag 头且**未带 Cache-Control: no-store**（#288）
// 时把 (etag, body) 回写存储；ETag 头缺席则不回写（下次请求也就无从注入）。
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
// ⚠️ 键形态纪律（#290）：key 必须覆盖**影响响应的一切入参**。只含
// method+URL 的键在 Authorization 轮换/换发场景会把凭据 A 的响应体重放给
// 凭据 B（跨凭据缓存串味），Accept 版本协商变体同理。带鉴权的请求
// （Request.Header 是鉴权注入钩子）请用 WithETagVary 声明 vary 头，或以
// CanonicalETagKey 构造含头分区的键。
//
// 用法：
//
//	store := httpjson.NewMemETagStore()
//	err := httpjson.DoJSON(ctx, client, req, &resp,
//	    httpjson.WithETagVary(store, "GET "+url, "Accept", "Authorization"))
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

// WithETagVary 条件请求 + vary 头分区（#290）：行为与 WithETag 一致，另把
// varyHeaders 各头（大小写不敏感，按 canonical MIME 名取值）在**本次请求
// 实际生效的请求头**里的值并入缓存键——键形如
// CanonicalETagKey(method, key, 值...)。凭据轮换（Authorization 换发）与
// 版本协商（Accept 变体）天然分区，互不串缓存。
//
// varyHeaders 为空等价 WithETag；头名空串/纯空白是配置错误，构造期 panic
// （仓规总则①）。Request.Header 钩子会为键派生**多调一次**（作用于独立
// 的临时 http.Header），钩子应保持无副作用。头在请求里缺席按空串计——
// 「没带该头」的请求共用一个分区，与 HTTP Vary 语义一致。
//
// 带鉴权的轮询请求（最常见组合）：
//
//	httpjson.DoJSON(ctx, client, req, &out,
//	    httpjson.WithETagVary(store, "GET "+url, "Accept", "Authorization"),
//	    /* req.Header 里注入 Bearer token */)
func WithETagVary(store ETagStore, key string, varyHeaders ...string) RequestOption {
	base := WithETag(store, key) // store/key 的 fail-fast 复用
	for _, h := range varyHeaders {
		if strings.TrimSpace(h) == "" {
			panic("httpjson: WithETagVary 的 vary 头名为空——影响响应的头必须给出可取值的头名（fail-fast，仓规总则①）")
		}
	}
	return func(r *Request) {
		base(r)
		r.etagVary = make([]string, len(varyHeaders))
		for i, h := range varyHeaders {
			r.etagVary[i] = textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(h))
		}
	}
}

// CanonicalETagKey 构造规范 ETag 缓存键（#290）：method、url 与各 vary 头
// 值以 "\n" 分隔拼接——分段拼接 + 分隔符保证各段边界无歧义（"a"+"bc" 与
// "ab"+"c" 不会撞键）。method 规范为大写（"get" 与 "GET" 同键）；url 与
// 各值 TrimSpace 后原样入键。
//
// 自行拼键的调用方（自管 vary 集合、或 store 侧需要同形键）用它替代手写
// "GET "+url——影响响应的头（Authorization/Accept 最小集）必须作为
// varyValues 参与；带 Vary 语义的调用方按响应的 Vary 头自行扩展。顺序由
// 调用方固定（同组头的值按同一顺序传入即稳定）。
func CanonicalETagKey(method, url string, varyValues ...string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}
	b := strings.Builder{}
	b.Grow(len(method) + len(url) + 16*len(varyValues))
	b.WriteString(method)
	b.WriteByte('\n')
	b.WriteString(strings.TrimSpace(url))
	for _, v := range varyValues {
		b.WriteByte('\n')
		b.WriteString(strings.TrimSpace(v))
	}
	return b.String()
}

// DefaultETagStoreEntries NewMemETagStore 缺省条目数上限（#289）。
// ETag 键空间随数据集自然增长（分页页码/时间过滤/cursor 都在键里），无界
// map 会让长驻轮询进程内存无界增长——缺省给 LRU 上限，需要更多条目的调用
// 方显式 WithETagStoreCapacity 放开。
const DefaultETagStoreEntries = 1024

// DefaultMaxETagBodySize NewMemETagStore 缺省单条缓存体上限（#289）。
// ETag 端点多是页面级 JSON 元数据（页面级缓存受益）；tarball/大 diff/大日志
// 这类大响应不值得驻留内存——超限条目直接不入存储，每次全量取。
const DefaultMaxETagBodySize = 1 << 20 // 1 MiB

// ETagStoreOption NewMemETagStore 的构造选项（仓规禁裸 Option）。
type ETagStoreOption func(*memETagStoreConfig)

type memETagStoreConfig struct {
	entries int
	maxBody int64
}

// WithETagStoreCapacity 覆盖条目数上限（LRU 淘汰）。entries < 1 是配置
// 错误，构造期 panic（仓规总则①）——容量 0 的 store 什么都不缓存，静默
// 生效等于「配了缓存却不工作」。
func WithETagStoreCapacity(entries int) ETagStoreOption {
	return func(c *memETagStoreConfig) {
		if entries < 1 {
			panic("httpjson: WithETagStoreCapacity 的 entries 必须 ≥ 1（fail-fast，仓规总则①）")
		}
		c.entries = entries
	}
}

// WithETagStoreMaxBodySize 覆盖单条缓存体上限（字节）。超限响应不入存储
// （每次全量取——ETag 缓存的受益面是页面级元数据，不是大载荷）。n < 1
// 是配置错误，构造期 panic（仓规总则①）；不提供「无上限」档——无界正是
// #289 要收口的缺陷，确需无界的调用方自行实现 ETagStore。
func WithETagStoreMaxBodySize(n int64) ETagStoreOption {
	return func(c *memETagStoreConfig) {
		if n < 1 {
			panic("httpjson: WithETagStoreMaxBodySize 必须 ≥ 1（无上限档不提供——#289 收口的就是无界；确需无界请自行实现 ETagStore）")
		}
		c.maxBody = n
	}
}

// NewMemETagStore 进程内 ETagStore（#289 有界化：LRU 条目上限 + 单条体积
// 上限，缺省 DefaultETagStoreEntries / DefaultMaxETagBodySize，经
// ETagStoreOption 覆盖）。body 以 string 持有（Go string 不可变，Get 返回
// 的副本与存储内部天然隔离）。
//
// 与旧版（无淘汰无上限的裸 map）的差异：条目超限按 LRU 淘汰最久未用者，
// 超大响应不入存储——被淘汰/未入存的 key 下次请求按无条件执行，行为退化为
// 「无缓存」，不是错误。
func NewMemETagStore(opts ...ETagStoreOption) ETagStore {
	cfg := memETagStoreConfig{entries: DefaultETagStoreEntries, maxBody: DefaultMaxETagBodySize}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &memETagStore{
		cfg:   cfg,
		m:     make(map[string]*list.Element, cfg.entries),
		order: list.New(), // front = 最旧（LRU 先淘汰），back = 最新
	}
}

type etagEntry struct {
	key, etag, body string
}

type memETagStore struct {
	cfg   memETagStoreConfig
	mu    sync.Mutex
	m     map[string]*list.Element
	order *list.List
}

func (s *memETagStore) Get(key string) (etag, body string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[key]
	if !ok {
		return "", "", false
	}
	s.order.MoveToBack(e) // LRU：读命中提升
	ent := e.Value.(*etagEntry)
	return ent.etag, ent.body, true
}

func (s *memETagStore) Put(key, etag, body string) {
	if int64(len(body)) > s.cfg.maxBody {
		return // 超限不入存储：每次全量取（不是错误，是无界的反面）
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.m[key]; ok {
		s.order.MoveToBack(e)
		ent := e.Value.(*etagEntry)
		ent.etag, ent.body = etag, body
		return
	}
	s.m[key] = s.order.PushBack(&etagEntry{key: key, etag: etag, body: body})
	for len(s.m) > s.cfg.entries {
		front := s.order.Front()
		if front == nil {
			break
		}
		delete(s.m, front.Value.(*etagEntry).key)
		s.order.Remove(front)
	}
}
