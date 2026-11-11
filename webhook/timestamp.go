package webhook

// 时间戳防重放 HMAC 验签器（#270）：Gitee 家族的「timestamp+secret 的
// HMAC-base64（含时间漂移上限校验防重放）」形态。签名值 = enc(hmac(algo,
// key=secret, message))，message 默认按 Gitee 官方口径 timestamp+"\n"+secret
// 组装（body 参与签名的平台用 WithMessage 注入自己的组装）。
//
// 多模消歧（Gitee 双模）：签名 header 缺席 → ErrMissingSignature（在
// Fallback 链里让位给密码模式）；签名在场面时间戳缺席 → 同样让位——
// 密码模式（明文令牌）的流量没有时间戳 header，按硬失败处理会把它错杀；
// 两者都在场 → 正常裁决：时间戳出窗 ErrStaleTimestamp、签名不符
// ErrBadSignature，均硬失败不降级（签名模式优先于密码模式）。

import (
	"crypto"
	"crypto/hmac"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultMaxDrift 时间戳容忍窗缺省（Gitee 官方文档：与请求调用时间误差
// 不能超过 1 小时）。
const DefaultMaxDrift = time.Hour

// TimestampHMACValidator 时间戳参与 HMAC 的防重放验签器。
type TimestampHMACValidator struct {
	secret  []byte
	algo    crypto.Hash
	tsName  string // 时间戳来源（header 名或 query 参数名，随 source）
	sigName string // 签名来源（header 名或 query 参数名）
	source  sourceKind
	msg     func(timestamp string, body []byte) []byte // message 组装钩子（nil = Gitee 官方缺省形态）
	drift   time.Duration
	now     func() time.Time // 时钟注入（假时钟确定性测试；nil = time.Now）
	urlEnc  bool             // 签名值经 URL 编码（Java URLEncoder 形态：先解一层再比对）
}

type sourceKind int

const (
	sourceHeader sourceKind = iota
	sourceQuery
)

// TimestampOption TimestampHMACValidator 的构造选项。
type TimestampOption func(*TimestampHMACValidator)

// WithMessage 自定义签名 message 组装（缺省 timestamp+"\n"+secret，Gitee
// 官方口径；body 参与签名的平台在此注入，如 timestamp+body）。
func WithMessage(fn func(timestamp string, body []byte) []byte) TimestampOption {
	return func(v *TimestampHMACValidator) {
		if fn == nil {
			panic("webhook: WithMessage 的组装函数为 nil（fail-fast，仓规总则①）")
		}
		v.msg = fn
	}
}

// WithMaxDrift 时间戳容忍窗（必须 >0，防重放窗不存在即失去本验签器的存在
// 意义——非法值 panic 而非静默关闭校验）。
func WithMaxDrift(d time.Duration) TimestampOption {
	return func(v *TimestampHMACValidator) {
		if d <= 0 {
			panic("webhook: WithMaxDrift 要求 >0——防重放窗不设防即失去本验签器存在意义（fail-fast，仓规总则①）")
		}
		v.drift = d
	}
}

// WithClock 时钟注入（假时钟确定性测试；与 cache.RunWithClock 同纪律）。
func WithClock(now func() time.Time) TimestampOption {
	return func(v *TimestampHMACValidator) {
		if now == nil {
			panic("webhook: WithClock 的时钟为 nil（fail-fast，仓规总则①）")
		}
		v.now = now
	}
}

// WithQuerySource 时间戳与签名改从 **URL query 参数**取（Gitee 签名形态
// 之一：timestamp 与 sign 拼在回调 URL 上）。缺省走 header。
func WithQuerySource() TimestampOption {
	return func(v *TimestampHMACValidator) { v.source = sourceQuery }
}

// WithURLEncoded 签名值经 URL 编码（Java URLEncoder.encode 形态：base64
// 的 +/= 被转 %2B/%2F/%3D）——比对前先做 PathUnescape 解一层。
func WithURLEncoded() TimestampOption {
	return func(v *TimestampHMACValidator) { v.urlEnc = true }
}

// NewTimestampHMACValidator 构造时间戳防重放 HMAC 验签器。
//
// 缺省口径 = Gitee 官方文档（WebHook 密钥验证和验证算法）：
//
//	message = timestamp + "\n" + secret
//	签名值  = base64(HMAC-SHA256(key=secret, message))
//	容忍窗  = DefaultMaxDrift（1h）；timestamp 自动识别秒/毫秒
//
// secret 为空 panic fail-closed（同 HMAC 家族）；tsName/sigName 空串
// panic。algo 白名单同 NewHMACValidator。
func NewTimestampHMACValidator(secret []byte, algo crypto.Hash, tsName, sigName string, opts ...TimestampOption) *TimestampHMACValidator {
	mustSecret(secret, "Timestamp HMAC")
	mustHMACAlgo(algo)
	if tsName == "" || sigName == "" {
		panic("webhook: Timestamp HMAC 的 tsName/sigName 为空串（fail-fast，仓规总则①）")
	}
	v := &TimestampHMACValidator{
		secret:  append([]byte(nil), secret...),
		algo:    algo,
		tsName:  tsName,
		sigName: sigName,
		drift:   DefaultMaxDrift,
	}
	for _, opt := range opts {
		if opt == nil {
			panic("webhook: TimestampOption 为 nil（fail-fast，仓规总则①）")
		}
		opt(v)
	}
	return v
}

// Validate 裁决：签名与时间戳都取到才进入判定，缺任一让位给密码模式
// （ErrMissingSignature）；都取到则时间窗、签名逐一判死。
func (v *TimestampHMACValidator) Validate(r *http.Request, body []byte) error {
	ts, tsOk := v.timestamp(r)
	sig, sigOk := v.signature(r)
	if !sigOk || !tsOk {
		// 签名/时间戳任一缺席 = 本模式不在场（密码模式流量没有时间戳
		// header）——让位，不判死。
		return ErrMissingSignature
	}
	t, ok := parseUnixTimestamp(ts)
	if !ok {
		return fmt.Errorf("%w: 时间戳 %q 无法解析", ErrStaleTimestamp, ts)
	}
	now := v.now
	if now == nil {
		now = time.Now
	}
	if d := now().Sub(t); d > v.drift || d < -v.drift {
		return fmt.Errorf("%w: 偏差 %v 超出容忍窗 %v", ErrStaleTimestamp, d, v.drift)
	}
	sig = strings.TrimSpace(sig)
	if v.urlEnc {
		if unescaped, err := url.PathUnescape(sig); err == nil {
			sig = unescaped
		}
	}
	got, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return fmt.Errorf("%w: 签名值 base64 解码失败", ErrBadSignature)
	}
	if !hmac.Equal(v.sum(v.message(ts, body)), got) {
		return ErrBadSignature
	}
	return nil
}

func (v *TimestampHMACValidator) message(timestamp string, body []byte) []byte {
	if v.msg != nil {
		return v.msg(timestamp, body)
	}
	return giteeMessage(timestamp, v.secret)
}

// giteeMessage 缺省 message 组装单源：Gitee 官方口径 timestamp + "\n" +
// secret——验证器与发端签名器（#325/#328）共用同一份，杜绝双源漂移。
func giteeMessage(timestamp string, secret []byte) []byte {
	return []byte(timestamp + "\n" + string(secret))
}

func (v *TimestampHMACValidator) sum(message []byte) []byte {
	return hmacSum(v.algo, v.secret, message)
}

func (v *TimestampHMACValidator) timestamp(r *http.Request) (string, bool) {
	if v.source == sourceQuery {
		ts := r.URL.Query().Get(v.tsName)
		return ts, ts != ""
	}
	ts := r.Header.Get(v.tsName)
	return ts, ts != ""
}

func (v *TimestampHMACValidator) signature(r *http.Request) (string, bool) {
	if v.source == sourceQuery {
		sig := r.URL.Query().Get(v.sigName)
		return sig, sig != ""
	}
	sig := r.Header.Get(v.sigName)
	return sig, sig != ""
}

// parseUnixTimestamp 解析时间戳并自动识别秒/毫秒（>=1e12 视为毫秒——
// 1e12 ms ≈ 2001-09，秒值到 1e12 是公元 33658 年，判据无歧义）。
func parseUnixTimestamp(s string) (time.Time, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}, false
	}
	if n >= 1e12 { // 毫秒
		return time.UnixMilli(n), true
	}
	return time.Unix(n, 0), true
}
