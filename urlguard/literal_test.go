package urlguard

import (
	"context"
	"strings"
	"testing"
)

// exoticIPv4Cases 是「宽松解析器（inet_aton 语义）会当成 IPv4、标准库 net.ParseIP
// 不认」的 exotic 字面量形态——写面放行等于把判定推给拨号层，构成回环绕过路径。
var exoticIPv4Cases = []string{
	"0x7f000001",   // 十六进制 32 位 → 127.0.0.1
	"0X7F000001",   // 大写 0X 前缀
	"0x7f.0.0.1",   // 十六进制逐段
	"0177.0.0.1",   // 八进制逐段 → 127.0.0.1
	"030052000001", // 八进制 32 位 → 127.0.0.1
	"127.1",        // 两段式（末段 24 位）→ 127.0.0.1
	"127.0.1",      // 三段式（末段 16 位）→ 127.0.0.1
	"2130706433",   // 十进制 32 位 → 127.0.0.1
	"192.168.1",    // 三段式 → 192.168.0.1
	"10.0.1",       // 三段式 → 10.0.0.1
	"01.02.03.04",  // 八进制逐段
}

// TestCheckURL_BlocksExoticIPv4Literals exotic IPv4 字面量在写面必须被拒
// （此前 net.ParseIP 返回 nil ⇒ 当普通域名放行）。
func TestCheckURL_BlocksExoticIPv4Literals(t *testing.T) {
	g := mustGuard(t)
	for _, host := range exoticIPv4Cases {
		raw := "http://" + host + "/hook"
		err := g.CheckURL(raw)
		if err == nil {
			t.Errorf("%s 应被阻断，实际放行", raw)
			continue
		}
		if !strings.Contains(err.Error(), "exotic IPv4") {
			t.Errorf("%s 阻断原因应是 exotic IPv4 字面量，got %v", raw, err)
		}
	}
}

// TestCheckHost_BlocksExoticIPv4Literals CheckHost 口径与 CheckURL 同源
// （写面与 host 判定必须一致，不能一个放行一个拒绝）。
func TestCheckHost_BlocksExoticIPv4Literals(t *testing.T) {
	g := mustGuard(t)
	for _, host := range exoticIPv4Cases {
		if err := g.CheckHost(host); err == nil {
			t.Errorf("host %q 应被阻断，实际放行", host)
		}
	}
}

// TestCheckURL_AllowsNonExoticHosts 词法判定不得误伤正常域名与正常 IP。
func TestCheckURL_AllowsNonExoticHosts(t *testing.T) {
	g := mustGuard(t)
	allowed := []string{
		"http://example.com/hook",
		"http://8.8.8.8/hook",            // 正常点分十进制
		"http://1.2.3.4/hook",            // 正常点分十进制
		"http://0xnothex.example.com/",   // 含 0x 但不是字面量
		"http://a1.b2.example.com/",      // 含字母
		"http://host-1.example.com/",     // 含连字符
		"http://xn--fiqs8s.example.com/", // punycode
		"http://1.2.3.example.com/",      // 数字段 + 字母段
	}
	for _, raw := range allowed {
		if err := g.CheckURL(raw); err != nil {
			t.Errorf("%s 应放行，实际阻断：%v", raw, err)
		}
	}
}

// TestLooksLikeExoticIPv4 exotic 判定的词法口径（表驱动）。
func TestLooksLikeExoticIPv4(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"0x7f000001", true},
		{"0X7f000001", true},
		{"0x7f.0.0.1", true},
		{"0177.0.0.1", true},
		{"127.1", true},
		{"2130706433", true},
		{"1", true},
		{"1.2", true},
		{"1.2.3", true},
		{"01.02.03.04", true},
		{"8.8.8.8", false}, // 正常四段十进制（net.ParseIP 先判）
		{"1.2.3.4", false}, // 正常四段十进制
		{"example.com", false},
		{"1.2.3.4.5", false}, // 超四段 ⇒ 不是 inet_aton 形态
		{"0xzz", false},      // 非法十六进制
		{"1.2.3.", false},    // 空段
		{"", false},
	}
	for _, c := range cases {
		if got := looksLikeExoticIPv4(c.host); got != c.want {
			t.Errorf("looksLikeExoticIPv4(%q)=%v, want %v", c.host, got, c.want)
		}
	}
}

// TestCheckURL_BlocksIPv6SpecialUseAndNAT64 IPv6 特殊用途段与 NAT64 段必须阻断：
// 64:ff9b::7f00:1 即 127.0.0.1 的 IPv6 写法，2002::/16 可内嵌任意 IPv4。
func TestCheckURL_BlocksIPv6SpecialUseAndNAT64(t *testing.T) {
	g := mustGuard(t)
	blocked := []string{
		"http://[64:ff9b::7f00:1]/hook", // NAT64 well-known prefix → 127.0.0.1
		"http://[64:ff9b::1]/hook",      // NAT64 well-known prefix
		"http://[64:ff9b:1::1]/hook",    // NAT64 local-use prefix
		"http://[2001:db8::1]/hook",     // 文档段
		"http://[2002::1]/hook",         // 6to4
		"http://[2002:7f00:1::]/hook",   // 6to4 内嵌 127.0.0.1
		"http://[2001::1]/hook",         // Teredo
		"http://[100::1]/hook",          // discard-only
		"http://[2001:2::1]/hook",       // benchmarking
		"http://[2001:10::1]/hook",      // ORCHID
		"http://[2001:20::1]/hook",      // ORCHIDv2
	}
	for _, raw := range blocked {
		if err := g.CheckURL(raw); err == nil {
			t.Errorf("%s 应被阻断，实际放行", raw)
		}
	}
}

// TestCheckURL_AllowsPublicIPv6 全球可达 IPv6 不得被误伤。
func TestCheckURL_AllowsPublicIPv6(t *testing.T) {
	g := mustGuard(t)
	allowed := []string{
		"http://[2606:4700:4700::1111]/hook", // Cloudflare 1.1.1.1
		"http://[2001:4860:4860::8888]/hook", // Google DNS
	}
	for _, raw := range allowed {
		if err := g.CheckURL(raw); err != nil {
			t.Errorf("%s 应放行，实际阻断：%v", raw, err)
		}
	}
}

// TestDialFunc_BlocksExoticIPv4 拨号层同样按单源判定拒 exotic 字面量
// （不把它推给 DNS 解析）。
func TestDialFunc_BlocksExoticIPv4(t *testing.T) {
	g := mustGuard(t)
	dial := g.DialFunc(nil)
	_, err := dial(context.Background(), "tcp", "127.1:80")
	if err == nil {
		t.Fatal("拨号层应拒绝 exotic IPv4 字面量")
	}
	if !strings.Contains(err.Error(), "exotic IPv4") {
		t.Fatalf("拒绝原因应是 exotic IPv4 字面量，got %v", err)
	}
}
