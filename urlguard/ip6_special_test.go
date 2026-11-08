package urlguard

import (
	"net"
	"testing"
)

// mustParseIP 测试内建地址的解析守卫。
func mustParseIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("测试地址 %q 解析失败", s)
	}
	return ip
}

// #297 回归钉：blockedCIDRs 补齐 IANA 在 #75 收口之后新登记的三段 IPv6
// 特殊用途前缀——3fff::/20（RFC 9637 文档段）、5f00::/16（RFC 9602 SRv6
// SID）、100:0:0:1::/64（RFC 9780 dummy prefix）。同生态 oekit/core/network
// 的 IsPublicIP 对这批地址全部阻断，urlguard 曾全部放行——判定表与 IANA
// 注册表脱节。
//
// 负对照（公网地址必须仍放行）保证新增走的是内置表的**增量**，不是「给
// 所有 IPv6 加一段」的蒙混：把新增三段从 blockedCIDRs 删掉，本文件的
// blocked 用例即转红（表驱动用例逐段断言）。

func TestBlockedCIDRs_NewIPv6SpecialPrefixes(t *testing.T) {
	blocked := []string{
		"3fff::1",           // RFC 9637 文档段（IPv6 侧 TEST-NET）
		"5f00::1",           // RFC 9602 SRv6 SID
		"100:0:0:1::1",      // RFC 9780 dummy prefix
		"3fff:f::1",         // 3fff::/20 段内（/20 = 首组 3fff + 次组 0x0000-0x000f）
		"5f00:abcd::1",      // 5f00::/16 段内（/16 = 仅首组固定）
		"100:0:0:1:ffff::1", // /64 段内非零接口 ID
	}
	for _, ip := range blocked {
		if err := mustGuard(t).CheckIP(mustParseIP(t, ip)); err == nil {
			t.Errorf("CheckIP(%s) 应被内置表阻断（#297）, got nil", ip)
		}
		if err := mustGuard(t).CheckHost(ip); err == nil {
			t.Errorf("CheckHost(%s) 应被字面量判定阻断（#297）, got nil", ip)
		}
		if err := mustGuard(t).CheckURL("https://[" + ip + "]/"); err == nil {
			t.Errorf("CheckURL(https://[%s]/) 应被阻断（#297）, got nil", ip)
		}
	}
}

// 负对照：真实公网地址不受新增段影响——v6（Cloudflare）与 v4 各一条。
func TestBlockedCIDRs_NewIPv6SpecialPrefixes_PublicStillAllowed(t *testing.T) {
	public := []string{
		"2606:4700::1", // Cloudflare IPv6
		"1.1.1.1",      // 公网 IPv4
		"2606:4700:4700::1111",
	}
	for _, ip := range public {
		if err := mustGuard(t).CheckIP(mustParseIP(t, ip)); err != nil {
			t.Errorf("CheckIP(%s) 应放行（负对照）, got %v", ip, err)
		}
		if err := mustGuard(t).CheckHost(ip); err != nil {
			t.Errorf("CheckHost(%s) 应放行（负对照）, got %v", ip, err)
		}
	}
}

// WithCheckIPFunc 既有语义不回退：新增段走内置表路径，构造态豁免与自定义
// 钩子的既有用例（policy_test.go）保持绿——此处补一条「allowPrivate 仍豁免
// 新增段」（豁免只作用于内置表，段加多少都不改变豁免口径）。
func TestBlockedCIDRs_NewIPv6SpecialPrefixes_AllowPrivateStillExempts(t *testing.T) {
	g := mustGuard(t, WithAllowPrivate())
	for _, ip := range []string{"3fff::1", "5f00::1", "100:0:0:1::1"} {
		if err := g.CheckIP(mustParseIP(t, ip)); err != nil {
			t.Errorf("WithAllowPrivate 应豁免内置网段判定（%s）, got %v", ip, err)
		}
	}
}
