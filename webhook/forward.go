package webhook

// 游标式 at-least-once 批量转发器（#482）：审计/事件外发到 SIEM 的
// 「取批 → 全 sink 投递 → 推进游标」不变量单源。收编自业务仓审计服务的
// ForwardOnce/sendWebhook/sendSyslog 骨架——任何要把审计流/事件流外发
// 到 SIEM、对端 webhook 或 syslog 守护进程的下游，都不必再重写游标语义。
//
// 机制只收不变量，两头的存储与数据由宿主注入：游标存储（如 bq_setting
// 表）经 CursorStore、批源（如 WHERE seq > ? ORDER BY seq LIMIT ? 的查询）
// 经 BatchSource，本包不假设任何存储形态。
//
// 用法：
//
//	fwd := webhook.NewForwarder(store, source, []webhook.ForwardSink{
//		webhook.NewWebhookSink(url, webhook.WithWebhookSigner(signer)),
//		webhook.NewSyslogSink("tcp", "siem:514", host, "bianque"),
//	})
//	n, err := fwd.ForwardOnce(ctx) // 宿主自持轮询节奏（ticker/驱动）

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// 转发面错误哨兵（errors.Is 判定）。
var (
	// ErrDeliveryFailed sink 投递失败（HTTP 非 2xx / 网络错 / 签名失败 /
	// syslog 连接或写帧失败）——游标不推进，整批下轮重发。
	ErrDeliveryFailed = errors.New("webhook: 转发投递失败")
	// ErrInvalidForwardPayload 批内载荷非法（空或非合法 JSON）——毒批
	// fail-loud 卡住游标，绝不静默跳过（跳过=有意丢数据，是宿主的决策
	// 不是转发器的；宿主需在源头保证产出质量或显式改游标绕过）。
	ErrInvalidForwardPayload = errors.New("webhook: 转发载荷非法")
	// ErrBadForwardBatch 批序非法（Seq 非严格递增或未越过游标）——源侧
	// 契约破坏，硬失败不推进（推进非最大 Seq 会静默丢事件）。
	ErrBadForwardBatch = errors.New("webhook: 转发批次序非法")
)

// ForwardEvent 转发批次里的一条事件。
type ForwardEvent struct {
	// Seq 单调递增游标序号。源侧契约：FetchAfter 只回 Seq > cursor 的
	// 事件且按 Seq 升序（转发器校验，破坏即 ErrBadForwardBatch 不推进
	// ——序号错乱时推进「批尾」会静默丢事件，宁可不发）。Seq 不上
	// webhook 线格式：幂等去重键在载荷自身的稳定 ID（SIEM 侧按 id 去重）。
	Seq uint64
	// Payload 已编码的 JSON 载荷字节（审计 wire event 等；两 sink 的
	// 共同输入：webhook 作 JSON 数组元素原样嵌入，syslog 作 MSG 文本）。
	Payload []byte
}

// CursorStore 游标存储接口：宿主注入（业务仓的 bq_setting 表、任何持久
// KV）。实现必须并发安全，或由宿主保证串行调用。契约：无游标时
// LoadCursor 返回 0（首次全量外发起点）而非 error——「还没有」与「读
// 失败」是两件事，混报会把首启伪装成故障。
type CursorStore interface {
	LoadCursor(ctx context.Context) (uint64, error)
	SaveCursor(ctx context.Context, cursor uint64) error
}

// MemoryCursorStore 内存游标存储：契约测试与启动期骨架用；进程重启即归
// 零（从头重发——at-least-once 的重发对幂等接收端是安全的，见
// Forwarder 幂等建议），生产用宿主持久化实现。
type MemoryCursorStore struct {
	mu  sync.Mutex
	cur uint64
}

// NewMemoryCursorStore 构造内存游标存储（初值 0）。
func NewMemoryCursorStore() *MemoryCursorStore { return &MemoryCursorStore{} }

// LoadCursor 实现 CursorStore。
func (s *MemoryCursorStore) LoadCursor(_ context.Context) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur, nil
}

// SaveCursor 实现 CursorStore。
func (s *MemoryCursorStore) SaveCursor(_ context.Context, cursor uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur = cursor
	return nil
}

// BatchSource 批源接口：按游标取一批待外发事件。契约（Forwarder 校验，
// 破坏即 ErrBadForwardBatch 硬失败）：只返回 Seq > cursor 的事件、按 Seq
// 严格升序、至多 limit 条。宿主查询形如
// WHERE seq > ? ORDER BY seq ASC LIMIT ?。
type BatchSource interface {
	FetchAfter(ctx context.Context, cursor uint64, limit int) ([]ForwardEvent, error)
}

// BatchSourceFunc 函数适配器——让普通查询函数当批源。
type BatchSourceFunc func(ctx context.Context, cursor uint64, limit int) ([]ForwardEvent, error)

// FetchAfter 实现 BatchSource。
func (f BatchSourceFunc) FetchAfter(ctx context.Context, cursor uint64, limit int) ([]ForwardEvent, error) {
	return f(ctx, cursor, limit)
}

// ForwardSink 转发 sink：把一批事件投递到一个下游。实现约定：
//
//   - 必须容忍整批重投（转发器的 at-least-once 语义——同一批可能被重复
//     调用多次），接收端幂等或 sink 自身去重；
//   - Forward 要么整体成功返回 nil，要么返回非 nil error——「部分成功」
//     不可表达（error 即「本 sink 本批未完成投递」），已送达的部分由
//     重投覆盖；
//   - 实现必须并发安全，或由宿主保证串行调用。
type ForwardSink interface {
	Forward(ctx context.Context, batch []ForwardEvent) error
}

// DefaultForwardBatchLimit 每轮最大取批条数缺省值——单轮外发的扇出上限
// （批越大重投窗口越大，接收端幂等窗口需与之匹配）。
const DefaultForwardBatchLimit = 500

// Forwarder 游标式 at-least-once 批量转发器。
//
// at-least-once 语义（宁重不丢）与幂等建议：
//
//   - 推进时机：游标只在**全部 sink 都成功**且**游标落库成功**后才推进
//     到本批最大 Seq；任何一步失败游标不动，下一轮重新取同窗重发整批；
//   - 重投边界：最小重投单位是一整批（不是单条）——sink A 成功、B 失败
//     的那轮，A 会在下轮收到重复事件，这是有意为之；游标落库失败
//     （SaveCursor 出错）同样下轮重发，先投递后记账的顺序保证「丢」只
//     可能表现为「重」，绝不反向丢失；
//   - 幂等建议：接收端（SIEM / 对端 webhook 服务）按载荷内的稳定事件
//     ID 去重（推荐 EventEnvelope.ID / 载荷自有 id；发出侧用 HMACSigner
//     时签名头自带 DefaultDeliveryHeader 幂等投递 ID，可按其去重），
//     重复投递在解包/落库之前丢弃；
//   - 顺序保证：重投只重复不乱序——批间按游标窗口升序、批内按 Seq
//     升序；接收端不得依赖「恰好一次」。
type Forwarder struct {
	store CursorStore
	src   BatchSource
	sinks []ForwardSink
	limit int
}

// ForwarderOption Forwarder 的构造选项。
type ForwarderOption func(*Forwarder)

// WithForwardBatchLimit 每轮最大取批条数（透传给 BatchSource 的 limit）。
// n <= 0 是构造期配置错误，panic（fail-fast，仓规总则①）。
func WithForwardBatchLimit(n int) ForwarderOption {
	return func(f *Forwarder) {
		if n <= 0 {
			panic("webhook: WithForwardBatchLimit 的批上限必须为正数（fail-fast，仓规总则①）")
		}
		f.limit = n
	}
}

// NewForwarder 构造转发器：store/source 为 nil、sinks 为空或含 nil、
// options 含 nil 均是构造期配置错误，panic（fail-fast，仓规总则①）。
func NewForwarder(store CursorStore, src BatchSource, sinks []ForwardSink, opts ...ForwarderOption) *Forwarder {
	if store == nil {
		panic("webhook: Forwarder 的游标存储为 nil（fail-fast，仓规总则①）")
	}
	if src == nil {
		panic("webhook: Forwarder 的批源为 nil（fail-fast，仓规总则①）")
	}
	if len(sinks) == 0 {
		panic("webhook: Forwarder 需要至少一个 sink（fail-fast，仓规总则①）")
	}
	for i, s := range sinks {
		if s == nil {
			panic(fmt.Sprintf("webhook: Forwarder sink #%d 为 nil（fail-fast，仓规总则①）", i))
		}
	}
	f := &Forwarder{store: store, src: src, sinks: append([]ForwardSink(nil), sinks...), limit: DefaultForwardBatchLimit}
	for _, opt := range opts {
		if opt == nil {
			panic("webhook: ForwarderOption 为 nil（fail-fast，仓规总则①）")
		}
		opt(f)
	}
	return f
}

// ForwardOnce 外发一轮：加载游标 → 取批 → 全 sink 依次投递 → 推进游标。
//
// 返回值：成功推进时为本批条数（>0）；空批返回 (0, nil)——空批不算
// 错误，调用方按自己的轮询节奏调度下一轮。任何失败游标都不推进（语义
// 见 Forwarder 文档），返回的 error 可用 ErrDeliveryFailed /
// ErrInvalidForwardPayload / ErrBadForwardBatch 哨兵分类。
func (f *Forwarder) ForwardOnce(ctx context.Context) (int, error) {
	cursor, err := f.store.LoadCursor(ctx)
	if err != nil {
		return 0, fmt.Errorf("webhook: 加载转发游标失败: %w", err)
	}
	batch, err := f.src.FetchAfter(ctx, cursor, f.limit)
	if err != nil {
		return 0, fmt.Errorf("webhook: 取转发批失败（游标 %d 之后）: %w", cursor, err)
	}
	if len(batch) == 0 {
		return 0, nil
	}
	prev := cursor
	for i, ev := range batch {
		if len(ev.Payload) == 0 || !json.Valid(ev.Payload) {
			return 0, fmt.Errorf("%w: 批 #%d（Seq=%d）为空或不是合法 JSON——毒批卡住游标属有意设计，宿主须在源头修复或显式绕过", ErrInvalidForwardPayload, i, ev.Seq)
		}
		if ev.Seq <= prev {
			return 0, fmt.Errorf("%w: 批 #%d（Seq=%d）未严格越过前序 %d（游标 %d）——源侧必须只回 Seq>cursor 的严格升序批", ErrBadForwardBatch, i, ev.Seq, prev, cursor)
		}
		prev = ev.Seq
	}
	for _, s := range f.sinks {
		if err := s.Forward(ctx, batch); err != nil {
			return 0, fmt.Errorf("%w: %w（游标 %d 不推进，整批 %d 条下轮重发）", ErrDeliveryFailed, err, cursor, len(batch))
		}
	}
	last := batch[len(batch)-1].Seq
	if err := f.store.SaveCursor(ctx, last); err != nil {
		return 0, fmt.Errorf("webhook: 保存转发游标 %d 失败: %w（sink 已投递，下轮将整批重发 %d 条——at-least-once，接收端按载荷 ID 去重）", last, err, len(batch))
	}
	return len(batch), nil
}

// DefaultWebhookTimeout webhook sink 单次投递整体超时缺省值——探测类
// 上限内（仓规「超时默认值」）；断链不重试（重试归转发循环：游标不动
// 下轮自然重发）。
const DefaultWebhookTimeout = 10 * time.Second

// sinkDrainLimit 响应体排空上限——连接复用要读完响应体，排空量有上限
// 防「对端用超大响应体拖住调用」（与 httpjson.drainLimit 同纪律）。
const sinkDrainLimit = 64 << 10

// WebhookSink webhook 转发 sink：整批一个 POST，body 为载荷 JSON 数组
// （json.RawMessage 原样嵌入，不重排序不重编码），Content-Type
// application/json；可选 HMAC 签名（复用发侧 Signer 家族——签的就是
// 发出的 body 字节，与收侧验证器同约定互逆）。
type WebhookSink struct {
	url    string
	signer Signer
	client *http.Client
}

// WebhookSinkOption WebhookSink 的构造选项。
type WebhookSinkOption func(*WebhookSink)

// WithWebhookSigner 配置发侧签名器：Sign 的输入即发出的 body 字节——
// 收端用 NewHMACValidator 系（同 secret 同选项）验签必过（配对约定见
// 包文档发出侧↔收侧配对表）。nil 是配置错误，panic。
func WithWebhookSigner(s Signer) WebhookSinkOption {
	return func(w *WebhookSink) {
		if s == nil {
			panic("webhook: WithWebhookSigner 的签名器为 nil（fail-fast，仓规总则①）")
		}
		w.signer = s
	}
}

// WithWebhookClient 自定义 HTTP 客户端（代理/连接池/细粒度超时策略归
// 调用方）。nil 是配置错误，panic；不配置时用缺省客户端（整体超时
// DefaultWebhookTimeout）。
func WithWebhookClient(c *http.Client) WebhookSinkOption {
	return func(w *WebhookSink) {
		if c == nil {
			panic("webhook: WithWebhookClient 的 HTTP 客户端为 nil（fail-fast，仓规总则①）")
		}
		w.client = c
	}
}

// NewWebhookSink 构造 webhook 转发 sink。url 为空串是配置错误，panic
// （fail-fast，仓规总则①）。
func NewWebhookSink(url string, opts ...WebhookSinkOption) *WebhookSink {
	if url == "" {
		panic("webhook: WebhookSink 的 URL 为空串（fail-fast，仓规总则①）")
	}
	w := &WebhookSink{url: url, client: &http.Client{Timeout: DefaultWebhookTimeout}}
	for _, opt := range opts {
		if opt == nil {
			panic("webhook: WebhookSinkOption 为 nil（fail-fast，仓规总则①）")
		}
		opt(w)
	}
	return w
}

// Forward 整批一个 POST（实现 ForwardSink）：body = 载荷 JSON 数组（元素
// 字节原样嵌入），Content-Type application/json；配置了签名器时按 Sign
// 产出套签名/事件/投递 ID 头。2xx 视为投递成功；非 2xx 与网络错均返回
// 包 ErrDeliveryFailed 的错误——游标不推进，整批下轮重发。
func (w *WebhookSink) Forward(ctx context.Context, batch []ForwardEvent) error {
	payloads := make([]json.RawMessage, 0, len(batch))
	for i, ev := range batch {
		if len(ev.Payload) == 0 || !json.Valid(ev.Payload) {
			return fmt.Errorf("%w: 批 #%d（Seq=%d）为空或不是合法 JSON", ErrInvalidForwardPayload, i, ev.Seq)
		}
		payloads = append(payloads, json.RawMessage(ev.Payload))
	}
	body, err := json.Marshal(payloads)
	if err != nil {
		return fmt.Errorf("%w: 批载荷编组失败: %w", ErrInvalidForwardPayload, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: 构造 POST %s 失败: %w", ErrDeliveryFailed, w.url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if w.signer != nil {
		hdr, sigErr := w.signer.Sign(body)
		if sigErr != nil {
			return fmt.Errorf("%w: 签名失败: %w", ErrDeliveryFailed, sigErr)
		}
		for k, vs := range hdr {
			req.Header[k] = vs
		}
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: POST %s: %w", ErrDeliveryFailed, w.url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	// 排空响应体让连接回空闲池（有上限防超大响应体拖住调用）
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, sinkDrainLimit))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%w: POST %s: HTTP %d", ErrDeliveryFailed, w.url, resp.StatusCode)
	}
	return nil
}
