package urlguard

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
)

func mustGuard(t *testing.T, opts ...Option) *Guard {
	t.Helper()
	g, err := New(opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return g
}

func TestCheckURL_BlockedLiterals(t *testing.T) {
	g := mustGuard(t)
	blocked := []string{
		"http://127.0.0.1/hook",                    // loopback
		"http://10.1.2.3/hook",                     // RFC1918
		"http://172.16.5.9/hook",                   // RFC1918
		"http://192.168.1.1/hook",                  // RFC1918
		"http://169.254.169.254/latest/meta-data/", // 云元数据端点（经典 SSRF 目标）
		"http://100.64.1.2/hook",                   // CGNAT
		"http://0.0.0.0/hook",                      // 未指定
		"http://[::1]/hook",                        // IPv6 loopback
		"http://[::ffff:10.0.0.1]/hook",            // IPv4-mapped IPv6 绕过（归一化后命中 10/8）
		"http://[fc00::1]/hook",                    // ULA
		"http://foo.internal/hook",                 // 约定域名
		"http://bar.local/hook",                    // mDNS
		"http://localhost/hook",
		"http://api.localhost/hook",
	}
	for _, raw := range blocked {
		if err := g.CheckURL(raw); err == nil {
			t.Errorf("%s 应被阻断，实际放行", raw)
		}
	}
}

func TestCheckURL_AllowsPublic(t *testing.T) {
	g := mustGuard(t)
	allowed := []string{
		"https://example.com/hook",
		"http://8.8.8.8/hook",
		"http://example.com:8443/hook", // 带端口
		"http://[2001:db8::1]/hook",    // 文档保留段不在阻断清单
	}
	for _, raw := range allowed {
		if err := g.CheckURL(raw); err != nil {
			t.Errorf("%s 应放行，实际阻断：%v", raw, err)
		}
	}
}

func TestCheckURL_Schemes(t *testing.T) {
	g := mustGuard(t)
	for _, raw := range []string{"file:///etc/passwd", "ftp://example.com/", "gopher://example.com/"} {
		if err := g.CheckURL(raw); err == nil || !strings.Contains(err.Error(), "scheme") {
			t.Errorf("%s 应因 scheme 被拒，got %v", raw, err)
		}
	}
	g2 := mustGuard(t, WithAllowedSchemes("ssh"))
	if err := g2.CheckURL("ssh://git@example.com/repo"); err != nil {
		t.Errorf("自定义 scheme 白名单应放行，got %v", err)
	}
}

func TestCheckIP_MappedNormalized(t *testing.T) {
	g := mustGuard(t)
	if err := g.CheckIP(net.ParseIP("::ffff:10.0.0.1")); err == nil {
		t.Fatal("::ffff:10.0.0.1 归一化后应命中 10/8")
	}
	if err := g.CheckIP(net.ParseIP("::ffff:8.8.8.8")); err != nil {
		t.Fatalf("::ffff:8.8.8.8 应放行，got %v", err)
	}
}

func TestAllowOptions(t *testing.T) {
	gp := mustGuard(t, WithAllowPrivate())
	if err := gp.CheckURL("http://127.0.0.1:8080/mock"); err != nil {
		t.Fatalf("WithAllowPrivate 应放行本机 mock，got %v", err)
	}

	gc, err := New(WithAllowCIDRs("10.20.0.0/16"))
	if err != nil {
		t.Fatal(err)
	}
	if err := gc.CheckIP(net.ParseIP("10.20.1.2")); err != nil {
		t.Fatalf("豁免网段应放行，got %v", err)
	}
	if err := gc.CheckIP(net.ParseIP("10.30.1.2")); err == nil {
		t.Fatal("豁免网段之外仍应阻断")
	}
	if _, err := New(WithAllowCIDRs("not-a-cidr")); err == nil {
		t.Fatal("非法 CIDR 应在 New 报错")
	}

	gd := mustGuard(t, WithAllowDomains("Example.COM"))
	if err := gd.CheckURL("https://api.example.com/hook"); err != nil {
		t.Fatalf("点分后缀应放行（大小写不敏感），got %v", err)
	}
	if err := gd.CheckURL("https://notexample.com/hook"); err != nil {
		t.Fatalf("公网域 notexample.com 应照常放行，got %v", err)
	}
	// 后缀豁免必须点分边界：allow("internal") 能豁免 foo.internal；
	// allow("ternal") 不豁免 foo.internal（internal 不是以 ".ternal" 点分结尾）
	gi := mustGuard(t, WithAllowDomains("internal"))
	if err := gi.CheckURL("https://foo.internal/hook"); err != nil {
		t.Fatalf("allow internal 应豁免 foo.internal，got %v", err)
	}
	gt := mustGuard(t, WithAllowDomains("ternal"))
	if err := gt.CheckURL("https://foo.internal/hook"); err == nil {
		t.Fatal("非点分边界的豁免（ternal）不得生效")
	}
}

func TestCheckHost_Forms(t *testing.T) {
	g := mustGuard(t)
	for _, h := range []string{"LOCALHOST", "localhost.", "Foo.INTERNAL.", "[::1]"} {
		if err := g.CheckHost(h); err == nil {
			t.Errorf("host %q 应被阻断（大小写/尾点/括号归一化）", h)
		}
	}
	if err := g.CheckHost(""); err == nil {
		t.Fatal("空 host 应报错")
	}
}

// fakeConn 仅实现 DialFunc 路径用到的 RemoteAddr/Close；其余方法来自内嵌 nil 接口、不应被调用。
type fakeConn struct {
	net.Conn
	remote net.Addr
	closed *bool
}

func (f *fakeConn) RemoteAddr() net.Addr { return f.remote }
func (f *fakeConn) Close() error         { *f.closed = true; return nil }

func tcpAddr(ip string, port int) net.Addr {
	return &net.TCPAddr{IP: net.ParseIP(ip), Port: port}
}

// TestDialFunc_PreCheckBlocks 钉住层 2：解析出的 IP 命中阻断即不建连（next 不被调用）。
func TestDialFunc_PreCheckBlocks(t *testing.T) {
	g := mustGuard(t)
	called := false
	dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
		called = true
		return nil, errors.New("should not dial")
	})
	g.lookup = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("93.184.216.34")}, nil
	}
	_, err := dial(context.Background(), "tcp", "rebind.example.com:443")
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("解析 IP 含阻断段应拒绝，got %v", err)
	}
	if called {
		t.Fatal("预检拒绝后不得调用底层拨号")
	}
}

// TestDialFunc_PostCheckCloses 钉住 TOCTOU 闭合：预检解析为豁免网段、实际建连端点
// 却是阻断地址（重解析窗口）——连接被立即关闭并报错。
func TestDialFunc_PostCheckCloses(t *testing.T) {
	g := mustGuard(t, WithAllowCIDRs("10.0.0.0/8"))
	g.lookup = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.5")}, nil
	}
	closed := false
	dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
		return &fakeConn{remote: tcpAddr("127.0.0.1", 9000), closed: &closed}, nil
	})
	_, err := dial(context.Background(), "tcp", "internal.svc:9000")
	if err == nil || !strings.Contains(err.Error(), "connected endpoint") {
		t.Fatalf("实际端点违规应报错，got %v", err)
	}
	if !closed {
		t.Fatal("违规连接必须被关闭")
	}
}

// TestDialFunc_HappyPath：解析与端点均合规 → next 收到原样 addr，连接返回。
func TestDialFunc_HappyPath(t *testing.T) {
	g := mustGuard(t)
	g.lookup = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	var gotAddr string
	dial := g.DialFunc(func(_ context.Context, _ string, addr string) (net.Conn, error) {
		gotAddr = addr
		return &fakeConn{remote: tcpAddr("93.184.216.34", 443), closed: new(bool)}, nil
	})
	conn, err := dial(context.Background(), "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("合规目标应放行，got %v", err)
	}
	if gotAddr != "example.com:443" {
		t.Fatalf("next 应收到原样 addr，got %q", gotAddr)
	}
	_ = conn.Close()
}

// TestDialFunc_AllowPrivateRealDNS：WithAllowPrivate + 真实 DNS（localhost）打本机 mock。
func TestDialFunc_AllowPrivateRealDNS(t *testing.T) {
	g := mustGuard(t, WithAllowPrivate())
	dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
		return &fakeConn{remote: tcpAddr("127.0.0.1", 8080), closed: new(bool)}, nil
	})
	if _, err := dial(context.Background(), "tcp", "localhost:8080"); err != nil {
		t.Fatalf("测试放行应打通本机 mock，got %v", err)
	}
}

// TestConcurrentUse：Guard 只读共享，-race 下并发判定无数据竞争。
func TestConcurrentUse(t *testing.T) {
	g := mustGuard(t)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = g.CheckURL("http://10.0.0.1/")
			_ = g.CheckURL("https://example.com/")
			_ = g.CheckIP(net.ParseIP("::ffff:192.168.0.9"))
		}()
	}
	wg.Wait()
}

func TestShortcuts(t *testing.T) {
	if err := IsBlockedURL("http://169.254.169.254/latest/meta-data/"); err == nil {
		t.Fatal("IsBlockedURL 应阻断云元数据端点")
	}
	if err := IsBlockedURL("https://example.com/"); err != nil {
		t.Fatalf("IsBlockedURL 应放行公网，got %v", err)
	}
	if SafeDialFunc(nil) == nil {
		t.Fatal("SafeDialFunc 应返回非 nil 包装")
	}
}
