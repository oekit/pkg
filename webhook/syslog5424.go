package webhook

// RFC5424 syslog 行格式化 + syslog 转发 sink（#482）：审计/事件外发的
// 第二条 sink 面。三域此前无任何 syslog/5424 实现（grep 仅 go.sum 命中），
// PRI 计算、NILVALUE、时间戳形态、分帧这些细节在业务仓里是手写的一次性
// 拼接——单源在此。
//
// 边界（#482 (e) 首版承诺）：只做「PRI + 版本 + 时间 + 主机 + APP-NAME +
// MSG」的一帧行文本与逐轮连接投递；STRUCTURED-DATA（SD）恒为 NILVALUE
// 不进首版，octet-counting 分帧、TLS、rsyslog 中继重试等传输深水区归
// 调用方或后续版本。

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// Facility RFC5424 PRI 的 facility 段（0–23，数值即协议值）。
type Facility uint8

// 标准 facility 常量（RFC5424 §6.2.1；12–15 的保留段无常量，数值字面量
// 自行指定）。
const (
	FacilityKernel   Facility = 0  // kernel messages
	FacilityUser     Facility = 1  // user-level messages
	FacilityMail     Facility = 2  // mail system
	FacilityDaemon   Facility = 3  // system daemons
	FacilityAuth     Facility = 4  // security/authorization messages
	FacilitySyslog   Facility = 5  // syslogd 内部消息
	FacilityLPR      Facility = 6  // line printer subsystem
	FacilityNews     Facility = 7  // network news subsystem
	FacilityUUCP     Facility = 8  // UUCP subsystem
	FacilityCron     Facility = 9  // clock daemon
	FacilityAuthPriv Facility = 10 // security/authorization（私有口径——审计/SIEM 外发常用）
	FacilityFTP      Facility = 11 // ftp daemon
	FacilityLocal0   Facility = 16 // 本地用途 0
	FacilityLocal1   Facility = 17
	FacilityLocal2   Facility = 18
	FacilityLocal3   Facility = 19
	FacilityLocal4   Facility = 20
	FacilityLocal5   Facility = 21
	FacilityLocal6   Facility = 22
	FacilityLocal7   Facility = 23
)

// Severity RFC5424 PRI 的 severity 段（0–7，数值即协议值；0 最重）。
type Severity uint8

// 标准 severity 常量（RFC5424 §6.2.1）。
const (
	SeverityEmergency     Severity = 0 // system is unusable
	SeverityAlert         Severity = 1 // action must be taken immediately
	SeverityCritical      Severity = 2 // critical conditions
	SeverityError         Severity = 3 // error conditions
	SeverityWarning       Severity = 4 // warning conditions
	SeverityNotice        Severity = 5 // normal but significant
	SeverityInformational Severity = 6 // informational messages
	SeverityDebug         Severity = 7 // debug-level messages
)

// RFC5424Message 一帧 RFC5424 行的字段。空串字段与零值 Timestamp 由
// FormatRFC5424 归一为 NILVALUE "-"（时间未知也是合法 RFC5424 帧）。
type RFC5424Message struct {
	Facility  Facility
	Severity  Severity
	Timestamp time.Time
	Hostname  string
	AppName   string
	ProcID    string
	MsgID     string
	Message   string
}

// rfc5424TimestampLayout RFC5424 TIMESTAMP 形态：RFC3339 毫秒精度、
// UTC 归一（时区混排会让 SIEM 侧解析回归分叉，单源统一 UTC 挂 "Z"）。
const rfc5424TimestampLayout = "2006-01-02T15:04:05.000Z"

// rfc5424NilValue RFC5424 NILVALUE（§6.3：字段缺失/未知时的占位）。
const rfc5424NilValue = "-"

// FormatRFC5424 构造一帧 RFC5424 行文本（不含行尾换行——帧终止符归
// sink 的传输面）：
//
//	<PRI>1 TIMESTAMP HOSTNAME APP-NAME PROCID MSGID - MSG
//
// PRI = facility*8 + severity（issue 原型 <86> 即 authpriv(10).info(6)）。
// 规则：
//
//   - facility/severity 越界 panic——PRI 是协议值，越界帧会被 SIEM 解析
//     面拒收，产出期就死好过发出垃圾（fail-fast，仓规总则①）；
//   - 空串 Hostname/AppName/ProcID/MsgID 与空 Message → NILVALUE "-"；
//   - Timestamp 归一 UTC 毫秒精度；零值 → NILVALUE "-"；
//   - Message 内 \r \n 替换为空格：本 sink 用「非透明分帧」（行 = 一帧，
//     帧尾 \n 由 sink 补），MSG 内换行会撕开分帧——有损替换是有意取舍
//     （octet-counting 分帧不在首版；要无损请用 WebhookSink 的 JSON 面）。
func FormatRFC5424(m RFC5424Message) string {
	if m.Facility > 23 {
		panic(fmt.Sprintf("webhook: FormatRFC5424 的 facility %d 越界（0–23，fail-fast，仓规总则①）", m.Facility))
	}
	if m.Severity > 7 {
		panic(fmt.Sprintf("webhook: FormatRFC5424 的 severity %d 越界（0–7，fail-fast，仓规总则①）", m.Severity))
	}
	pri := int(m.Facility)*8 + int(m.Severity)
	ts := rfc5424NilValue
	if !m.Timestamp.IsZero() {
		ts = m.Timestamp.UTC().Format(rfc5424TimestampLayout)
	}
	msg := m.Message
	if msg == "" {
		msg = rfc5424NilValue
	} else {
		msg = strings.ReplaceAll(msg, "\r", " ")
		msg = strings.ReplaceAll(msg, "\n", " ")
	}
	return fmt.Sprintf("<%d>1 %s %s %s %s %s %s %s",
		pri, ts, nilOr(m.Hostname), nilOr(m.AppName), nilOr(m.ProcID), nilOr(m.MsgID),
		rfc5424NilValue, msg) // STRUCTURED-DATA 恒 NILVALUE（首版承诺）
}

// nilOr 空串归 NILVALUE。
func nilOr(s string) string {
	if s == "" {
		return rfc5424NilValue
	}
	return s
}

// DefaultSyslogDialTimeout syslog 连接超时缺省值——5s（issue 原型
// DialTimeout 口径，探测类上限内）。断链不是终态也不重试连接：连接复用
// 是性能优化不是正确性要件，下一轮转发循环自然重试。
const DefaultSyslogDialTimeout = 5 * time.Second

// DefaultSyslogWriteTimeout 写帧整体死线缺省值——30s（治理类上限内）：
// 单批至多数百帧，慢链路重传需要余量，但不得挂死转发循环。
const DefaultSyslogWriteTimeout = 30 * time.Second

// SyslogSink RFC5424 syslog 转发 sink（实现 ForwardSink）：每次 Forward
// 现连现写现断——逐轮连接、断链下轮重试（SIEM 前置 rsyslog/syslog-ng 的
// 常态是断链重启，长连接保活反而把「对端没了」藏成写超时）。每条事件
// 一帧，MSG = 载荷 JSON 原文，帧尾 \n（非透明分帧；UDP 每帧一个数据报）。
type SyslogSink struct {
	network      string
	addr         string
	dialTimeout  time.Duration
	writeTimeout time.Duration
	hostname     string
	appName      string
	procID       string
	msgID        string
	facility     Facility
	severity     Severity
	now          func() time.Time
}

// SyslogSinkOption SyslogSink 的构造选项。
type SyslogSinkOption func(*SyslogSink)

// WithSyslogFacility PRI facility 段（缺省 FacilityAuthPriv——审计/SIEM
// 外发常用口径，对应 PRI 80–87）。越界 panic。
func WithSyslogFacility(f Facility) SyslogSinkOption {
	return func(s *SyslogSink) {
		if f > 23 {
			panic(fmt.Sprintf("webhook: WithSyslogFacility 的 facility %d 越界（0–23，fail-fast，仓规总则①）", f))
		}
		s.facility = f
	}
}

// WithSyslogSeverity PRI severity 段（缺省 SeverityInformational——issue
// 原型 <86> 即 authpriv.info）。越界 panic。
func WithSyslogSeverity(v Severity) SyslogSinkOption {
	return func(s *SyslogSink) {
		if v > 7 {
			panic(fmt.Sprintf("webhook: WithSyslogSeverity 的 severity %d 越界（0–7，fail-fast，仓规总则①）", v))
		}
		s.severity = v
	}
}

// WithSyslogProcID PROCID 段（缺省 NILVALUE；如进程 PID、实例号）。
func WithSyslogProcID(id string) SyslogSinkOption {
	return func(s *SyslogSink) { s.procID = id }
}

// WithSyslogMsgID MSGID 段（缺省 NILVALUE；如事件类型名）。
func WithSyslogMsgID(id string) SyslogSinkOption {
	return func(s *SyslogSink) { s.msgID = id }
}

// WithSyslogDialTimeout 连接超时。d <= 0 panic（用缺省值请不配置本选项，
// nil 参数=回退默认是 nil 选项的事，显式给了就必须合法）。
func WithSyslogDialTimeout(d time.Duration) SyslogSinkOption {
	return func(s *SyslogSink) {
		if d <= 0 {
			panic("webhook: WithSyslogDialTimeout 的超时必须为正（fail-fast，仓规总则①）")
		}
		s.dialTimeout = d
	}
}

// WithSyslogWriteTimeout 写帧整体死线。d <= 0 panic。
func WithSyslogWriteTimeout(d time.Duration) SyslogSinkOption {
	return func(s *SyslogSink) {
		if d <= 0 {
			panic("webhook: WithSyslogWriteTimeout 的死线必须为正（fail-fast，仓规总则①）")
		}
		s.writeTimeout = d
	}
}

// WithSyslogClock 时钟注入（假时钟确定性测试；与 cache.RunWithClock 同纪律）。
func WithSyslogClock(now func() time.Time) SyslogSinkOption {
	return func(s *SyslogSink) {
		if now == nil {
			panic("webhook: WithSyslogClock 的时钟为 nil（fail-fast，仓规总则①）")
		}
		s.now = now
	}
}

// NewSyslogSink 构造 syslog 转发 sink。addr 为空串是配置错误 panic；
// network 为空串回退 "tcp"（仓规总则②：空参数=显式回退默认值）。hostname
// 空串与 appName 空串合法（帧内归 NILVALUE，但审计外发建议都给全——
// SIEM 侧按 host/app 分面检索）。
func NewSyslogSink(network, addr, hostname, appName string, opts ...SyslogSinkOption) *SyslogSink {
	if addr == "" {
		panic("webhook: SyslogSink 的地址为空串（fail-fast，仓规总则①）")
	}
	s := &SyslogSink{
		network:      network,
		addr:         addr,
		dialTimeout:  DefaultSyslogDialTimeout,
		writeTimeout: DefaultSyslogWriteTimeout,
		hostname:     hostname,
		appName:      appName,
		facility:     FacilityAuthPriv,
		severity:     SeverityInformational,
	}
	if s.network == "" {
		s.network = "tcp"
	}
	for _, opt := range opts {
		if opt == nil {
			panic("webhook: SyslogSinkOption 为 nil（fail-fast，仓规总则①）")
		}
		opt(s)
	}
	return s
}

// Forward 每条事件一帧 RFC5424 逐帧写出（实现 ForwardSink）。任一帧写
// 失败即返回包 ErrDeliveryFailed 的错误——已写出的帧会在下轮整批重发
// （at-least-once，接收端按载荷 ID 去重）；空载荷返回包
// ErrInvalidForwardPayload 的错误（空帧是源侧 bug，不产出占位垃圾）。
func (s *SyslogSink) Forward(ctx context.Context, batch []ForwardEvent) error {
	d := net.Dialer{Timeout: s.dialTimeout}
	conn, err := d.DialContext(ctx, s.network, s.addr)
	if err != nil {
		return fmt.Errorf("%w: dial %s %s: %w", ErrDeliveryFailed, s.network, s.addr, err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(s.writeTimeout)); err != nil {
		return fmt.Errorf("%w: 设置写死线失败: %w", ErrDeliveryFailed, err)
	}
	now := s.now
	if now == nil {
		now = time.Now
	}
	for i, ev := range batch {
		if len(ev.Payload) == 0 {
			return fmt.Errorf("%w: 批 #%d（Seq=%d）载荷为空", ErrInvalidForwardPayload, i, ev.Seq)
		}
		frame := FormatRFC5424(RFC5424Message{
			Facility:  s.facility,
			Severity:  s.severity,
			Timestamp: now(),
			Hostname:  s.hostname,
			AppName:   s.appName,
			ProcID:    s.procID,
			MsgID:     s.msgID,
			Message:   string(ev.Payload),
		})
		if _, err := fmt.Fprintf(conn, "%s\n", frame); err != nil {
			return fmt.Errorf("%w: 写帧失败（批 #%d，Seq=%d）: %w", ErrDeliveryFailed, i, ev.Seq, err)
		}
	}
	return nil
}
