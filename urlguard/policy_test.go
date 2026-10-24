package urlguard

// urlguard 自定义判定钩子测试（pkg#91）：
//
// 缺口：urlguard 是纯放行型防护——缺省阻断私网/loopback/保留段与约定域名
// 后缀，此外一律放行。它表达不了消费方常见的限制型策略（IP 白名单只放行
// 我的网段 / 黑名单屏蔽这些网段 / 域名白黑名单），于是消费方只能在 urlguard
// 之外手写一层策略叠加——「SSRF 防护的单源判定」这个定位被复制了一份。
//
// 修法：WithCheckIPFunc / WithCheckHostFunc 两个钩子，让消费方的策略同时
// 作用于「预检」与「建连后端点复核」两处。

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

// TestCheckIPFunc_RejectsCustomPolicy 钉住 IP 钩子的基本语义：命中消费方
// 策略即拒绝，未命中放行。
//
// 用的都是**公网**地址（不在内置阻断清单里）——这样被拒的原因只可能是消费方
// 自己的策略，而不是内置兜底。
func TestCheckIPFunc_RejectsCustomPolicy(t *testing.T) {
	denied := errors.New("not in allowlist")
	g := mustGuard(t, WithCheckIPFunc(func(ip net.IP) error {
		if ip.Equal(net.ParseIP("93.184.216.34")) {
			return denied
		}
		return nil
	}))

	if err := g.CheckIP(net.ParseIP("93.184.216.34")); err == nil {
		t.Fatal("命中消费方黑名单应拒绝")
	} else if !errors.Is(err, denied) {
		t.Fatalf("应保留消费方原始错误（errors.Is 可判定），got %v", err)
	}
	if err := g.CheckIP(net.ParseIP("8.8.8.8")); err != nil {
		t.Fatalf("未命中消费方策略应放行，got %v", err)
	}
}

// TestCheckIPFunc_NormalizedInput 钉住入参口径：IPv4-mapped IPv6 已归一化为
// IPv4——消费方无需自己处理 mapped 绕过。
func TestCheckIPFunc_NormalizedInput(t *testing.T) {
	var got net.IP
	g := mustGuard(t, WithCheckIPFunc(func(ip net.IP) error {
		got = ip
		return nil
	}))
	if err := g.CheckIP(net.ParseIP("::ffff:93.184.216.34")); err != nil {
		t.Fatalf("mapped 地址应走归一化判定，got %v", err)
	}
	if got == nil || got.To4() == nil {
		t.Fatalf("钩子入参应为归一化后的 IPv4，got %v", got)
	}
	if got.String() != "93.184.216.34" {
		t.Fatalf("钩子入参应为 93.184.216.34，got %v", got)
	}
}

// TestCheckIPFunc_ExemptionDoesNotSkipCustom 钉住豁免语义收敛（pkg#91）：
// WithAllowPrivate / WithAllowCIDRs 只豁免**内置**判定，不再跳过自定义判定。
//
// 这是本 issue 的核心诉求：豁免不再意味着「整个 IP 判定被短路」。
func TestCheckIPFunc_ExemptionDoesNotSkipCustom(t *testing.T) {
	denied := errors.New("denied by consumer policy")

	t.Run("AllowPrivate", func(t *testing.T) {
		g := mustGuard(t, WithAllowPrivate(), WithCheckIPFunc(func(net.IP) error {
			return denied
		}))
		if err := g.CheckIP(net.ParseIP("10.0.0.1")); err == nil {
			t.Fatal("AllowPrivate 不得跳过自定义判定")
		}
	})

	t.Run("AllowCIDRs", func(t *testing.T) {
		g := mustGuard(t, WithAllowCIDRs("10.0.0.0/8"), WithCheckIPFunc(func(net.IP) error {
			return denied
		}))
		if err := g.CheckIP(net.ParseIP("10.0.0.1")); err == nil {
			t.Fatal("AllowCIDRs 不得跳过自定义判定")
		}
	})
}

// TestCheckIPFunc_BuiltInStillBlocks 钉住顺序：内置判定在前——自定义判定
// 放行也不豁免内置的私网/保留段阻断（fail-closed 兜底不被削弱）。
func TestCheckIPFunc_BuiltInStillBlocks(t *testing.T) {
	g := mustGuard(t, WithCheckIPFunc(func(net.IP) error { return nil }))
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "::1"} {
		if err := g.CheckIP(net.ParseIP(ip)); err == nil {
			t.Errorf("%s 属内置阻断段，自定义判定放行也不应放行", ip)
		}
	}
}

// TestCheckIPFunc_UnsetKeepsLegacyBehavior 钉住未注入钩子时的行为不变：
// 构造态豁免仍然整体短路（与 pkg#91 前的历史行为一致）。
func TestCheckIPFunc_UnsetKeepsLegacyBehavior(t *testing.T) {
	g := mustGuard(t, WithAllowPrivate())
	if err := g.CheckIP(net.ParseIP("127.0.0.1")); err != nil {
		t.Fatalf("AllowPrivate 下 loopback 应放行，got %v", err)
	}
	g2 := mustGuard(t, WithAllowCIDRs("10.0.0.0/8"))
	if err := g2.CheckIP(net.ParseIP("10.1.2.3")); err != nil {
		t.Fatalf("AllowCIDRs 覆盖网段应放行，got %v", err)
	}
}

// TestDialFunc_CustomPolicyAppliesToPreCheck 钉住「预检」处生效：解析出的
// IP 命中消费方策略即不建连（next 不被调用）。
func TestDialFunc_CustomPolicyAppliesToPreCheck(t *testing.T) {
	g := mustGuard(t, WithCheckIPFunc(func(ip net.IP) error {
		if ip.Equal(net.ParseIP("93.184.216.34")) {
			return errors.New("blocked by consumer policy")
		}
		return nil
	}))
	called := false
	dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
		called = true
		return nil, errors.New("should not dial")
	})
	g.lookup = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	_, err := dial(context.Background(), "tcp", "example.com:443")
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("预检命中消费方策略应拒绝，got %v", err)
	}
	if called {
		t.Fatal("预检拒绝后不得调用底层拨号")
	}
}

// TestDialFunc_CustomPolicyAppliesToEndpointRecheck 钉住「建连后端点复核」
// 处生效（pkg#91 的核心）：预检解析的 IP 合规、实际建连端点命中消费方策略
// （重解析窗口）⇒ 连接被立即关闭并报错。
//
// 这是「消费方的策略成为单一判定源」的落点：TOCTOU 窗口由消费方自己的
// 策略闭合，而不是靠 urlguard 内置的私网清单。
//
// 两端都用**公网**地址（内置判定都放行）——被拒只可能是消费方策略生效。
func TestDialFunc_CustomPolicyAppliesToEndpointRecheck(t *testing.T) {
	g := mustGuard(t, WithCheckIPFunc(func(ip net.IP) error {
		if ip.Equal(net.ParseIP("8.8.8.8")) {
			return errors.New("denied by consumer policy")
		}
		return nil
	}))
	g.lookup = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	closed := false
	dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
		return &fakeConn{remote: tcpAddr("8.8.8.8", 443), closed: &closed}, nil
	})
	_, err := dial(context.Background(), "tcp", "rebind.example.com:443")
	if err == nil || !strings.Contains(err.Error(), "connected endpoint") {
		t.Fatalf("实际端点命中消费方策略应报错，got %v", err)
	}
	if !closed {
		t.Fatal("违规连接必须被关闭")
	}
}

// TestCheckHostFunc_AppliesToURLAndHost 钉住 host 钩子作用于 CheckURL 与
// CheckHost 两个入口。
func TestCheckHostFunc_AppliesToURLAndHost(t *testing.T) {
	denied := errors.New("domain not allowed")
	g := mustGuard(t, WithCheckHostFunc(func(host string) error {
		if host == "evil.example.com" {
			return denied
		}
		return nil
	}))

	if err := g.CheckURL("https://evil.example.com/hook"); err == nil {
		t.Fatal("CheckURL 应命中 host 钩子")
	} else if !errors.Is(err, denied) {
		t.Fatalf("应保留消费方原始错误，got %v", err)
	}
	if err := g.CheckHost("evil.example.com"); err == nil {
		t.Fatal("CheckHost 应命中 host 钩子")
	}
	if err := g.CheckURL("https://good.example.com/hook"); err != nil {
		t.Fatalf("未命中 host 钩子应放行，got %v", err)
	}
}

// TestCheckHostFunc_NormalizedInput 钉住入参口径：归一化后（小写、去尾点）。
func TestCheckHostFunc_NormalizedInput(t *testing.T) {
	var got string
	g := mustGuard(t, WithCheckHostFunc(func(host string) error {
		got = host
		return nil
	}))
	if err := g.CheckHost("EXAMPLE.COM."); err != nil {
		t.Fatalf("CheckHost: %v", err)
	}
	if got != "example.com" {
		t.Fatalf("钩子入参应为归一化后的 host，got %q", got)
	}
}

// TestDialFunc_CustomHostPolicyApplies 钉住 host 钩子在拨号层也生效：
// 消费方可能只挂 DialFunc（不挂 CheckURL），此时 host 判定不能缺席。
func TestDialFunc_CustomHostPolicyApplies(t *testing.T) {
	g := mustGuard(t, WithCheckHostFunc(func(host string) error {
		if host == "evil.example.com" {
			return errors.New("host denied by consumer policy")
		}
		return nil
	}))
	called := false
	dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
		called = true
		return nil, errors.New("should not dial")
	})
	g.lookup = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	_, err := dial(context.Background(), "tcp", "evil.example.com:443")
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("拨号层应命中 host 钩子，got %v", err)
	}
	if called {
		t.Fatal("host 钩子拒绝后不得调用底层拨号")
	}
}

// TestCustomPolicy_CombinedIPAndHost 钉住两个维度可叠加：host 钩子与 IP 钩子
// 各管各的维度，同时注入时互不干扰。
func TestCustomPolicy_CombinedIPAndHost(t *testing.T) {
	g := mustGuard(t,
		WithCheckHostFunc(func(host string) error {
			if !strings.HasSuffix(host, ".allowed.example.com") {
				return errors.New("host not allowed")
			}
			return nil
		}),
		WithCheckIPFunc(func(ip net.IP) error {
			if !ip.Equal(net.ParseIP("93.184.216.34")) {
				return errors.New("ip not allowed")
			}
			return nil
		}),
	)
	// host 不合规 → host 钩子拒绝
	if err := g.CheckURL("https://evil.example.com/x"); err == nil {
		t.Fatal("host 钩子应拒绝")
	}
	// host 合规但 IP 不合规（IP 字面量）→ IP 钩子拒绝
	if err := g.CheckURL("http://8.8.8.8/x"); err == nil {
		t.Fatal("IP 钩子应拒绝")
	}
	// 两者均合规 → 放行
	if err := g.CheckURL("https://api.allowed.example.com/x"); err != nil {
		t.Fatalf("两者均合规应放行，got %v", err)
	}
}
