package urlguard

import (
	"context"
	"net"
	"testing"
)

// TestCheckURL_IPv6ZoneBypass 回归测试：IPv6 字面量带 zone 后缀（%25lo /
// %25eth0）时，IP 字面量判定不得被整段跳过。
//
// 缺陷成因：net.ParseIP 不认 zone，而 url.Parse 后的 u.Hostname() 会保留它
// （Go 把 %25 还原成 %）⇒ ParseIP 返回 nil ⇒ checkHostLiteral 直接落到
// 域名分支 ⇒ loopback / 链路本地 / ULA / IPv4-mapped 全能以 zone 形态绕过。
func TestCheckURL_IPv6ZoneBypass(t *testing.T) {
	g := mustGuard(t)
	blocked := []string{
		"http://[::1%25lo]/hook",                // IPv6 loopback + zone
		"http://[fe80::1%25eth0]/hook",          // 链路本地 + zone（含云元数据同段）
		"http://[fc00::1%25eth0]/hook",          // ULA + zone
		"http://[::ffff:127.0.0.1%25eth0]/hook", // IPv4-mapped loopback + zone
		"http://[::ffff:10.0.0.1%25eth0]/hook",  // IPv4-mapped RFC1918 + zone
	}
	for _, raw := range blocked {
		if err := g.CheckURL(raw); err == nil {
			t.Errorf("CheckURL(%q) 应被阻断（zone 形态绕过 IP 字面量判定）", raw)
		}
	}
}

// TestCheckHost_IPv6ZoneBypass 回归测试：CheckHost 入口同样不得被 zone 绕过。
func TestCheckHost_IPv6ZoneBypass(t *testing.T) {
	g := mustGuard(t)
	for _, host := range []string{"::1%lo", "fe80::1%eth0", "fc00::1%eth0", "::ffff:127.0.0.1%eth0"} {
		if err := g.CheckHost(host); err == nil {
			t.Errorf("CheckHost(%q) 应被阻断", host)
		}
	}
}

// TestCheckURL_IPv6Zone_AllowedPrivate 豁免口径：开了 WithAllowPrivate 后
// 带 zone 的私网地址与不带 zone 的同地址判定一致（豁免的是网段，不是写法）。
func TestCheckURL_IPv6Zone_AllowedPrivate(t *testing.T) {
	g := mustGuard(t, WithAllowPrivate())
	for _, raw := range []string{"http://[fc00::1]/hook", "http://[fc00::1%25eth0]/hook"} {
		if err := g.CheckURL(raw); err != nil {
			t.Errorf("CheckURL(%q) 在 WithAllowPrivate 下应放行，实得 %v", raw, err)
		}
	}
}

// TestCheckURL_IPv6ReservedRanges 回归测试：IPv4-compatible（::/96）与已废弃的
// site-local（fec0::/10）此前不在阻断表内。
func TestCheckURL_IPv6ReservedRanges(t *testing.T) {
	g := mustGuard(t)
	for _, raw := range []string{
		"http://[::127.0.0.1]/hook", // IPv4-compatible：127.0.0.1 的 IPv6 写法
		"http://[fec0::1]/hook",     // site-local（RFC 3879 已废弃）
	} {
		if err := g.CheckURL(raw); err == nil {
			t.Errorf("CheckURL(%q) 应被阻断（IPv6 保留段）", raw)
		}
	}
}

// fakeAddrConn 是只关心 RemoteAddr 的假连接（用于端点复核测试）。
type fakeAddrConn struct {
	net.Conn
	ra net.Addr
}

func (c fakeAddrConn) RemoteAddr() net.Addr { return c.ra }

// Close 覆写内嵌 nil net.Conn 的 Close（拒绝路径会调它）。
func (c fakeAddrConn) Close() error { return nil }

// TestDialFunc_EndpointRecheck_UDP 回归测试：端点复核此前只认 *net.TCPAddr，
// UDP 连接的端点复核被整段跳过 ⇒ TOCTOU 窗口对 UDP 完全敞开。
func TestDialFunc_EndpointRecheck_UDP(t *testing.T) {
	g := mustGuard(t)
	next := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return fakeAddrConn{ra: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 53}}, nil
	}
	dial := g.DialFunc(next)
	conn, err := dial(context.Background(), "udp", "8.8.8.8:53")
	if err == nil {
		_ = conn.Close()
		t.Fatal("端点为 127.0.0.1 的 UDP 连接应被端点复核拦下")
	}
}

// TestDialFunc_EndpointRecheck_IPAddr 回归测试：*net.IPAddr 同样要复核。
func TestDialFunc_EndpointRecheck_IPAddr(t *testing.T) {
	g := mustGuard(t)
	next := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return fakeAddrConn{ra: &net.IPAddr{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	dial := g.DialFunc(next)
	conn, err := dial(context.Background(), "tcp", "8.8.8.8:53")
	if err == nil {
		_ = conn.Close()
		t.Fatal("端点为 127.0.0.1 的 IP 连接应被端点复核拦下")
	}
}

// TestDialFunc_EndpointRecheck_UnknownAddrType 自定义 conn（取不到 IP）时
// 端点复核跳过——刻意不 fail-closed（代理链路的 RemoteAddr 是代理地址，
// 一拒就把所有代理用法打死）。本测试把该边界钉住。
func TestDialFunc_EndpointRecheck_UnknownAddrType(t *testing.T) {
	g := mustGuard(t)
	next := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return fakeAddrConn{ra: unknownAddr("opaque")}, nil
	}
	dial := g.DialFunc(next)
	conn, err := dial(context.Background(), "tcp", "8.8.8.8:53")
	if err != nil {
		t.Fatalf("未知 addr 类型不应 fail-closed，实得 %v", err)
	}
	_ = conn.Close()
}

type unknownAddr string

func (a unknownAddr) Network() string { return "opaque" }
func (a unknownAddr) String() string  { return string(a) }

// TestStripIPv6Zone 剥 zone 的边界：无 zone 原样返回；zone 只截第一段。
func TestStripIPv6Zone(t *testing.T) {
	cases := map[string]string{
		"::1":             "::1",
		"::1%lo":          "::1",
		"fe80::1%eth0":    "fe80::1",
		"example.com":     "example.com",
		"%":               "",
		"host%25pct%more": "host",
	}
	for in, want := range cases {
		if got := stripIPv6Zone(in); got != want {
			t.Errorf("stripIPv6Zone(%q) = %q，期望 %q", in, got, want)
		}
	}
}
