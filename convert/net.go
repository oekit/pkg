package convert

import (
	"fmt"
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

// ===== 可信代理集合（预解析，请求期零 CIDR 解析）=====

// TrustedProxySet 是预解析的可信代理集合。
//
// 存在的理由：按清单逐条 net.ParseCIDR 的写法每请求都要把整份清单重新解析一遍
// （清单 N 条 ⇒ 每请求 N 次 ParseCIDR），而解析结果在清单不变时是恒定的
// （pkg#107）。构造期解析一次、请求期只做网段包含判定，热路径成本降为一次
// 预解析 + 若干次位运算。
//
// 本类型不可变：构造后只读，可安全跨 goroutine 共享。
type TrustedProxySet struct {
	nets []*net.IPNet
}

// NewTrustedProxySet 解析可信代理清单为预解析集合（严格模式：非法条目返回错误）。
//
// 条目形态：
//   - CIDR（"10.0.0.0/8"）：按原样使用；
//   - 裸 IP（"203.0.113.5"）：归一为单主机网段（IPv4 /32、IPv6 /128）；
//   - 其它：解析失败即返回错误，**不静默跳过**。
//
// 为什么裸 IP 必须归一而不是忽略：清单里写 "203.0.113.5" 的本意是「信任这一台
// 代理」，而 net.ParseCIDR 对裸 IP 解析失败，旧实现直接 continue —— 于是配置
// **不生效且无任何告警**（pkg#104）：可信代理悄悄退化成「不信任任何代理头」，
// 表现为限流 key 变成代理 IP、审计里看不到真实客户端，而查配置看不出任何异常。
//
// 为什么非法条目必须报错而不是静默跳过：静默跳过把「配置写错」变成「安全策略
// 降级」且不可观测；fail-fast 让配置在加载期就红。
//
// 空清单合法（等价于「不信任任何代理头」，fail-closed）。
func NewTrustedProxySet(proxies []string) (*TrustedProxySet, error) {
	nets, err := trustedProxyNetworks(proxies, true)
	if err != nil {
		return nil, err
	}
	return &TrustedProxySet{nets: nets}, nil
}

// Contains 判断 IP 是否落在集合内；集合为空或 IP 非法时返回 false。
func (s *TrustedProxySet) Contains(ip net.IP) bool {
	if s == nil || ip == nil {
		return false
	}
	for _, network := range s.nets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// ContainsString 判断字符串形态的 IP 是否落在集合内；解析失败返回 false。
func (s *TrustedProxySet) ContainsString(ipStr string) bool {
	if s == nil {
		return false
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return s.Contains(ip)
}

// Len 返回集合内的网段条数。
func (s *TrustedProxySet) Len() int {
	if s == nil {
		return 0
	}
	return len(s.nets)
}

// networks 返回底层网段列表（nil 安全的只读访问）。
func (s *TrustedProxySet) networks() []*net.IPNet {
	if s == nil {
		return nil
	}
	return s.nets
}

// trustedProxyNetworks 把可信代理清单解析为网段列表。
//
// strict=true：非法条目返回错误（构造期 fail-fast）。
// strict=false：非法条目跳过（列表形态的历史口径，见 ResolveClientIP 的说明）。
// 裸 IP 在两种模式下都归一为单主机网段（pkg#104）。
func trustedProxyNetworks(proxies []string, strict bool) ([]*net.IPNet, error) {
	nets := make([]*net.IPNet, 0, len(proxies))
	for _, raw := range proxies {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		network, err := parseTrustedProxy(item)
		if err != nil {
			if strict {
				return nil, err
			}
			continue
		}
		nets = append(nets, network)
	}
	return nets, nil
}

// parseTrustedProxy 把单条目解析为网段：CIDR 原样，裸 IP 归一为单主机网段。
func parseTrustedProxy(item string) (*net.IPNet, error) {
	if strings.Contains(item, "/") {
		_, network, err := net.ParseCIDR(item)
		if err != nil {
			return nil, fmt.Errorf("convert: 可信代理条目 %q 不是合法 CIDR: %w", item, err)
		}
		return network, nil
	}
	ip := net.ParseIP(item)
	if ip == nil {
		return nil, fmt.Errorf("convert: 可信代理条目 %q 不是合法 IP 或 CIDR", item)
	}
	if v4 := ip.To4(); v4 != nil {
		return &net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)}, nil
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}, nil
}

// ===== 客户端 IP 解析（纯函数）=====

// ResolveClientIP 是框架无关的客户端 IP 解析纯函数——HTTP 框架只负责把
// 「对端地址 + X-Forwarded-For 头」取出来，采信判定全部在本函数内。
//
// 入参：
//   - remote：对端地址，形态为 "ip:port" 或裸 IP（IPv4 / IPv6）；解析不出端口时按原样当 IP 用。
//   - xff：X-Forwarded-For 头原文，多段以逗号分隔。
//   - trustedProxies：可信代理清单（CIDR 或裸 IP）；nil 或空时只认 remote（代理头一律忽略）。
//
// 采信规则：
//   - remote 不在可信代理内 ⇒ 返回 remote，**忽略一切代理头**
//     （直连方可以随意伪造 XFF，任何代理头都不可信）。
//   - remote 在可信代理内 ⇒ 从 XFF **右向左**扫描，跳过可信代理，
//     返回第一个非可信 IP；XFF 整条链都是可信代理（如内网互调）⇒ 回退到最左端的
//     原始发起方；XFF 缺失或整条链都没有合法 IP ⇒ 返回 remote。
//
// 为什么从右向左：XFF 语义是每个代理把上一跳追加到右端，左侧是客户端可任意伪造的；
// 从左向右扫描会把攻击者注入的伪造值当成客户端 IP（限流 key / 审计 / 访问控制被绕过）。
//
// 候选值合法性：XFF 的每一段都必须能归一为合法 IP 才参与判定——非法段直接跳过，
// 不进返回值。理由有二：任意字符串进限流/锁定键等于「伪造 IP 永不命中计数 = 绕过」，
// 且可注入任意内容污染审计日志；带端口的候选归一为裸 IP，避免同一客户端换个端口
// 就换一个分桶键（pkg#104）。
//
// 与 v0.25.2 的差异（BREAKING）：
//   - **不再读取 X-Real-IP**。X-Real-IP 是单跳头，HTTP 头本身无法区分
//     「代理写的」与「客户端自带的」——大量网关（Envoy / Higress / Traefik）默认
//     不写该头，但会**原样透传**客户端自带的 X-Real-IP，于是 XFF 那条有右向左
//     可信链保护的路径与 XRI 这条没有保护的路径信任强度不对等：客户端可以任意
//     改写自己的客户端 IP（pkg#106）。需要采信 X-Real-IP 的部署请显式调用
//     ResolveClientIPWithXRI。
//   - 裸 IP 形态的可信代理条目现在生效（此前被静默忽略，pkg#104）。
//   - 候选值带端口时归一为裸 IP；非法候选值不再进入返回值。
//
// 清单形态的调用每次请求都会重新解析清单（热路径不划算）；高 QPS 场景请用
// NewTrustedProxySet 构造一次、之后走 ResolveClientIPWithSet（pkg#107）。
//
// 清单里的非法条目在本形态下被跳过（历史口径，函数签名无法回报错误）；需要
// fail-fast 的配置加载路径请用 NewTrustedProxySet。
func ResolveClientIP(remote, xff string, trustedProxies []string) string {
	nets, _ := trustedProxyNetworks(trustedProxies, false)
	return resolveClientIP(remote, xff, "", nets)
}

// ResolveClientIPWithXRI 在 ResolveClientIP 的基础上**额外**采信 X-Real-IP。
//
// 仅在部署形态能保证「可信代理会覆写（而非追加/透传）X-Real-IP」时使用——
// 例如 nginx 的 proxy_set_header X-Real-IP $remote_addr（覆写语义）。
// 若可信代理只是透传客户端自带的 X-Real-IP，本函数等同于把客户端 IP 的决定权
// 交给客户端：限流/锁定计数桶可被绕过，审计与访问日志的来源 IP 可被改写
// （pkg#106）。
func ResolveClientIPWithXRI(remote, xff, xri string, trustedProxies []string) string {
	nets, _ := trustedProxyNetworks(trustedProxies, false)
	return resolveClientIP(remote, xff, xri, nets)
}

// ResolveClientIPWithSet 用预解析的可信代理集合解析客户端 IP（热路径形态）。
//
// 与 ResolveClientIP 的采信规则逐字相同，差别只有一处：清单在构造期解析一次，
// 请求期不再逐条 ParseCIDR（pkg#107）。set 为 nil 时等价于「不信任任何代理头」。
func ResolveClientIPWithSet(remote, xff string, set *TrustedProxySet) string {
	return resolveClientIP(remote, xff, "", set.networks())
}

// ResolveClientIPWithSetAndXRI 是 ResolveClientIPWithSet 与 X-Real-IP 采信的组合。
//
// 与 ResolveClientIPWithXRI 同样的前置条件：可信代理必须覆写 X-Real-IP，
// 否则客户端可任意改写自己的客户端 IP（pkg#106）。
func ResolveClientIPWithSetAndXRI(remote, xff, xri string, set *TrustedProxySet) string {
	return resolveClientIP(remote, xff, xri, set.networks())
}

// resolveClientIP 是采信判定本体（清单与集合两种形态共用）。
//
// xri 为空串表示不采信 X-Real-IP。
func resolveClientIP(remote, xff, xri string, trusted []*net.IPNet) string {
	remoteIP := hostOnly(remote)

	if len(trusted) == 0 || !containsIP(trusted, remoteIP) {
		return remoteIP
	}

	if ip, ok := xffCandidate(xff, trusted); ok {
		return ip
	}

	if xri != "" {
		if ip := normalizeIP(xri); ip != "" {
			return ip
		}
	}

	return remoteIP
}

// xffCandidate 从 X-Forwarded-For 链里取客户端 IP。
//
// 返回值第二项为 false 表示「XFF 不可用」（头缺失、为空、或整条链都没有合法
// IP）——调用方据此决定是否回退 X-Real-IP。XFF 存在且给出了合法候选时，第二项
// 恒为 true：即使整条链都是可信代理，也由 XFF 决定结果（回退最左端），不再看
// X-Real-IP。
func xffCandidate(xff string, trusted []*net.IPNet) (string, bool) {
	if strings.TrimSpace(xff) == "" {
		return "", false
	}

	parts := strings.Split(xff, ",")
	candidates := make([]string, 0, len(parts))
	for _, part := range parts {
		if ip := normalizeIP(part); ip != "" {
			candidates = append(candidates, ip)
		}
	}
	if len(candidates) == 0 {
		return "", false
	}

	for i := len(candidates) - 1; i >= 0; i-- {
		if !containsIP(trusted, candidates[i]) {
			return candidates[i], true
		}
	}
	// 整条链都是可信代理（如内网互调、健康检查）：回退到最左端的原始发起方。
	return candidates[0], true
}

// normalizeIP 把一个代理头候选值归一为裸 IP：去空白、剥端口、校验合法性，
// 并转成规范文本形态（IPv6 压缩写法、v4-mapped 归一）。
//
// 非法值返回空串——调用方跳过该候选，不让它进入返回值。
func normalizeIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	ip := net.ParseIP(hostOnly(value))
	if ip == nil {
		return ""
	}
	return ip.String()
}

// hostOnly 从 "ip:port" 形态的地址取 IP 部分；解析不出端口时按原样返回
// （裸 IP 形态，含 IPv6 的 "::1" —— SplitHostPort 对其报 "too many colons"）。
func hostOnly(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// containsIP 判断 IP 是否落在可信代理网段集合内。
func containsIP(nets []*net.IPNet, ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, network := range nets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// ===== 客户端 IP 解析（net/http 适配层）=====

// GetClientIP 从 HTTP 请求中提取真实客户端 IP。
// 优先级：X-Forwarded-For（仅当请求来自可信代理时） > RemoteAddr。
//
// ⚠️ 本函数**不采信 X-Real-IP**——单跳头无法区分「代理写的」与「客户端自带的」，
// 网关默认透传客户端自带的该头，采信它等于把客户端 IP 的决定权交给客户端
// （pkg#106）。确实需要时用 GetClientIPWithXRI。
//
// 安全说明：默认只信任内网代理（10/8, 172.16/12, 192.168/16, 127/8）。
// 如果部署在公网代理后面（如 Cloudflare），需要通过 GetClientIPWithProxies 指定可信代理列表。
func GetClientIP(r *http.Request) string {
	return ResolveClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), defaultTrustedProxies)
}

// GetClientIPWithProxies 从 HTTP 请求中提取真实客户端 IP，指定可信代理清单。
// 只有当 RemoteAddr 属于可信代理时，才信任 X-Forwarded-For。
// trustedProxies 为 nil 或空时，只使用 RemoteAddr。
//
// ⚠️ 本函数**不采信 X-Real-IP**（同 GetClientIP）。
//
// 本函数是 net/http 适配层，算法本体在 ResolveClientIP（非 net/http 框架也可复用）。
func GetClientIPWithProxies(r *http.Request, trustedProxies []string) string {
	return ResolveClientIP(r.RemoteAddr, r.Header.Get("X-Forwarded-For"), trustedProxies)
}

// GetClientIPWithXRI 在 GetClientIP 的基础上额外采信 X-Real-IP。
//
// 仅在可信代理会**覆写** X-Real-IP 时使用（否则客户端可任意改写自己的客户端 IP，
// pkg#106）。
func GetClientIPWithXRI(r *http.Request) string {
	return ResolveClientIPWithXRI(
		r.RemoteAddr,
		r.Header.Get("X-Forwarded-For"),
		r.Header.Get("X-Real-IP"),
		defaultTrustedProxies,
	)
}

// GetClientIPWithProxiesAndXRI 指定可信代理清单并额外采信 X-Real-IP。
//
// 前置条件同 GetClientIPWithXRI：可信代理必须覆写 X-Real-IP。
func GetClientIPWithProxiesAndXRI(r *http.Request, trustedProxies []string) string {
	return ResolveClientIPWithXRI(
		r.RemoteAddr,
		r.Header.Get("X-Forwarded-For"),
		r.Header.Get("X-Real-IP"),
		trustedProxies,
	)
}
