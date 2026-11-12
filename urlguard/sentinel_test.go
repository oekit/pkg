package urlguard

import (
	"context"
	"errors"
	"net"
	"testing"
)

// #380 回归：内置拒绝必须有可编程判定的哨兵（errors.Is），防护拒绝与
// 网络故障要分得开；拨号面两处 %v 丢链的点位改为 %w 后，哨兵要能穿透
// "dial %q blocked:" 包装冒出来。

func TestSentinels_CheckURL(t *testing.T) {
	g := mustGuard(t)
	cases := []struct {
		raw      string
		sentinel error
	}{
		{"http://10.1.2.3/hook", ErrBlockedRange},          // RFC1918
		{"http://127.0.0.1/hook", ErrBlockedRange},         // loopback
		{"http://169.254.169.254/latest", ErrBlockedRange}, // 云元数据
		{"http://svc.internal/hook", ErrBlockedDomain},     // 约定域后缀
		{"http://box.local/hook", ErrBlockedDomain},
		{"http://127.1/hook", ErrExoticLiteral},      // inet_aton 两段式
		{"http://0x7f000001/hook", ErrExoticLiteral}, // 十六进制
		{"http://2130706433/hook", ErrExoticLiteral}, // 十进制 32 位
	}
	for _, c := range cases {
		err := g.CheckURL(c.raw)
		if !errors.Is(err, c.sentinel) {
			t.Errorf("CheckURL(%q) 应命中哨兵 %v, got %v", c.raw, c.sentinel, err)
		}
	}
}

func TestSentinels_CheckIP(t *testing.T) {
	g := mustGuard(t)
	if err := g.CheckIP(net.ParseIP("192.168.1.9")); !errors.Is(err, ErrBlockedRange) {
		t.Fatalf("CheckIP(192.168.1.9) 应命中 ErrBlockedRange, got %v", err)
	}
	if err := g.CheckIP(net.ParseIP("::ffff:10.0.0.1")); !errors.Is(err, ErrBlockedRange) {
		t.Fatalf("mapped IPv6 归一后同样应命中 ErrBlockedRange, got %v", err)
	}
	if err := g.CheckIP(net.ParseIP("93.184.216.34")); err != nil {
		t.Fatalf("公网 IP 应放行, got %v", err)
	}
}

func TestSentinels_DialFace_Propagates(t *testing.T) {
	// 预检拒绝（:743 点位，改 %w 后哨兵必须穿透 "dial blocked" 包装）。
	g := mustGuard(t)
	g.lookup = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.7")}, nil
	}
	dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("预检拒绝后不得建连")
		return nil, nil
	})
	_, err := dial(context.Background(), "tcp", "rebind.example.com:443")
	if !errors.Is(err, ErrBlockedRange) {
		t.Fatalf("拨号面预检拒绝应透传 ErrBlockedRange, got %v", err)
	}

	// 端点复核拒绝（:760 点位）：预检放行公网、真实端点落私网（TOCTOU）。
	g2 := mustGuard(t)
	g2.lookup = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	dial2 := g2.DialFunc(func(context.Context, string, string) (net.Conn, error) {
		return &fakeConn{remote: tcpAddr("10.9.9.9", 443), closed: new(bool)}, nil
	})
	_, err = dial2(context.Background(), "tcp", "example.com:443")
	if !errors.Is(err, ErrBlockedRange) {
		t.Fatalf("端点复核拒绝应透传 ErrBlockedRange, got %v", err)
	}
}

func TestSentinels_NotOnNetworkFailure(t *testing.T) {
	// 网络故障（解析失败）不得误判为防护拦截——哨兵只标记「防护拒绝」。
	g := mustGuard(t)
	g.lookup = func(context.Context, string) ([]net.IP, error) {
		return nil, errors.New("dns timeout")
	}
	dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("解析失败不得建连")
		return nil, nil
	})
	_, err := dial(context.Background(), "tcp", "example.com:443")
	if err == nil {
		t.Fatal("解析失败应报错")
	}
	for _, s := range []error{ErrBlockedRange, ErrBlockedDomain, ErrExoticLiteral} {
		if errors.Is(err, s) {
			t.Fatalf("网络故障 %v 不得命中防护哨兵 %v", err, s)
		}
	}
}

func TestSentinels_ExemptionSuppressed(t *testing.T) {
	// 构造态豁免命中时哨兵不出现（豁免 = 放行，不是「命中但豁免」）。
	g := mustGuard(t, WithAllowPrivate())
	if err := g.CheckIP(net.ParseIP("10.1.2.3")); err != nil {
		t.Fatalf("allowPrivate 下私网应放行, got %v", err)
	}
	g2 := mustGuard(t, WithAllowDomains("svc.internal"))
	if err := g2.CheckURL("http://api.svc.internal/hook"); err != nil {
		t.Fatalf("allowDomains 下约定域应放行, got %v", err)
	}
}

// TestSentinels_WriteFaceConfig 钉住 #390：写面（CheckURL/CheckHost/CheckIP
// 层 1）剩余的三个可分流类别必须有哨兵——scheme 白名单（配置类）、非 ASCII
// fail-closed（输入形态类）、精确白名单未命中（配置类）。调用方分流指标与
// 告警面不再解析文案。
func TestSentinels_WriteFaceConfig(t *testing.T) {
	// scheme 白名单：配置类哨兵。
	g := mustGuard(t)
	if err := g.CheckURL("ftp://example.com/file"); !errors.Is(err, ErrSchemeNotAllowed) {
		t.Fatalf("scheme 拒绝 err = %v, want ErrSchemeNotAllowed", err)
	}
	// 非 ASCII fail-closed：输入形态类哨兵。
	if err := g.CheckURL("http://例え.jp/"); !errors.Is(err, ErrNonASCIIHost) {
		t.Fatalf("非 ASCII 拒绝 err = %v, want ErrNonASCIIHost", err)
	}
	if err := g.CheckHost("例え.jp"); !errors.Is(err, ErrNonASCIIHost) {
		t.Fatalf("CheckHost 非 ASCII err = %v, want ErrNonASCIIHost", err)
	}
	// 精确白名单未命中：配置类哨兵（命中面不出现）。
	ge := mustGuard(t, WithExactHosts("api.example.com:8443"))
	if err := ge.CheckURL("https://api.example.com:8443/hook"); err != nil {
		t.Fatalf("白名单命中不应拒绝: %v", err)
	}
	if err := ge.CheckURL("https://other.example.com/hook"); !errors.Is(err, ErrNotInExactHosts) {
		t.Fatalf("白名单未命中 err = %v, want ErrNotInExactHosts", err)
	}
	// 分流底线：配置类哨兵不得与防护拦截面互相命中。
	for _, s := range []error{ErrBlockedRange, ErrBlockedDomain, ErrExoticLiteral} {
		if err := g.CheckURL("ftp://example.com/file"); errors.Is(err, s) {
			t.Fatalf("配置类拒绝不得命中防护哨兵 %v", s)
		}
	}
	if err := g.CheckURL("http://例え.jp/"); errors.Is(err, ErrSchemeNotAllowed) || errors.Is(err, ErrNotInExactHosts) {
		t.Fatal("非 ASCII 拒绝不得命中配置类哨兵")
	}
}
