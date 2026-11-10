package webhook

// 时间戳防重放 HMAC 签名器（#325/#328）：TimestampHMACValidator 的发出侧
// 对偶——Gitee 家族「timestamp 参与签名 + 时间戳随报文发出」形态的生成端。
// message 组装（giteeMessage）与验签侧共享同一份，时间戳形态取毫秒（Gitee
// 官方 13 位口径；验签侧秒/毫秒自动识别，两侧无形态耦合）。
//
// 边界：只支持 header 载体——query 形态（timestamp/sign 拼在回调 URL 上）
// 是 URL 组装，不是 header 集，归调用方拼（Sign 的边界是不碰 URL）。

import (
	"crypto"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// TimestampHMACSigner TimestampHMACValidator 的发出侧对偶：签名值 =
// base64(hmac(algo, key=secret, message))，message 缺省按 Gitee 官方口径
// timestamp+"\n"+secret 组装（body 参与签名的平台用 SignerWithMessage
// 注入自己的组装，与验签侧 WithMessage 一一对应）。
type TimestampHMACSigner struct {
	secret  []byte
	algo    crypto.Hash
	tsName  string                                     // 时间戳 header 名
	sigName string                                     // 签名 header 名
	msg     func(timestamp string, body []byte) []byte // message 组装钩子（nil = Gitee 官方缺省形态）
	urlEnc  bool                                       // 签名值 URL 编码（Java URLEncoder 形态对偶）
	now     func() time.Time                           // 时钟注入（nil = time.Now）
}

// TimestampSignerOption TimestampHMACSigner 的构造选项。
type TimestampSignerOption func(*TimestampHMACSigner)

// SignerWithMessage 自定义签名 message 组装（与验签侧 WithMessage 同语义
// 同签名——两侧必须用同一个组装函数，否则验签端算出的 message 对不上）。
func SignerWithMessage(fn func(timestamp string, body []byte) []byte) TimestampSignerOption {
	return func(s *TimestampHMACSigner) {
		if fn == nil {
			panic("webhook: SignerWithMessage 的组装函数为 nil（fail-fast，仓规总则①）")
		}
		s.msg = fn
	}
}

// SignerWithURLEncoded 签名值 URL 编码（与验签侧 WithURLEncoded 同语义：
// Java URLEncoder 形态的接收端先解一层再比对——两侧必须同配）。
func SignerWithURLEncoded() TimestampSignerOption {
	return func(s *TimestampHMACSigner) { s.urlEnc = true }
}

// TimestampSignerWithClock 时钟注入（假时钟确定性测试；与验签侧 WithClock
// 同纪律。与泛型签名器的 SignerWithClock 是两个选项类型，名字带全量前缀消歧）。
func TimestampSignerWithClock(now func() time.Time) TimestampSignerOption {
	return func(s *TimestampHMACSigner) {
		if now == nil {
			panic("webhook: TimestampSignerWithClock 的时钟为 nil（fail-fast，仓规总则①）")
		}
		s.now = now
	}
}

// NewTimestampHMACSigner 构造时间戳防重放 HMAC 签名器——NewTimestampHMAC
// Validator 的逐参数对偶。构造校验同口径：secret 为空 panic fail-closed、
// algo 白名单、tsName/sigName 空串 panic、nil 选项 panic。
func NewTimestampHMACSigner(secret []byte, algo crypto.Hash, tsName, sigName string, opts ...TimestampSignerOption) *TimestampHMACSigner {
	if len(secret) == 0 {
		panic("webhook: Timestamp HMACSigner secret 为空——空密钥使 HMAC 签名对可预测载荷形同虚设，拒绝构造（fail-closed）")
	}
	switch algo {
	case crypto.SHA1, crypto.SHA224, crypto.SHA256, crypto.SHA384, crypto.SHA512:
	default:
		panic(fmt.Sprintf("webhook: 不支持的哈希算法 crypto.Hash(%d)——HMAC 家族限 SHA-1/224/256/384/512（fail-fast，仓规总则①）", uint(algo)))
	}
	if tsName == "" || sigName == "" {
		panic("webhook: Timestamp HMACSigner 的 tsName/sigName 为空串（fail-fast，仓规总则①）")
	}
	s := &TimestampHMACSigner{
		secret:  append([]byte(nil), secret...),
		algo:    algo,
		tsName:  tsName,
		sigName: sigName,
	}
	for _, opt := range opts {
		if opt == nil {
			panic("webhook: TimestampSignerOption 为 nil（fail-fast，仓规总则①）")
		}
		opt(s)
	}
	return s
}

// Sign 签出带时间戳的 header 集：时间戳头 = 当前毫秒时间戳（Gitee 官方
// 13 位口径），签名头 = base64(hmac(algo, key=secret, message))。message
// 缺省不含 body（与验签侧缺省一致——Gitee 口径密钥+时间戳即信物；body
// 参与签名的平台走 SignerWithMessage）。error 恒为 nil（本签名器无随机
// 成分），为与 Signer 接口同形保留。
func (s *TimestampHMACSigner) Sign(payload []byte) (http.Header, error) {
	now := s.now
	if now == nil {
		now = time.Now
	}
	ts := strconv.FormatInt(now().UnixMilli(), 10)
	sig := base64.StdEncoding.EncodeToString(hmacSum(s.algo, s.secret, s.message(ts, payload)))
	if s.urlEnc {
		sig = url.QueryEscape(sig)
	}
	h := make(http.Header, 2)
	h.Set(s.tsName, ts)
	h.Set(s.sigName, sig)
	return h, nil
}

func (s *TimestampHMACSigner) message(timestamp string, body []byte) []byte {
	if s.msg != nil {
		return s.msg(timestamp, body)
	}
	return giteeMessage(timestamp, s.secret)
}
