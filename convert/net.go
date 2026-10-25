package convert

import (
	"net"
	"net/http"
	"strings"
)

// defaultTrustedProxies 是默认的可信代理 CIDR 列表（内网地址 + 回环地址）。
var defaultTrustedProxies = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"::1/128",   // IPv6 loopback
	"fc00::/7",  // IPv6 unique local address
	"fe80::/10", // IPv6 link-local
}

// DefaultTrustedProxies 返回默认可信代理 CIDR 列表的副本。
//
// 返回副本而非包级切片本身：包级切片一旦被调用方改写（append / 排序 / 改元素），
// 会静默改变本包 GetClientIP 的判定口径。框架适配层（如 Hertz 侧）用本函数
// 取同一份默认口径，避免各框架各抄一份内网网段清单后各自漂移。
func DefaultTrustedProxies() []string {
	out := make([]string, len(defaultTrustedProxies))
	copy(out, defaultTrustedProxies)
	return out
}

// GetClientIP 从 HTTP 请求中提取真实客户端 IP。
// 优先级：X-Forwarded-For（仅当请求来自可信代理时） > X-Real-IP > RemoteAddr。
//
// 安全说明：默认只信任内网代理（10/8, 172.16/12, 192.168/16, 127/8）。
// 如果部署在公网代理后面（如 Cloudflare），需要通过 GetClientIPWithProxies 指定可信代理列表。
func GetClientIP(r *http.Request) string {
	return GetClientIPWithProxies(r, defaultTrustedProxies)
}

// GetClientIPWithProxies 从 HTTP 请求中提取真实客户端 IP，指定可信代理 CIDR 列表。
// 只有当 RemoteAddr 属于可信代理时，才信任 X-Forwarded-For 和 X-Real-IP 头。
// trustedProxies 为 nil 或空时，只使用 RemoteAddr。
//
// 本函数是 net/http 适配层，算法本体在 ResolveClientIP（非 net/http 框架也可复用）。
func GetClientIPWithProxies(r *http.Request, trustedProxies []string) string {
	return ResolveClientIP(
		r.RemoteAddr,
		r.Header.Get("X-Forwarded-For"),
		r.Header.Get("X-Real-IP"),
		trustedProxies,
	)
}

// ResolveClientIP 是框架无关的客户端 IP 解析纯函数——HTTP 框架只负责把
// 「对端地址 + 两个代理头」取出来，采信判定全部在本函数内。
//
// 入参：
//   - remote：对端地址，形态为 "ip:port" 或裸 IP（IPv4 / IPv6）；解析不出端口时按原样当 IP 用。
//   - xff：X-Forwarded-For 头原文，多段以逗号分隔。
//   - xri：X-Real-IP 头原文。
//   - trustedProxies：可信代理 CIDR 列表；nil 或空时只认 remote（代理头一律忽略）。
//
// 采信规则：
//   - remote 不在 trustedProxies 内 ⇒ 返回 remote，**忽略一切代理头**
//     （直连方可以随意伪造 XFF，任何代理头都不可信）。
//   - remote 在 trustedProxies 内 ⇒ 从 XFF **右向左**扫描，跳过可信代理，
//     返回第一个非可信 IP；XFF 缺失时回退 X-Real-IP；两者都缺失时返回 remote。
//   - XFF 整条链都是可信代理（如内网互调）⇒ 回退到最左端的原始发起方。
//
// 为什么从右向左：XFF 语义是每个代理把上一跳追加到右端，左侧是客户端可任意伪造的；
// 从左向右扫描会把攻击者注入的伪造值当成客户端 IP（限流 key / 审计 / 访问控制被绕过）。
//
// 纯函数形态便于单测覆盖伪造 XFF 场景，也便于非 net/http 框架（Hertz 等）
// 复用同一份采信口径，避免各仓各写一遍后口径漂移。
func ResolveClientIP(remote, xff, xri string, trustedProxies []string) string {
	remoteIP := hostOnly(remote)

	if len(trustedProxies) > 0 && isTrustedProxy(remoteIP, trustedProxies) {
		if xff != "" {
			parts := strings.Split(xff, ",")
			for i := len(parts) - 1; i >= 0; i-- {
				ip := strings.TrimSpace(parts[i])
				if ip != "" && !isTrustedProxy(ip, trustedProxies) {
					return ip
				}
			}
			// 全链路都是可信代理（如内网健康检查）：回退到最左端的原始发起方
			if len(parts) > 0 {
				return strings.TrimSpace(parts[0])
			}
		}

		if xri != "" {
			return strings.TrimSpace(xri)
		}
	}

	return remoteIP
}

// hostOnly 从 "ip:port" 形态的地址取 IP 部分；解析不出端口时按原样返回
// （裸 IP 形态，含 IPv6 的 "::1" —— SplitHostPort 对其报 "too many colons"）。
func hostOnly(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// isTrustedProxy 检查 IP 是否在可信代理列表中。
func isTrustedProxy(ipStr string, trustedProxies []string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, cidr := range trustedProxies {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
