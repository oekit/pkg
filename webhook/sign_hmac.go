package webhook

// 发出侧签名器家族（#325/#328）：验证器家族的对偶——验证器怎么读、签名器
// 就怎么写。message 形态（hmacSum）、前缀/编码（encodeSignature，与
// decodeSignature 互为镜像）、平台 header 名（platforms.go 同一组常量）
// 全部与验签侧共享同一份实现，「发」与「收」的字段约定单源。
//
// 边界哲学与验签器一致（纯函数零状态、不读体不发送）：Sign 只产出可直接
// 套到出站请求的 header 集，HTTP 发送归调用方；平台自定义的额外头（如
// X-<Service>-Event）由调用方在返回的 http.Header 上继续补。

import (
	"crypto"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// 发出侧推荐约定头名（#328 设计点 2）：通用发端服务用推荐名（与 GitHub
// 的 X-GitHub-Event/X-GitHub-Delivery 同风格），平台历史名随平台构造器走。
// 验签侧没有这些头的既定约定，用泛型验证器按名取即可。
const (
	// DefaultEventHeader 事件类型头（Sign 在配置了事件类型时写入）。
	DefaultEventHeader = "X-Webhook-Event"
	// DefaultDeliveryHeader 幂等投递 ID 头（Sign 写入，值形如
	// <producer>-<event>-<uuidv4>，见 NewEventID）。
	DefaultDeliveryHeader = "X-Webhook-Delivery"
	// DefaultTimestampHeader 发出时间戳头（SignerWithTimestampHeader 开启
	// 时写入，unix 秒；收端可用 NewHMACValidator 同头自取或另行校验防重放窗）。
	DefaultTimestampHeader = "X-Webhook-Timestamp"
)

// Signer 发出侧签名器接口：Sign 产出已签 payload 的 header 集。实现必须
// 并发安全（共享 secret 只读）；error 仅在幂等 ID 随机源失败时非 nil
// ——发出方该失败要显式上抛，不能发出去一个空/重复 ID。
type Signer interface {
	Sign(payload []byte) (http.Header, error)
}

// HMACSigner HMACValidator 的发出侧对偶：签名值 = prefix + enc(hmac(algo,
// key=secret, message=payload))，与 NewHMACValidator 同语义面逐字节互逆
// ——同 secret 同选项下 Sign 产出的 header 喂给 Validate 必过（配对表
// 测试钉住）。可选附带事件类型头、幂等投递 ID 头与时间戳头（推荐约定，
// 见 SignerWithEvent / SignerWithTimestampHeader）。
type HMACSigner struct {
	secret    []byte
	algo      crypto.Hash
	header    string           // 签名 header 名
	prefix    string           // 签名值前缀（如 "sha256="，与 WithSignaturePrefix 同语义）
	useBase64 bool             // false = hex（缺省），true = base64（std）
	event     string           // 事件类型（空 = 不写事件/投递头）
	producer  string           // 幂等 ID 前缀段（空 = ID 退化为 <event>-<uuid>）
	newID     func() string    // 自定义投递 ID 生成（nil = NewEventID(producer, event)）
	tsHeader  string           // 时间戳 header 名（空 = 不打时间戳）
	now       func() time.Time // 时钟注入（nil = time.Now）
}

// HMACSignerOption HMACSigner 的构造选项（发出侧选项统一 SignerWith* 前缀，
// 与验签侧的 With* 一一对应成对）。
type HMACSignerOption func(*HMACSigner)

// SignerWithSignaturePrefix 签名值前缀（如 GitHub 的 "sha256="）——与验签
// 侧 WithSignaturePrefix 同语义：两侧必须同配，否则验签端剥不掉前缀
// （带前缀值喂给无前缀验证器会被裸解码拒绝）。
func SignerWithSignaturePrefix(prefix string) HMACSignerOption {
	return func(s *HMACSigner) {
		if prefix == "" {
			panic("webhook: SignerWithSignaturePrefix 前缀为空串（fail-fast，仓规总则①）")
		}
		s.prefix = prefix
	}
}

// SignerWithBase64 签名值按 base64（std）编码；缺省 hex——与验签侧
// WithBase64 同语义，两侧必须同配。
func SignerWithBase64() HMACSignerOption {
	return func(s *HMACSigner) { s.useBase64 = true }
}

// SignerWithEvent 声明事件类型：Sign 写入 DefaultEventHeader，并连带写入
// DefaultDeliveryHeader 幂等投递 ID。event 为空串是配置错误（事件类型是
// 幂等 ID 推荐形态的组成部分），panic。
func SignerWithEvent(event string) HMACSignerOption {
	return func(s *HMACSigner) {
		if event == "" {
			panic("webhook: SignerWithEvent 事件类型为空串（fail-fast，仓规总则①）")
		}
		s.event = event
	}
}

// SignerWithProducer 幂等投递 ID 升级为推荐完整形态 <producer>-<event>-
// <uuidv4>（#328 设计点 2）——跨服务对账时 producer 段区分「谁发的」，
// 同 event 名的服务不会撞 ID。producer 为空串是配置错误，panic。
func SignerWithProducer(producer string) HMACSignerOption {
	return func(s *HMACSigner) {
		if producer == "" {
			panic("webhook: SignerWithProducer 生产者名为空串（fail-fast，仓规总则①）")
		}
		s.producer = producer
	}
}

// SignerWithDeliveryID 自定义投递 ID 生成器（已有分布式 ID 的服务接入用；
// 缺省走 NewEventID 推荐形态）。fn 返回空串视为配置错误——空 ID 使消费端
// 幂等去重失效，Sign 时 panic 而非发出。
func SignerWithDeliveryID(fn func() string) HMACSignerOption {
	return func(s *HMACSigner) {
		if fn == nil {
			panic("webhook: SignerWithDeliveryID 的生成函数为 nil（fail-fast，仓规总则①）")
		}
		s.newID = func() string {
			id := fn()
			if id == "" {
				panic("webhook: SignerWithDeliveryID 的生成函数返回空串——空 ID 使消费端幂等去重失效（fail-fast，仓规总则①）")
			}
			return id
		}
	}
}

// SignerWithTimestampHeader 开启发出时间戳头并命名（unix 秒；推荐用
// DefaultTimestampHeader）。时间戳防重放窗的判定在收端（TimestampHMAC
// 家族或服务端自校验），发出侧职责是把时间戳按约定打出。name 为空串 panic。
func SignerWithTimestampHeader(name string) HMACSignerOption {
	return func(s *HMACSigner) {
		if name == "" {
			panic("webhook: SignerWithTimestampHeader 的 header 名为空串（fail-fast，仓规总则①）")
		}
		s.tsHeader = name
	}
}

// SignerWithClock 时钟注入（假时钟确定性测试；与 cache.RunWithClock 同纪律）。
func SignerWithClock(now func() time.Time) HMACSignerOption {
	return func(s *HMACSigner) {
		if now == nil {
			panic("webhook: SignerWithClock 的时钟为 nil（fail-fast，仓规总则①）")
		}
		s.now = now
	}
}

// NewHMACSigner 构造泛型 HMAC 签名器——NewHMACValidator 的逐参数对偶。
//
// 构造校验与验签器同口径：secret 为空 panic fail-closed（发出的签名没有
// 密钥等于裸奔，宁可不发）；algo 白名单同 NewHMACValidator；header 空串
// panic；nil 选项 panic。
func NewHMACSigner(secret []byte, algo crypto.Hash, header string, opts ...HMACSignerOption) *HMACSigner {
	mustSecret(secret, "HMACSigner")
	mustHMACAlgo(algo)
	if header == "" {
		panic("webhook: HMACSigner 的 header 为空串（fail-fast，仓规总则①）")
	}
	s := &HMACSigner{
		secret: append([]byte(nil), secret...),
		algo:   algo,
		header: header,
	}
	for _, opt := range opts {
		if opt == nil {
			panic("webhook: HMACSignerOption 为 nil（fail-fast，仓规总则①）")
		}
		opt(s)
	}
	return s
}

// Sign 签出已签 payload 的 header 集：
//
//   - 签名头（构造参数 header）：prefix + enc(hmac(algo, key=secret,
//     message=payload))——与 NewHMACValidator 同选项下逐字节互逆；
//   - 时间戳头（SignerWithTimestampHeader 开启时）：unix 秒；
//   - 事件头 + 幂等投递 ID 头（SignerWithEvent 配置时）：DefaultEventHeader /
//     DefaultDeliveryHeader，ID 走 NewEventID 推荐形态或 SignerWithDeliveryID。
//
// 返回的 header 可直接套到出站请求（h := signed.Header(); req.Header = h
// 或逐个 req.Header.Set），调用方可在其上继续追加自有头。error 仅在幂等
// ID 的随机源失败（crypto/rand 读失败）时非 nil。
func (s *HMACSigner) Sign(payload []byte) (http.Header, error) {
	h := make(http.Header, 3)
	h.Set(s.header, encodeSignature(hmacSum(s.algo, s.secret, payload), s.prefix, s.useBase64))
	if s.tsHeader != "" {
		now := s.now
		if now == nil {
			now = time.Now
		}
		h.Set(s.tsHeader, strconv.FormatInt(now().Unix(), 10))
	}
	if s.event != "" {
		h.Set(DefaultEventHeader, s.event)
		id, err := s.deliveryID()
		if err != nil {
			return nil, err
		}
		h.Set(DefaultDeliveryHeader, id)
	}
	return h, nil
}

func (s *HMACSigner) deliveryID() (string, error) {
	if s.newID != nil {
		return s.newID(), nil
	}
	return NewEventID(s.producer, s.event)
}

// NewEventID 推荐幂等事件 ID 形态（#328 设计点 2）：<producer>-<event>-
// <uuidv4>（空段跳过——没有 producer 就是 <event>-<uuid>）。uuidv4 取自
// crypto/rand（pkg 零第三方依赖，不引 uuid 库）。随机源失败显式上抛
// （仓规总则③）——降级成时间戳 ID 会让重试窗口内的重发无法被识别为同一次投递。
func NewEventID(producer, event string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("webhook: 生成幂等事件 ID 的随机源失败: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx
	id := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	switch {
	case producer != "" && event != "":
		return producer + "-" + event + "-" + id, nil
	case producer != "":
		return producer + "-" + id, nil
	case event != "":
		return event + "-" + id, nil
	}
	return id, nil
}

// SignHMAC 发端 helper（#325）：与 NewHMACValidator 同语义面的纯函数形态
// ——返回可直接塞 header 的签名值，前缀/编码由同一组 HMACOption 驱动（与
// 验签器共用选项值，无需另记一套）。测试夹具与一次性签名用；要附带
// 时间戳/事件/幂等 ID 的完整发出约定用 HMACSigner。
func SignHMAC(secret []byte, algo crypto.Hash, payload []byte, opts ...HMACOption) string {
	// 约定载体：header 名在本 helper 无意义（只产出值），用合法占位名复用
	// 验签器的同一构造校验与前缀/编码选项——「验证器怎么读、这里就怎么写」
	// 由同一份代码保证，而非两处各写一遍。
	v := NewHMACValidator(secret, algo, signCarrierHeader, opts...)
	return encodeSignature(hmacSum(algo, secret, payload), v.prefix, v.useBase64)
}

// signCarrierHeader SignHMAC 的约定载体占位 header 名（不产出、仅承载选项）。
const signCarrierHeader = "X-Oekit-Signature"

// SignTimestampHMAC 发端 helper（#325）：与 NewTimestampHMACValidator 缺省
// 口径同语义面——Gitee 形态 base64(hmac(algo, key=secret,
// msg=ts+"\n"+secret))。与 giteeMessage 共享 message 组装，天然不漂移。
func SignTimestampHMAC(secret []byte, algo crypto.Hash, ts string) string {
	mustSecret(secret, "SignTimestampHMAC")
	mustHMACAlgo(algo)
	return base64.StdEncoding.EncodeToString(hmacSum(algo, secret, giteeMessage(ts, secret)))
}
