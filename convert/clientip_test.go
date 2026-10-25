package convert

import (
	"net/http/httptest"
	"testing"
)

// TestResolveClientIP 表驱动覆盖纯函数的采信口径：直连不可信 / 可信代理链 /
// 伪造 XFF / 全链路可信 / X-Real-IP 回落 / 无可信代理 / IPv6 / 裸 IP 对端。
func TestResolveClientIP(t *testing.T) {
	cases := []struct {
		name    string
		remote  string
		xff     string
		xri     string
		trusted []string
		want    string
	}{
		{
			name:    "直连不可信时忽略一切代理头",
			remote:  "5.6.7.8:9999",
			xff:     "9.9.9.9",
			xri:     "8.8.8.8",
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
			name:    "可信代理且无 XFF 时回退 X-Real-IP",
			remote:  "10.0.0.2:12345",
			xri:     "1.2.3.4",
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
			name:    "XFF 为空串时走 X-Real-IP",
			remote:  "10.0.0.2:12345",
			xff:     "",
			xri:     " 1.2.3.4 ",
			trusted: defaultTrustedProxies,
			want:    "1.2.3.4",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveClientIP(tc.remote, tc.xff, tc.xri, tc.trusted); got != tc.want {
				t.Errorf("ResolveClientIP(%q, %q, %q) = %q, want %q",
					tc.remote, tc.xff, tc.xri, got, tc.want)
			}
		})
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

			want := ResolveClientIP(tc.remote, tc.xff, tc.xri, trusted)
			if got := GetClientIPWithProxies(r, trusted); got != want {
				t.Errorf("GetClientIPWithProxies = %q, want %q (must match ResolveClientIP)", got, want)
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
