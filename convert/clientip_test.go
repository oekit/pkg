package convert

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestResolveClientIP 表驱动覆盖纯函数的采信口径：直连不可信 / 可信代理链 /
// 伪造 XFF / 全链路可信 / 无可信代理 / IPv6 / 裸 IP 对端 / 候选值非法 / 端口归一。
//
// ⚠️ 默认形态**不采信 X-Real-IP**（pkg#106）——XRI 相关用例在
// TestResolveClientIPWithXRI 里单独覆盖。
func TestResolveClientIP(t *testing.T) {
	cases := []struct {
		name    string
		remote  string
		xff     string
		trusted []string
		want    string
	}{
		{
			name:    "直连不可信时忽略一切代理头",
			remote:  "5.6.7.8:9999",
			xff:     "9.9.9.9",
			trusted: defaultTrustedProxies,
			want:    "5.6.7.8",
		},
		{
			name:    "可信代理链从右向左取第一个非可信 IP",
			remote:  "10.0.0.2:12345",
			xff:     "9.9.9.9, 1.2.3.4, 10.0.0.1",
			trusted: defaultTrustedProxies,
			want:    "1.2.3.4",
		},
		{
			name:    "干净代理链",
			remote:  "10.0.0.2:12345",
			xff:     "1.2.3.4, 10.0.0.1",
			trusted: defaultTrustedProxies,
			want:    "1.2.3.4",
		},
		{
			name:    "全链路可信回退最左端",
			remote:  "10.0.0.2:12345",
			xff:     "10.0.0.0, 10.0.0.1",
			trusted: defaultTrustedProxies,
			want:    "10.0.0.0",
		},
		{
			name:    "无可信代理清单时只认对端地址",
			remote:  "10.0.0.2:12345",
			xff:     "1.2.3.4",
			trusted: nil,
			want:    "10.0.0.2",
		},
		{
			name:    "自定义可信网段",
			remote:  "203.0.113.5:443",
			xff:     "1.2.3.4",
			trusted: []string{"203.0.113.0/24"},
			want:    "1.2.3.4",
		},
		{
			name:    "裸 IP 对端（无端口）",
			remote:  "1.2.3.4",
			xff:     "9.9.9.9",
			trusted: defaultTrustedProxies,
			want:    "1.2.3.4",
		},
		{
			name:    "IPv6 对端带端口",
			remote:  "[2001:db8::1]:443",
			trusted: defaultTrustedProxies,
			want:    "2001:db8::1",
		},
		{
			name:    "IPv6 裸地址对端",
			remote:  "::1",
			trusted: defaultTrustedProxies,
			want:    "::1",
		},
		{
			name:    "XFF 段带空白",
			remote:  "10.0.0.2:12345",
			xff:     " 1.2.3.4 , 10.0.0.1 ",
			trusted: defaultTrustedProxies,
			want:    "1.2.3.4",
		},
		{
			name:    "XFF 为空串时回退对端地址",
			remote:  "10.0.0.2:12345",
			xff:     "",
			trusted: defaultTrustedProxies,
			want:    "10.0.0.2",
		},
		{
			name:    "空对端地址返回空串",
			remote:  "",
			xff:     "1.2.3.4",
			trusted: defaultTrustedProxies,
			want:    "",
		},
		{
			name:    "候选值非法时跳过该段取下一个合法候选",
			remote:  "10.0.0.2:12345",
			xff:     "not-an-ip, 1.2.3.4, 10.0.0.1",
			trusted: defaultTrustedProxies,
			want:    "1.2.3.4",
		},
		{
			name:    "候选值带端口归一为裸 IP",
			remote:  "10.0.0.2:12345",
			xff:     "1.2.3.4:54321",
			trusted: defaultTrustedProxies,
			want:    "1.2.3.4",
		},
		{
			name:    "整条链都非法时回退对端地址",
			remote:  "10.0.0.2:12345",
			xff:     "not-an-ip, also-bad",
			trusted: defaultTrustedProxies,
			want:    "10.0.0.2",
		},
		{
			name:    "IPv6 候选值归一为压缩写法",
			remote:  "10.0.0.2:12345",
			xff:     "2001:0db8:0000::0001",
			trusted: defaultTrustedProxies,
			want:    "2001:db8::1",
		},
		{
			name:    "裸 IP 形态的可信代理条目生效",
			remote:  "203.0.113.5:443",
			xff:     "1.2.3.4",
			trusted: []string{"203.0.113.5"},
			want:    "1.2.3.4",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveClientIP(tc.remote, tc.xff, tc.trusted); got != tc.want {
				t.Errorf("ResolveClientIP(%q, %q) = %q, want %q",
					tc.remote, tc.xff, got, tc.want)
			}
		})
	}
}

// TestResolveClientIP_IgnoresXRealIP 钉住默认形态**不采信 X-Real-IP**（pkg#106）。
//
// X-Real-IP 是单跳头，HTTP 头本身无法区分「代理写的」与「客户端自带的」：大量
// 网关（Envoy / Higress / Traefik）默认不写该头，但会原样透传客户端自带的。
// 若默认采信它，客户端可以任意改写自己的客户端 IP —— 限流/锁定计数桶被绕过、
// 审计与访问日志的来源 IP 可被改写。
//
// 本例的 XRI 值是公网地址，即便做了合法性校验也会被接受（真实客户端 IP 也是
// 公网地址），所以「XRI 也走可信链判定」救不了它 —— 只有默认不采信才行。
func TestResolveClientIP_IgnoresXRealIP(t *testing.T) {
	got := ResolveClientIP("10.0.0.2:12345", "", defaultTrustedProxies)
	if got != "10.0.0.2" {
		t.Errorf("ResolveClientIP with X-Real-IP = %q, want 10.0.0.2 (XRI must not be trusted by default)", got)
	}

	// XFF 存在时，XRI 也不参与（XFF 决定结果）。
	got = ResolveClientIP("10.0.0.2:12345", "1.2.3.4", defaultTrustedProxies)
	if got != "1.2.3.4" {
		t.Errorf("ResolveClientIP = %q, want 1.2.3.4", got)
	}
}

// TestResolveClientIPWithXRI 覆盖显式开启 X-Real-IP 采信的形态。
func TestResolveClientIPWithXRI(t *testing.T) {
	cases := []struct {
		name    string
		remote  string
		xff     string
		xri     string
		trusted []string
		want    string
	}{
		{
			name:    "可信代理且无 XFF 时回退 X-Real-IP",
			remote:  "10.0.0.2:12345",
			xri:     "1.2.3.4",
			trusted: defaultTrustedProxies,
			want:    "1.2.3.4",
		},
		{
			name:    "XFF 为空串时回退 X-Real-IP",
			remote:  "10.0.0.2:12345",
			xff:     "",
			xri:     " 1.2.3.4 ",
			trusted: defaultTrustedProxies,
			want:    "1.2.3.4",
		},
		{
			name:    "直连不可信时 X-Real-IP 也忽略",
			remote:  "5.6.7.8:9999",
			xri:     "1.2.3.4",
			trusted: defaultTrustedProxies,
			want:    "5.6.7.8",
		},
		{
			name:    "XFF 优先于 X-Real-IP",
			remote:  "10.0.0.2:12345",
			xff:     "1.2.3.4",
			xri:     "9.9.9.9",
			trusted: defaultTrustedProxies,
			want:    "1.2.3.4",
		},
		{
			name:    "X-Real-IP 非法时回退对端地址",
			remote:  "10.0.0.2:12345",
			xri:     "not-an-ip",
			trusted: defaultTrustedProxies,
			want:    "10.0.0.2",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveClientIPWithXRI(tc.remote, tc.xff, tc.xri, tc.trusted); got != tc.want {
				t.Errorf("ResolveClientIPWithXRI(%q, %q, %q) = %q, want %q",
					tc.remote, tc.xff, tc.xri, got, tc.want)
			}
		})
	}
}

// TestNewTrustedProxySet_BareIPNormalized 钉住裸 IP 形态的可信代理条目被归一为
// 单主机网段（pkg#104）——旧实现直接 continue，配置不生效且无任何告警。
func TestNewTrustedProxySet_BareIPNormalized(t *testing.T) {
	set, err := NewTrustedProxySet([]string{"203.0.113.5", "2001:db8::1", "10.0.0.0/8"})
	if err != nil {
		t.Fatalf("NewTrustedProxySet: %v", err)
	}
	if got := set.Len(); got != 3 {
		t.Fatalf("Len() = %d, want 3", got)
	}
	if !set.ContainsString("203.0.113.5") {
		t.Error("bare IPv4 entry must be honored (was silently ignored before #104)")
	}
	if !set.ContainsString("2001:db8::1") {
		t.Error("bare IPv6 entry must be honored (was silently ignored before #104)")
	}
	if set.ContainsString("203.0.113.6") {
		t.Error("bare IPv4 entry must not widen to the whole segment")
	}
	if !set.ContainsString("10.1.2.3") {
		t.Error("CIDR entry must still match")
	}
}

// TestNewTrustedProxySet_InvalidEntryFailsFast 钉住非法条目在构造期报错，
// 而不是静默跳过（静默跳过 = 配置写错被吞成安全策略降级且不可观测）。
func TestNewTrustedProxySet_InvalidEntryFailsFast(t *testing.T) {
	_, err := NewTrustedProxySet([]string{"10.0.0.0/8", "not-an-ip"})
	if err == nil {
		t.Fatal("NewTrustedProxySet must fail on invalid entry")
	}
	if !strings.Contains(err.Error(), "not-an-ip") {
		t.Errorf("error must name the offending entry, got %v", err)
	}
}

// TestResolveClientIP_ListVsSetEquivalence 钉住清单形态与集合形态的采信结果
// 逐字一致——集合形态只是把清单解析从请求期挪到构造期，判定规则不许有分叉。
func TestResolveClientIP_ListVsSetEquivalence(t *testing.T) {
	trusted := []string{"10.0.0.0/8", "203.0.113.5"}
	set, err := NewTrustedProxySet(trusted)
	if err != nil {
		t.Fatalf("NewTrustedProxySet: %v", err)
	}

	cases := []struct {
		remote string
		xff    string
	}{
		{"10.0.0.2:12345", "9.9.9.9, 1.2.3.4, 10.0.0.1"},
		{"10.0.0.2:12345", "10.0.0.0, 10.0.0.1"},
		{"203.0.113.5:443", "1.2.3.4"},
		{"203.0.113.6:443", "1.2.3.4"},
		{"5.6.7.8:9999", "9.9.9.9"},
		{"10.0.0.2:12345", "not-an-ip"},
	}

	for _, tc := range cases {
		t.Run(tc.remote+"/"+tc.xff, func(t *testing.T) {
			want := ResolveClientIP(tc.remote, tc.xff, trusted)
			got := ResolveClientIPWithSet(tc.remote, tc.xff, set)
			if got != want {
				t.Errorf("set form = %q, list form = %q (must be identical)", got, want)
			}
		})
	}
}

// TestResolveClientIPWithSet_NilSet nil 集合等价于「不信任任何代理头」。
func TestResolveClientIPWithSet_NilSet(t *testing.T) {
	if got := ResolveClientIPWithSet("10.0.0.2:12345", "1.2.3.4", nil); got != "10.0.0.2" {
		t.Errorf("ResolveClientIPWithSet with nil set = %q, want 10.0.0.2", got)
	}
}

// TestGetClientIPWithProxies_MatchesResolveClientIP 钉住 net/http 适配层与纯函数
// 逐字一致——适配层只是把请求拆成三个入参，采信判定只有一处实现。
// 纯函数新增后若有人改了两边任一处的判定，这里即红。
func TestGetClientIPWithProxies_MatchesResolveClientIP(t *testing.T) {
	cases := []struct {
		name    string
		remote  string
		xff     string
		xri     string
		trusted []string
	}{
		{name: "直连不可信", remote: "5.6.7.8:9999", xff: "9.9.9.9"},
		{name: "伪造 XFF", remote: "10.0.0.2:12345", xff: "9.9.9.9, 1.2.3.4, 10.0.0.1"},
		{name: "全链路可信", remote: "10.0.0.2:12345", xff: "10.0.0.0, 10.0.0.1"},
		{name: "仅 X-Real-IP", remote: "10.0.0.2:12345", xri: "1.2.3.4"},
		{name: "无可信代理", remote: "10.0.0.2:12345", xff: "1.2.3.4", trusted: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.remote
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.xri != "" {
				r.Header.Set("X-Real-IP", tc.xri)
			}
			trusted := tc.trusted
			if trusted == nil && tc.name != "无可信代理" {
				trusted = defaultTrustedProxies
			}

			want := ResolveClientIP(tc.remote, tc.xff, trusted)
			if got := GetClientIPWithProxies(r, trusted); got != want {
				t.Errorf("GetClientIPWithProxies = %q, want %q (must match ResolveClientIP)", got, want)
			}

			wantXRI := ResolveClientIPWithXRI(tc.remote, tc.xff, tc.xri, trusted)
			if got := GetClientIPWithProxiesAndXRI(r, trusted); got != wantXRI {
				t.Errorf("GetClientIPWithProxiesAndXRI = %q, want %q (must match ResolveClientIPWithXRI)", got, wantXRI)
			}
		})
	}
}

// TestDefaultTrustedProxies_ReturnsCopy 钉住返回的是副本：调用方改写返回值
// 不得改变本包 GetClientIP 的判定口径。
func TestDefaultTrustedProxies_ReturnsCopy(t *testing.T) {
	first := DefaultTrustedProxies()
	if len(first) == 0 {
		t.Fatal("DefaultTrustedProxies() returned empty list")
	}
	for i := range first {
		first[i] = "0.0.0.0/0"
	}

	second := DefaultTrustedProxies()
	for i, v := range second {
		if v == "0.0.0.0/0" {
			t.Fatalf("DefaultTrustedProxies()[%d] = %q, caller mutation leaked into package state", i, v)
		}
	}
}
