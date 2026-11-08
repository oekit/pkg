package urlguard

import (
	"strings"
	"testing"
)

// nonascii_test.go 钉住 pkg#266：全角数字与 Unicode 句点形式的回环地址不再放行。
//
// 判定面此前只覆盖 ASCII：`127。0。0。1`（U+3002）、`127．0。0．1`（U+FF0E）、
// `①②⑦.0.0.1`（圈号）、`１２７.0.0.1`（全角数字）经浏览器 / node 的 WHATWG
// URL 规范化都是 127.0.0.1（UTS46 映射），而 net.ParseIP 与 exotic 词法判定对
// 它们全部失明 ⇒ 落进「普通域名」分支交给 DNS——判定放行、客户端连回环。

// TestCheckURL_BlocksUnicodeLoopbackForms issue 复现样本逐一必须阻断：
// 可确定性折叠的形态折成 127.0.0.1 走网段判定；无法折叠的（圈号）fail-closed。
func TestCheckURL_BlocksUnicodeLoopbackForms(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1/",  // ASCII 基线
		"http://127。0。0。1/",  // U+3002 表意句号
		"http://127．0．0．1/",  // U+FF0E 全角句号
		"http://①②⑦.0.0.1/",  // 圈号数字（无法确定性映射 → fail-closed）
		"http://１２７.0.0.1/",  // 全角数字
		"http://127｡0｡0｡1/",  // U+FF61 半宽表意句号
		"http://１２７．０．０．１/",  // 全角数字 + 全角句号组合
		"http://localhost/",  // ASCII 约定域基线
		"http://loｃalhost/",  // 全角拉丁折成 localhost
		"http://x｡internal/", // 全角句点 + 约定域后缀（折成 x.internal）
	} {
		if err := IsBlockedURL(raw); err == nil {
			t.Errorf("%q 应被阻断（Unicode 形态的回环/约定域），实际放行", raw)
		}
	}
}

// TestCheckURL_BlockReasonsForUnicodeForms 折叠形态阻断原因走 IP 网段判定
// （错误信息含回环段），不可折叠形态走 fail-closed 拒绝——两者可区分。
func TestCheckURL_BlockReasonsForUnicodeForms(t *testing.T) {
	folded := map[string]string{
		"http://127。0。0。1/": "blocked range",
		"http://１２７.0.0.1/": "blocked range",
	}
	for raw, want := range folded {
		err := IsBlockedURL(raw)
		if err == nil {
			t.Errorf("%q 应被阻断", raw)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q 阻断原因应含 %q，got %v", raw, want, err)
		}
	}
	err := IsBlockedURL("http://①②⑦.0.0.1/")
	if err == nil {
		t.Fatal("圈号 host 应 fail-closed 拒绝")
	}
	if !strings.Contains(err.Error(), "non-ASCII") {
		t.Errorf("圈号 host 拒绝原因应说明 non-ASCII fail-closed，got %v", err)
	}
}

// TestCheckURL_FoldedExoticForms 折叠后的 exotic IPv4 形态照走既有闸门：
// 全角句点拼出的 127｡1 折成 127.1，inet_aton 语义可解，必须被 exotic 闸门拦下。
func TestCheckURL_FoldedExoticForms(t *testing.T) {
	for _, raw := range []string{
		"http://127｡1/",      // → 127.1
		"http://２１３０７０６４３３/", // → 2130706433（= 127.0.0.1 的十进制整型）
	} {
		err := IsBlockedURL(raw)
		if err == nil {
			t.Errorf("%q 折叠后是 exotic IPv4 字面量，应被闸门阻断", raw)
			continue
		}
		if !strings.Contains(err.Error(), "exotic IPv4") {
			t.Errorf("%q 阻断原因应是 exotic IPv4 字面量，got %v", raw, err)
		}
	}
}

// TestCheckURL_AllowsASCIIPublicDomains 折叠判定不伤 ASCII 公网域名：
// 纯 ASCII 走快路径零折叠，普通域名照常放行。
func TestCheckURL_AllowsASCIIPublicDomains(t *testing.T) {
	for _, raw := range []string{
		"https://example.com/hook",
		"https://api.example.com:8443/hook",
		"http://8.8.8.8/",
	} {
		if err := IsBlockedURL(raw); err != nil {
			t.Errorf("%q 应放行，实际阻断：%v", raw, err)
		}
	}
}

// TestAllowPrivate_UnicodeForms 豁免与折叠的交互口径（pkg#236 之上再钉 #266）：
// 可确定性折叠成**标准点分十进制**的全角形态在 allowPrivate 下与等价 ASCII
// 形态同判（豁免的就是网段判定——用户写了标准形态本就放行）；不可折叠形态
// （fail-closed）与折叠成 exotic 字面量的形态照拒——字面量形态闸门不受豁免。
func TestAllowPrivate_UnicodeForms(t *testing.T) {
	g, err := New(WithAllowPrivate())
	if err != nil {
		t.Fatal(err)
	}
	// 与 ASCII 等价物同判：allowPrivate 下放行（折叠结果 = 标准点分十进制）。
	for _, raw := range []string{
		"http://127。0。0。1/",
		"http://１２７.0.0.1/",
		"http://10｡0｡0｡5/",
	} {
		if err := g.CheckURL(raw); err != nil {
			t.Errorf("allowPrivate 下 %q 应放行（等价标准形态在豁免内）：%v", raw, err)
		}
	}
	// 仍拒：fail-closed 与 exotic 字面量闸门不受 allowPrivate 影响。
	for _, raw := range []string{
		"http://①②⑦.0.0.1/",  // 无法确定性映射 → fail-closed
		"http://２１３０７０６４３３/", // 折叠成十进制整型字面量 → exotic 闸门
	} {
		if err := g.CheckURL(raw); err == nil {
			t.Errorf("allowPrivate 下 %q 仍应被拒（字面量形态判定不受豁免）", raw)
		}
	}
	// allowPrivate 的既有语义不回归：标准点分十进制/方括号 IPv6 照常放行。
	for _, raw := range []string{"http://127.0.0.1/", "http://[::1]/", "http://10.0.0.5/"} {
		if err := g.CheckURL(raw); err != nil {
			t.Errorf("allowPrivate 下 %q 应放行：%v", raw, err)
		}
	}
}

// TestCheckHost_NonASCII_FailClosed host 维度（CheckHost）同口径：
// 非 ASCII 一律拒绝，折叠后的全角形态落到对应判定。
func TestCheckHost_NonASCII_FailClosed(t *testing.T) {
	g, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{
		"127。0。0。1",           // → 127.0.0.1，回环阻断
		"例え.jp",               // 非 ASCII IDN，fail-closed
		"xn--example-9x4j.jp", // punycode（ASCII）走 DNS 判定路径，放行不在此列
	} {
		err := g.CheckHost(host)
		if host == "xn--example-9x4j.jp" {
			if err != nil {
				t.Errorf("punycode host %q 是 ASCII，应走普通判定放行：%v", host, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("CheckHost(%q) 应被拒", host)
		}
	}
}
