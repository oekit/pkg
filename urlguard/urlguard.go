// Package urlguard 提供出站 URL 的 SSRF（Server-Side Request Forgery）防护原语，
// 供「出站目标是用户自配 URL」的服务（webhook 接收器：钉钉/飞书/企微机器人、
// 告警接入通道等）复用（[oekit/httpx#11](https://github.com/oekit/httpx/issues/11)）。
//
// 两层防护缺一不可：
//
// 层 1 CheckURL——URL 字面量校验：host 直接写私网/loopback IP、
// .internal/.local/localhost 约定域名、非 http(s) scheme 一律拒绝，挡住低成本探测。
//
// 层 2 DialFunc——拨号层包装：host 解析出的**每一个** IP 都过 CheckIP（DNS rebinding /
// 劫持场景，URL 层对它无能为力），全部放行才调底层拨号；建连成功后再核对实际
// RemoteAddr——预检与真实端点之间的重解析窗口（TOCTOU）被端点核对闭合，
// Happy-Eyeballs 双栈竞争的胜出路径同样被覆盖。
//
// 默认阻断范围：未指定地址（0.0.0.0/8、::）、loopback（127.0.0.0/8、::1）、
// RFC1918 私网（10/8、172.16/12、192.168/16）、链路本地（169.254/16——含云元数据端点
// 169.254.169.254、fe80::/10）、CGNAT（100.64/10）、ULA（fc00::/7）、组播/保留
// （224/4、240/4、ff00::/8）；域名 .internal / .local / localhost（含子域）。
// IPv4-mapped IPv6（::ffff:10.0.0.1）先归一化为 IPv4 再判定。
//
// Guard 在 New 之后只读，可并发共享。仅依赖标准库（pkg 零第三方依赖红线）。
//
// 用法（webhook 出口一行接入，[httpx](https://github.com/oekit/httpx) 的
// WithDialFunc 直接组合）：
//
//	g, err := urlguard.New()                       // 缺省全阻断；测试环境
//	// g, err := urlguard.New(urlguard.WithAllowPrivate()) // 打本机 mock server 用
//	if err != nil { ... }
//	if err := g.CheckURL(webhookURL); err != nil { ... }   // 层 1：入参校验
//	hc := &http.Client{Transport: &http.Transport{ DialContext: urlguard.SafeDialFunc(nil) }}
//	// 或与 httpx 组合：httpx.WithDialFunc(g.DialFunc(nil))
package urlguard

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// DialFunc 与 httpx/net 的拨号函数同形（标准库形态），可直接互相包装。
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// blockedCIDRs 是缺省阻断网段（见包文档）。固定清单而非可变注册表——
// 需要放行某网段走 WithAllowCIDRs 显式豁免，宁可白名单也不要运行时改黑名单。
var blockedCIDRs = mustParseCIDRs(
	"0.0.0.0/8",      // "this network"（含 0.0.0.0）
	"127.0.0.0/8",    // loopback
	"10.0.0.0/8",     // RFC1918 私网
	"172.16.0.0/12",  // RFC1918 私网
	"192.168.0.0/16", // RFC1918 私网
	"169.254.0.0/16", // 链路本地（含云元数据 169.254.169.254）
	"100.64.0.0/10",  // CGNAT 共享地址
	"224.0.0.0/4",    // IPv4 组播
	"240.0.0.0/4",    // IPv4 保留/广播
	"::1/128",        // IPv6 loopback
	"::/128",         // IPv6 未指定
	"fe80::/10",      // IPv6 链路本地
	"fc00::/7",       // IPv6 ULA
	"ff00::/8",       // IPv6 组播
)

// blockedDomainSuffixes 是缺省阻断域名后缀（小写；host 等于后缀或以其为点分后缀均命中）。
var blockedDomainSuffixes = []string{"localhost", "internal", "local"}

// defaultSchemes 是 CheckURL 缺省允许的 scheme。出站 webhook 场景下 file://、
// gopher:// 等一律拒绝；需要其它 scheme 用 WithAllowedSchemes 显式放行。
var defaultSchemes = []string{"http", "https"}

// Guard 是 SSRF 防护器：New 之后只读，可并发共享。
type Guard struct {
	allowPrivate  bool
	rawAllowCIDRs []string // New 里解析，文法非法 fail-fast
	allowCIDRs    []*net.IPNet
	allowDomains  map[string]struct{} // 小写；命中语义 = 等于或点分后缀
	schemes       map[string]struct{} // 小写
	lookup        func(ctx context.Context, host string) ([]net.IP, error)
}

// Option 配置 Guard。
type Option func(*Guard)

// WithAllowPrivate 放行全部内网/loopback 地址——仅供测试环境打本机 mock server；
// 生产构造不要使用。
func WithAllowPrivate() Option {
	return func(g *Guard) { g.allowPrivate = true }
}

// WithAllowCIDRs 按网段显式豁免（如内网 webhook 端点 "10.20.0.0/16"）。
// 网段文法非法时 New 报错。
func WithAllowCIDRs(cidrs ...string) Option {
	return func(g *Guard) { g.rawAllowCIDRs = append(g.rawAllowCIDRs, cidrs...) }
}

// WithAllowDomains 按域名豁免（小写化后保存；命中语义 = 等于或点分后缀，
// "example.com" 同时放行 "api.example.com"；不支持通配符前缀，写根域即可）。
func WithAllowDomains(domains ...string) Option {
	return func(g *Guard) {
		if g.allowDomains == nil {
			g.allowDomains = map[string]struct{}{}
		}
		for _, d := range domains {
			d = canonicalHost(d)
			if d != "" {
				g.allowDomains[d] = struct{}{}
			}
		}
	}
}

// WithAllowedSchemes 覆盖 CheckURL 的 scheme 白名单（缺省 http/https）。
func WithAllowedSchemes(schemes ...string) Option {
	return func(g *Guard) {
		if g.schemes == nil {
			g.schemes = map[string]struct{}{}
		}
		for _, s := range schemes {
			if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
				g.schemes[s] = struct{}{}
			}
		}
	}
}

// New 构造 Guard。CIDR 文法非法时报错（fail-fast，不带病运行）。
func New(opts ...Option) (*Guard, error) {
	g := &Guard{
		schemes: map[string]struct{}{},
		lookup:  defaultLookup,
	}
	for _, s := range defaultSchemes {
		g.schemes[s] = struct{}{}
	}
	for _, opt := range opts {
		if opt != nil {
			opt(g)
		}
	}
	for _, raw := range g.rawAllowCIDRs {
		_, n, err := net.ParseCIDR(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("urlguard: invalid CIDR %q: %w", raw, err)
		}
		g.allowCIDRs = append(g.allowCIDRs, n)
	}
	return g, nil
}

// IsBlockedURL 用缺省 Guard 做层 1 字面量校验（等价 New().CheckURL）。
// nil 返回 = 放行。
func IsBlockedURL(raw string) error {
	g, err := New()
	if err != nil {
		return err
	}
	return g.CheckURL(raw)
}

// SafeDialFunc 用缺省 Guard 包装拨号函数（等价 New().DialFunc(next)）。
// next 为 nil 时用 net 的默认拨号。
func SafeDialFunc(next DialFunc) DialFunc {
	g, _ := New()
	return g.DialFunc(next)
}

// CheckIP 判定单个 IP 是否可放行（nil 返回 = 放行）。
// IPv4-mapped IPv6 先归一化为 IPv4 再判定。
func (g *Guard) CheckIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("urlguard: nil ip")
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4 // ::ffff:a.b.c.d 归一化，防 mapped 绕过
	}
	if g.allowPrivate {
		return nil
	}
	for _, n := range g.allowCIDRs {
		if n.Contains(ip) {
			return nil
		}
	}
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return fmt.Errorf("urlguard: %s is in blocked range %s", ip, n)
		}
	}
	return nil
}

// CheckHost 判定主机名/IP 字面量是否可放行（nil 返回 = 放行）。
// host 不带端口；大小写与尾点不敏感。
func (g *Guard) CheckHost(host string) error {
	host = canonicalHost(host)
	if host == "" {
		return fmt.Errorf("urlguard: empty host")
	}
	if ip := net.ParseIP(host); ip != nil {
		return g.CheckIP(ip)
	}
	if g.allowPrivate {
		return nil
	}
	if _, ok := g.allowDomains[host]; ok {
		return nil
	}
	for d := range g.allowDomains {
		if strings.HasSuffix(host, "."+d) {
			return nil
		}
	}
	for _, suffix := range blockedDomainSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return fmt.Errorf("urlguard: host %q uses blocked domain %q", host, suffix)
		}
	}
	return nil
}

// CheckURL 做层 1 字面量校验（nil 返回 = 放行）：scheme 必须在白名单内，
// host 走 CheckHost。仅校验字面量——DNS rebinding 场景必须配合 DialFunc（层 2）。
func (g *Guard) CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("urlguard: parse url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if _, ok := g.schemes[scheme]; !ok {
		return fmt.Errorf("urlguard: scheme %q not allowed", scheme)
	}
	host := u.Hostname() // 去端口；IPv6 字面量为去括号形式
	return g.CheckHost(host)
}

// DialFunc 返回层 2 拨号包装：解析出的每个 IP 先过预检（任一命中阻断即不建连，
// fail-fast 且错误可读），建连成功后再核对实际 RemoteAddr——两道校验之间的
// 重解析窗口被端点核对闭合。next 为 nil 时用 net 默认拨号。
func (g *Guard) DialFunc(next DialFunc) DialFunc {
	if next == nil {
		var d net.Dialer
		next = d.DialContext
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("urlguard: split %q: %w", addr, err)
		}
		ips, err := g.resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if cerr := g.CheckIP(ip); cerr != nil {
				return nil, fmt.Errorf("urlguard: dial %q blocked: %v", addr, cerr)
			}
		}
		conn, err := next(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		// 端点核对（authoritative）：预检解析与拨号器实际解析之间可能被重解析，
		// 以真实 RemoteAddr 为准；不合规立即关闭连接。
		if ra := conn.RemoteAddr(); ra != nil {
			if tcp, ok := ra.(*net.TCPAddr); ok {
				if err := g.CheckIP(tcp.IP); err != nil {
					_ = conn.Close()
					return nil, fmt.Errorf("urlguard: dial %q blocked: connected endpoint %s violates guard: %v", addr, tcp.IP, err)
				}
			}
		}
		return conn, nil
	}
}

// resolve 解析 host 为全部 IP：IP 字面量直接返回（不做 DNS）；域名走 lookup
// （缺省 net.DefaultResolver）。解析失败原样返回错误。
func (g *Guard) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	addrs, err := g.lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("urlguard: resolve %q: %w", host, err)
	}
	return addrs, nil
}

func defaultLookup(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	return ips, nil
}

// canonicalHost 归一化 host：小写、去尾点、去 IPv6 方括号。
func canonicalHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return host
}

func mustParseCIDRs(specs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(specs))
	for _, spec := range specs {
		_, n, err := net.ParseCIDR(strings.TrimSpace(spec))
		if err != nil {
			panic(fmt.Sprintf("urlguard: invalid CIDR %q: %v", spec, err)) // 仅用于包内固定清单
		}
		out = append(out, n)
	}
	return out
}
