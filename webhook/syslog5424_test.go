package webhook

// RFC5424 形态与 syslog sink 测试（#482）：帧格式按字节钉住（PRI/版本/
// 时间戳/主机/APP-NAME/PROCID/MSGID/NILVALUE/MSG）、PRI 计算、换行净化、
// 越界 fail-fast、真 socket 投递与断链失败形态。

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestFormatRFC5424_FramePinned(t *testing.T) {
	// issue 原型 <86>：authpriv(10).info(6) = PRI 86；固定时钟钉死时间戳
	m := RFC5424Message{
		Facility:  FacilityAuthPriv,
		Severity:  SeverityInformational,
		Timestamp: fixedClock,
		Hostname:  "host01",
		AppName:   "bianque",
		ProcID:    "",
		MsgID:     "audit",
		Message:   `{"id":"ev-1"}`,
	}
	want := `<86>1 2027-01-15T08:00:00.000Z host01 bianque - audit - {"id":"ev-1"}`
	if got := FormatRFC5424(m); got != want {
		t.Fatalf("RFC5424 帧形态漂移:\n want %s\n got  %s", want, got)
	}

	// PROCID 在场形态
	m.ProcID = "7"
	want = `<86>1 2027-01-15T08:00:00.000Z host01 bianque 7 audit - {"id":"ev-1"}`
	if got := FormatRFC5424(m); got != want {
		t.Fatalf("PROCID 在场帧漂移:\n want %s\n got  %s", want, got)
	}
}

func TestFormatRFC5424_PRIComputation(t *testing.T) {
	cases := []struct {
		fac  Facility
		sev  Severity
		pri  string
		desc string
	}{
		{FacilityKernel, SeverityEmergency, "<0>", "kern.emerg"},
		{FacilityUser, SeverityDebug, "<15>", "user.debug"},
		{FacilityAuthPriv, SeverityInformational, "<86>", "authpriv.info（issue 原型）"},
		{FacilityLocal7, SeverityError, "<187>", "local7.err"},
		{Facility(23), Severity(7), "<191>", "边界值"},
	}
	for _, tc := range cases {
		got := FormatRFC5424(RFC5424Message{Facility: tc.fac, Severity: tc.sev})
		if !strings.HasPrefix(got, tc.pri+"1 ") {
			t.Fatalf("%s: 前缀应 %s, got %s", tc.desc, tc.pri+"1", got)
		}
	}
}

func TestFormatRFC5424_NilValues(t *testing.T) {
	// 全空字段：hostname/procid/msgid → "-"，零值时间戳 → "-"，空 MSG → "-"
	got := FormatRFC5424(RFC5424Message{Facility: FacilityAuthPriv, Severity: SeverityNotice})
	want := "<85>1 - - - - - - -"
	if got != want {
		t.Fatalf("全 NILVALUE 帧漂移:\n want %s\n got  %s", want, got)
	}
}

func TestFormatRFC5424_UTCTimezoneNormalization(t *testing.T) {
	// 非UTC 时区输入归一 UTC：08:00+00:00 在 +08:00 是 16:00
	loc := time.FixedZone("UTC+8", 8*3600)
	m := RFC5424Message{
		Facility:  FacilityAuthPriv,
		Severity:  SeverityInformational,
		Timestamp: time.Date(2027, 1, 15, 16, 0, 0, 0, loc),
	}
	got := FormatRFC5424(m)
	wantPrefix := "<86>1 2027-01-15T08:00:00.000Z "
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("应归一 UTC 挂 Z:\n want 前缀 %s\n got  %s", wantPrefix, got)
	}
}

func TestFormatRFC5424_NewlineSanitized(t *testing.T) {
	// MSG 内 \r 与 \n 逐字符替换为空格（\r\n ⇒ 两个空格；非透明分帧：
	// 行=一帧，换行会撕开分帧）
	got := FormatRFC5424(RFC5424Message{
		Facility:  FacilityAuthPriv,
		Severity:  SeverityInformational,
		Timestamp: fixedClock,
		Message:   "{\"id\":1}\n{\"id\":2}\r\n尾",
	})
	want := `<86>1 2027-01-15T08:00:00.000Z - - - - - {"id":1} {"id":2}  尾`
	if got != want {
		t.Fatalf("换行净化形态漂移:\n want %s\n got  %s", want, got)
	}
}

func TestFormatRFC5424_FailFastOnOutOfRange(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"facility越界", func() { FormatRFC5424(RFC5424Message{Facility: Facility(24)}) }},
		{"severity越界", func() { FormatRFC5424(RFC5424Message{Severity: Severity(8)}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("PRI 越界应 panic（协议值越界会被 SIEM 拒收，产出期就死）")
				}
			}()
			tc.fn()
		})
	}
}

// captureSyslog 起一个 TCP 监听并在连接关闭后返回收到的字节。
func captureSyslog(t *testing.T) (addr string, read func() []byte, close func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	done := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- nil
			return
		}
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 1024)
		for {
			n, err := conn.Read(tmp)
			buf = append(buf, tmp[:n]...)
			if err != nil {
				break
			}
		}
		_ = conn.Close()
		done <- buf
	}()
	return ln.Addr().String(),
		func() []byte { return <-done },
		func() { _ = ln.Close() }
}

func TestSyslogSink_TCPSendPinned(t *testing.T) {
	addr, read, closeLn := captureSyslog(t)
	defer closeLn()

	sink := NewSyslogSink("tcp", addr, "host01", "bianque",
		WithSyslogMsgID("audit"),
		WithSyslogClock(func() time.Time { return fixedClock }),
	)
	batch := []ForwardEvent{
		{Seq: 1, Payload: []byte(`{"id":"ev-1"}`)},
		{Seq: 2, Payload: []byte(`{"id":"ev-2"}`)},
	}
	if err := sink.Forward(context.Background(), batch); err != nil {
		t.Fatalf("投递不应出错, got %v", err)
	}
	closeLn() // 关监听触发读 goroutine 收尾

	raw := read()
	want := "<86>1 2027-01-15T08:00:00.000Z host01 bianque - audit - {\"id\":\"ev-1\"}\n" +
		"<86>1 2027-01-15T08:00:00.000Z host01 bianque - audit - {\"id\":\"ev-2\"}\n"
	if string(raw) != want {
		t.Fatalf("线上字节漂移:\n want %q\n got  %q", want, raw)
	}
	// 每条事件一帧：帧数 = 换行数 = 批大小
	if got := strings.Count(string(raw), "\n"); got != len(batch) {
		t.Fatalf("应 %d 帧, got %d", len(batch), got)
	}
}

func TestSyslogSink_UDPPerDatagram(t *testing.T) {
	// UDP：每帧一个数据报，报文=一帧（帧尾 \n 属非透明分帧惯例）
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("解析 UDP 地址失败: %v", err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		t.Fatalf("UDP 监听失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	sink := NewSyslogSink("udp", conn.LocalAddr().String(), "host01", "bianque",
		WithSyslogClock(func() time.Time { return fixedClock }),
	)
	if ferr := sink.Forward(context.Background(), []ForwardEvent{{Seq: 1, Payload: []byte(`{"id":"ev-1"}`)}}); ferr != nil {
		t.Fatalf("UDP 投递不应出错, got %v", ferr)
	}
	buf := make([]byte, 2048)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("读数据报失败: %v", err)
	}
	want := "<86>1 2027-01-15T08:00:00.000Z host01 bianque - - - {\"id\":\"ev-1\"}\n"
	if string(buf[:n]) != want {
		t.Fatalf("UDP 报文漂移:\n want %q\n got  %q", want, buf[:n])
	}
}

func TestSyslogSink_DialFailureFailsDelivery(t *testing.T) {
	// 起监听再立刻关：地址确定不可连（无固定端口竞争）
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	sink := NewSyslogSink("tcp", addr, "host01", "bianque",
		WithSyslogDialTimeout(time.Second),
	)
	err = sink.Forward(context.Background(), []ForwardEvent{{Seq: 1, Payload: []byte(`{"id":"ev-1"}`)}})
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("断链应包 ErrDeliveryFailed, got %v", err)
	}
}

func TestSyslogSink_EmptyPayloadRejected(t *testing.T) {
	addr, read, closeLn := captureSyslog(t)
	defer closeLn()

	sink := NewSyslogSink("tcp", addr, "host01", "bianque")
	err := sink.Forward(context.Background(), []ForwardEvent{{Seq: 1, Payload: nil}})
	if !errors.Is(err, ErrInvalidForwardPayload) {
		t.Fatalf("空载荷应包 ErrInvalidForwardPayload, got %v", err)
	}
	closeLn()
	if raw := read(); len(raw) != 0 {
		t.Fatalf("拒绝前不得写帧, got %q", raw)
	}
}

func TestSyslogSink_ForwarderIntegration(t *testing.T) {
	// 端到端：Forwarder + syslog sink——投递成功推进游标、断链游标不动
	addr, read, closeLn := captureSyslog(t)

	store := NewMemoryCursorStore()
	src := &cursorSource{events: batchOf(1)}
	sink := NewSyslogSink("tcp", addr, "host01", "bianque",
		WithSyslogClock(func() time.Time { return fixedClock }),
	)
	f := NewForwarder(store, src, []ForwardSink{sink})

	if _, err := f.ForwardOnce(context.Background()); err != nil {
		t.Fatalf("外发不应出错, got %v", err)
	}
	cur, _ := store.LoadCursor(context.Background())
	if cur != 1 {
		t.Fatalf("成功后游标推进, got %d", cur)
	}

	// 第二批事件存在，但断链（先关监听再投递）——游标不动，下轮重发
	closeLn()
	src.events = batchOf(1, 2)
	if _, err := f.ForwardOnce(context.Background()); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("断链应包 ErrDeliveryFailed, got %v", err)
	}
	cur, _ = store.LoadCursor(context.Background())
	if cur != 1 {
		t.Fatalf("断链游标不动, got %d", cur)
	}
	if !strings.Contains(string(read()), `"id":"ev-1"`) {
		t.Fatal("首轮帧应含 ev-1 载荷")
	}
}

func TestSyslogSink_FailFast(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"空地址", func() { NewSyslogSink("tcp", "", "h", "a") }},
		{"niloption", func() { NewSyslogSink("tcp", "127.0.0.1:514", "h", "a", nil) }},
		{"nil时钟", func() {
			NewSyslogSink("tcp", "127.0.0.1:514", "h", "a", WithSyslogClock(nil))
		}},
		{"非法dial超时", func() {
			NewSyslogSink("tcp", "127.0.0.1:514", "h", "a", WithSyslogDialTimeout(0))
		}},
		{"非法write死线", func() {
			NewSyslogSink("tcp", "127.0.0.1:514", "h", "a", WithSyslogWriteTimeout(-1))
		}},
		{"facility越界", func() {
			NewSyslogSink("tcp", "127.0.0.1:514", "h", "a", WithSyslogFacility(Facility(24)))
		}},
		{"severity越界", func() {
			NewSyslogSink("tcp", "127.0.0.1:514", "h", "a", WithSyslogSeverity(Severity(8)))
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

func TestSyslogSink_EmptyNetworkFallsBackTCP(t *testing.T) {
	// network 空串回退 "tcp"（仓规总则②）——用真 socket 验证落到了 TCP 面
	addr, read, closeLn := captureSyslog(t)
	defer closeLn()

	sink := NewSyslogSink("", addr, "host01", "bianque",
		WithSyslogClock(func() time.Time { return fixedClock }),
	)
	if err := sink.Forward(context.Background(), []ForwardEvent{{Seq: 1, Payload: []byte(`{}`)}}); err != nil {
		t.Fatalf("回退 tcp 不应出错, got %v", err)
	}
	closeLn()
	raw := read()
	if !strings.HasPrefix(string(raw), "<86>1 ") {
		t.Fatalf("TCP 帧应送达, got %q", raw)
	}
}
