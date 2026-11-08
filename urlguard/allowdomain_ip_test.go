package urlguard

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

// #296 回归钉：allowDomains 的文档承诺「放宽某些域的私网/约定域阻断」，
// 但 CheckIP 只认 allowPrivate/allowCIDRs——自建内网域名（解析到 10/8 的
// 自建 GitLab/webhook 是该选项的主要用例）写面（CheckURL）放行、拨号面
// （DialFunc）拒绝，同一 Guard 两个面结论相反；调用方被迫改用更宽的
// WithAllowCIDRs("10.0.0.0/8")，把整个网段对所有 host 放开，破坏最小权限。
//
// 修法：checkIPForHost(host, ip) 在 host 命中 allowDomains 时豁免内置网段
// 判定；CheckURL/CheckHost 的 IP 字面量分支与 DialFunc 的预检、端点复核
// 都走它。公开 CheckIP 无 host 上下文，不豁免。

// privateDNS 是「组织内网 DNS 把可信域解析到私网段」的常态形态。
var privateDNS = net.ParseIP("10.20.30.40")

// withPrivateLookup 把 DNS 固定成「解析出 10.20.30.40」。
func withPrivateLookup(t *testing.T, g *Guard) *Guard {
	t.Helper()
	g.lookup = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{privateDNS}, nil
	}
	return g
}

// issue 触发路径的原样复现：CheckURL 放行、DialFunc 拒绝（修复前）；
// 修复后两个面一致放行。第三条是 issue 里的正对照：同样的域名与解析结果，
// WithAllowCIDRs 也能通——证明卡住的确实是 IP 面。
func TestDialFunc_AllowDomainsExemptsPrivateResolve(t *testing.T) {
	t.Run("修复前形态：allowDomains 写面放行、拨号面拒绝（现在两半一致）", func(t *testing.T) {
		g := withPrivateLookup(t, mustGuard(t, WithAllowDomains("git.internal.corp")))

		// 写面：约定域后缀（.internal）被 allowDomains 短路——既有语义
		if err := g.CheckURL("https://git.internal.corp/hook"); err != nil {
			t.Fatalf("CheckURL 应放行, got %v", err)
		}
		// 拨号面：解析结果落在 10/8，host 命中 allowDomains ⇒ 豁免内置网段判定
		dialed := false
		dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return &fakeConn{remote: tcpAddr("10.20.30.40", 443), closed: new(bool)}, nil
		})
		if _, err := dial(context.Background(), "tcp", "git.internal.corp:443"); err != nil {
			t.Fatalf("DialFunc 应放行（#296：allowDomains 必须豁免 IP 面）, got %v", err)
		}
		if !dialed {
			t.Fatal("应到达底层拨号")
		}
	})

	t.Run("正对照：WithAllowCIDRs 同样能通（issue 第三行）", func(t *testing.T) {
		g := withPrivateLookup(t, mustGuard(t, WithAllowCIDRs("10.0.0.0/8")))
		dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
			return &fakeConn{remote: tcpAddr("10.20.30.40", 443), closed: new(bool)}, nil
		})
		if _, err := dial(context.Background(), "tcp", "git.internal.corp:443"); err != nil {
			t.Fatalf("allowCIDRs 正对照应通, got %v", err)
		}
	})

	t.Run("负对照：无豁免时拨号面拒绝", func(t *testing.T) {
		g := withPrivateLookup(t, mustGuard(t))
		dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
			t.Error("被阻断的解析结果不得进入底层拨号")
			return nil, errors.New("should not dial")
		})
		_, err := dial(context.Background(), "tcp", "git.internal.corp:443")
		if err == nil || !strings.Contains(err.Error(), "blocked range 10.0.0.0/8") {
			t.Fatalf("无豁免应被内置网段判定阻断, got %v", err)
		}
	})

	t.Run("点分后缀命中：子域同样豁免", func(t *testing.T) {
		g := withPrivateLookup(t, mustGuard(t, WithAllowDomains("internal.corp")))
		dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
			return &fakeConn{remote: tcpAddr("10.20.30.40", 443), closed: new(bool)}, nil
		})
		if _, err := dial(context.Background(), "tcp", "git.internal.corp:443"); err != nil {
			t.Fatalf("allowDomains 根域应覆盖子域, got %v", err)
		}
	})

	t.Run("未命中域不豁免：私网解析照拒", func(t *testing.T) {
		g := withPrivateLookup(t, mustGuard(t, WithAllowDomains("other.corp")))
		dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
			t.Error("未命中域不得豁免")
			return nil, errors.New("should not dial")
		})
		_, err := dial(context.Background(), "tcp", "git.internal.corp:443")
		if err == nil || !strings.Contains(err.Error(), "blocked range") {
			t.Fatalf("未命中 allowDomains 的域应照常阻断, got %v", err)
		}
	})
}

// 端点复核与预检同口径：host 豁免对建连后的真实 RemoteAddr 同样生效。
func TestDialFunc_AllowDomainsExemptsEndpointRecheck(t *testing.T) {
	g := withPrivateLookup(t, mustGuard(t, WithAllowDomains("git.internal.corp")))
	closed := new(bool)
	dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
		return &fakeConn{remote: tcpAddr("10.20.30.40", 443), closed: closed}, nil
	})
	conn, err := dial(context.Background(), "tcp", "git.internal.corp:443")
	if err != nil {
		t.Fatalf("端点为私网 IP 的连接不应被复核关闭（host 已豁免）, got %v", err)
	}
	if *closed {
		t.Fatal("连接不得被关闭")
	}
	_ = conn
}

// 公开 CheckIP 无 host 上下文：不享受 allowDomains 豁免（豁免是 host 维度的）。
func TestCheckIP_AllowDomainsDoesNotExempt(t *testing.T) {
	g := mustGuard(t, WithAllowDomains("git.internal.corp"))
	if err := g.CheckIP(privateDNS); err == nil || !strings.Contains(err.Error(), "blocked range") {
		t.Fatalf("CheckIP 无 host 上下文，私网 IP 应照常阻断, got %v", err)
	}
	// CheckHost 的 IP 字面量路径带 host 上下文：host 命中 allowDomains 时豁免
	if err := g.CheckHost("10.20.30.40"); err == nil {
		t.Fatal("未配置豁免的 IP 字面量应被拒")
	}
	g2 := mustGuard(t, WithAllowDomains("10.20.30.40"))
	if err := g2.CheckHost("10.20.30.40"); err != nil {
		t.Fatalf("host 命中 allowDomains 的字面量应豁免内置网段判定, got %v", err)
	}
	if err := g2.CheckURL("https://10.20.30.40/"); err != nil {
		t.Fatalf("CheckURL 字面量分支同口径豁免, got %v", err)
	}
}

// 豁免只作用于内置判定：自定义 checkIPFunc 照常执行（pkg#91 语义不回退）。
func TestDialFunc_AllowDomainsExemptionDoesNotSkipCustomPolicy(t *testing.T) {
	denied := errors.New("not in policy allowlist")
	g := withPrivateLookup(t, mustGuard(t,
		WithAllowDomains("git.internal.corp"),
		WithCheckIPFunc(func(net.IP) error { return denied }),
	))
	dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
		t.Error("消费方策略拒绝时不得进入底层拨号")
		return nil, errors.New("should not dial")
	})
	_, err := dial(context.Background(), "tcp", "git.internal.corp:443")
	// 拨号层 IP 预检的错误包装用 %v（既有口径，errors.Is 不穿链），
	// 按错误文本断言策略拒绝仍在。
	if err == nil || !strings.Contains(err.Error(), "rejected by policy") {
		t.Fatalf("豁免不得跳过消费方策略, got %v", err)
	}
}
