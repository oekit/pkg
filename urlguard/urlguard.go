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
// （224/4、240/4、ff00::/8、192.0.0.0/24、TEST-NET 192.0.2.0/24·198.51.100.0/24·
// 203.0.113.0/24、基准测试 198.18.0.0/15）；域名 .internal / .local / .localdomain /
// localhost（含子域）。
// IPv4-mapped IPv6（::ffff:10.0.0.1）先归一化为 IPv4 再判定。
//
// 构造态豁免：WithAllowPrivate / WithAllowCIDRs / WithAllowDomains。
// **精确 host 白名单**：WithExactHosts —— "host 必须完全等于管理员配的那个 host"
// 这类判据在放行型模型下没有对应表达（后缀豁免只能放宽域及其全部子域，无法钉死
// 到具体 host），它与豁免维度独立可叠加，且命中后仍走 scheme 与私网判定。
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
	"strconv"
	"strings"
)

// DialFunc 与 httpx/net 的拨号函数同形（标准库形态），可直接互相包装。
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// blockedCIDRs 是缺省阻断网段（见包文档）。固定清单而非可变注册表——
// 需要放行某网段走 WithAllowCIDRs 显式豁免，宁可白名单也不要运行时改黑名单。
var blockedCIDRs = mustParseCIDRs(
	"0.0.0.0/8",       // "this network"（含 0.0.0.0）
	"127.0.0.0/8",     // loopback
	"10.0.0.0/8",      // RFC1918 私网
	"172.16.0.0/12",   // RFC1918 私网
	"192.168.0.0/16",  // RFC1918 私网
	"169.254.0.0/16",  // 链路本地（含云元数据 169.254.169.254）
	"100.64.0.0/10",   // CGNAT 共享地址
	"192.0.0.0/24",    // IETF 协议指定
	"192.0.2.0/24",    // TEST-NET-1（文档保留）
	"198.18.0.0/15",   // 网络基准测试保留
	"198.51.100.0/24", // TEST-NET-2（文档保留）
	"203.0.113.0/24",  // TEST-NET-3（文档保留）
	"224.0.0.0/4",     // IPv4 组播
	"240.0.0.0/4",     // IPv4 保留/广播
	"::1/128",         // IPv6 loopback
	"::/128",          // IPv6 未指定
	"fe80::/10",       // IPv6 链路本地
	"fc00::/7",        // IPv6 ULA
	"ff00::/8",        // IPv6 组播
)

// blockedDomainSuffixes 是缺省阻断域名后缀（小写；host 等于后缀或以其为点分后缀均命中）。
var blockedDomainSuffixes = []string{"localhost", "internal", "local", "localdomain"}

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

	// 精确 host 白名单（WithExactHosts）：与 allowDomains 是不同维度——
	// allowDomains 是"放宽某些域的私网/约定域阻断"，exactHosts 是"只允许这些
	// host"。exactEnabled 区分"未启用该维度"与"启用了但清单为空"（后者 fail-closed）。
	exactEnabled  bool
	rawExactHosts []string // New 里解析，文法非法 fail-fast
	exactHosts    map[exactHost]struct{}
}

// exactHost 是精确白名单条目：host（小写、去尾点、去 IPv6 方括号）+ 端口
// （portNoPort = -1 表示该条目未指定端口）。
type exactHost struct {
	host string
	port int
}

// portNoPort 表示"未指定端口"，与端口 0 区分。
const portNoPort = -1

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

// WithExactHosts 精确 host 白名单：出站目标的 authority（host[:port]）必须
// **完全等于**列表中某项，否则一律拒绝。与 WithAllowDomains（后缀豁免）
// 语义独立、可叠加——两者是不同维度：WithAllowDomains 定"哪些域放宽私网等
// 阻断"，WithExactHosts 定"只允许哪些 host"。
//
// 为什么需要这一档：WithAllowDomains 只能"放宽"某个域及其全部子域，无法
// "钉死"到具体 host。而放行型模型下（默认只阻断私网/loopback/保留段/
// 约定域名），白名单外的任意公网域默认放行——"host 必须精确等于管理员配的
// 那个 URL 的 host"（webhook 出口签名绑 host、内部平台 API 出口多一个子域
// 就多一个未审计入口、凭据与端点绑定）在既有配置里根本没有对应表达。
//
// 判定位置：CheckURL 的 scheme 白名单之后、私网判定之前。命中白名单**不等于**
// 放行私网——钉死的 host 仍要满足 scheme 白名单与私网/保留段判定。
//
// 比较口径：
//   - host 大小写不敏感、尾点不敏感（DNS 语义）；
//   - 端口按数值比较且**参与比较**——配置不带端口时 URL 必须也不带端口
//     （"https://h:443" 与 "https://h" 不是同一个字面量，按字面判不命中；
//     宁可严，不替调用方猜默认端口）；
//   - userinfo 不参与比较：url.Parse 已把 "https://h@evil.com/" 的 userinfo
//     拆开，authority 是 "evil.com"——这正是"钉死 host"要挡的逃逸形态。
//
// 条目文法：host[:port]（IPv6 字面量用方括号形态 "[::1]:8080"）。空串、含
// 路径/空白/用户信息、端口非法或越界时 New 报错（fail-fast，不带病运行）。
//
// 未设置本选项时行为与既有完全一致；设置了但清单为空 = 一个 host 都不许
// （fail-closed）。
//
// 注意：白名单只在 URL 层判定。DialFunc 做的是 IP 级判定（DNS rebinding /
// TOCTOU 闭合），不参与白名单比对——拨号层的 addr 是实际拨号地址（默认端口
// 已补齐），按字面比对会误杀正常配置。
func WithExactHosts(hosts ...string) Option {
	return func(g *Guard) {
		g.exactEnabled = true
		g.rawExactHosts = append(g.rawExactHosts, hosts...)
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
	if g.exactEnabled {
		g.exactHosts = make(map[exactHost]struct{}, len(g.rawExactHosts))
		for _, raw := range g.rawExactHosts {
			eh, err := parseExactHost(raw)
			if err != nil {
				return nil, fmt.Errorf("urlguard: invalid exact host %q: %w", raw, err)
			}
			g.exactHosts[eh] = struct{}{}
		}
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
//
// 配了 WithExactHosts 时：host 必须命中白名单中**不带端口**的条目——
// CheckHost 的入参没有端口，端口级条目无法被它确认（否则 CheckURL 已按
// authority 命中端口级条目的 URL 会被这里误杀）。未命中一律拒绝。
func (g *Guard) CheckHost(host string) error {
	host = canonicalHost(host)
	if host == "" {
		return fmt.Errorf("urlguard: empty host")
	}
	if g.exactEnabled {
		if err := g.checkExact(host, ""); err != nil {
			return err
		}
	}
	return g.checkHostLiteral(host)
}

// checkHostLiteral 是 CheckHost 的字面量判定部分（不含精确白名单维度），
// 供 CheckURL 复用——CheckURL 已在 authority 级完成白名单判定。
func (g *Guard) checkHostLiteral(host string) error {
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
//
// 配了 WithExactHosts 时，URL 的 authority（host[:port]）必须完全命中白名单，
// 否则在 scheme 校验之后、私网判定之前拒绝。
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
	if g.exactEnabled {
		if err := g.checkExact(host, u.Port()); err != nil {
			return err
		}
	}
	return g.checkHostLiteral(canonicalHost(host))
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

// parseExactHost 解析精确白名单条目：host[:port]（IPv6 字面量用方括号形态
// "[::1]:8080"）。文法非法时报错（fail-fast）。
func parseExactHost(raw string) (exactHost, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return exactHost{}, fmt.Errorf("empty host")
	}
	if strings.ContainsAny(s, "/@ \t\r\n") {
		return exactHost{}, fmt.Errorf("must be host[:port] without path, userinfo or whitespace")
	}
	host, portStr, err := splitAuthority(s)
	if err != nil {
		return exactHost{}, err
	}
	if host == "" {
		return exactHost{}, fmt.Errorf("empty host")
	}
	canon := canonicalHost(host)
	if canon == "" {
		return exactHost{}, fmt.Errorf("empty host")
	}
	if strings.Contains(canon, ":") && net.ParseIP(canon) == nil {
		return exactHost{}, fmt.Errorf("invalid host %q", host)
	}
	p, err := parsePort(portStr)
	if err != nil {
		return exactHost{}, err
	}
	return exactHost{host: canon, port: p}, nil
}

// splitAuthority 拆 host[:port]（IPv6 字面量用方括号形态 "[::1]:8080"）。
// 文法非法时返回错误（fail-fast）——不把拆不开的串当 host 静默放过。
func splitAuthority(s string) (host, port string, err error) {
	if ip := net.ParseIP(s); ip != nil {
		return s, "", nil // IP 字面量（IPv4 / 裸 IPv6），无端口
	}
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return "", "", fmt.Errorf("invalid authority %q", s)
		}
		rest := s[end+1:]
		switch {
		case rest == "":
			return s[:end+1], "", nil
		case strings.HasPrefix(rest, ":"):
			p := rest[1:]
			if p == "" {
				return "", "", fmt.Errorf("empty port")
			}
			return s[:end+1], p, nil
		default:
			return "", "", fmt.Errorf("invalid authority %q", s)
		}
	}
	if !strings.Contains(s, ":") {
		return s, "", nil
	}
	h, p, serr := net.SplitHostPort(s)
	if serr != nil {
		return "", "", fmt.Errorf("invalid authority %q: %w", s, serr)
	}
	if p == "" {
		return "", "", fmt.Errorf("empty port")
	}
	return h, p, nil
}

// parsePort 解析端口：空串 = 未指定端口（portNoPort）；否则必须是 0-65535
// 的十进制数。
func parsePort(p string) (int, error) {
	if p == "" {
		return portNoPort, nil
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return 0, fmt.Errorf("invalid port %q", p)
	}
	if n < 0 || n > 65535 {
		return 0, fmt.Errorf("port %d out of range", n)
	}
	return n, nil
}

// checkExact 判定 authority（host + 端口）是否命中精确白名单。
func (g *Guard) checkExact(host, port string) error {
	canon := canonicalHost(host)
	if canon == "" {
		return fmt.Errorf("urlguard: empty host")
	}
	p, err := parsePort(port)
	if err != nil {
		return fmt.Errorf("urlguard: %w", err)
	}
	if _, ok := g.exactHosts[exactHost{host: canon, port: p}]; !ok {
		return fmt.Errorf("urlguard: host %q is not in the exact host allowlist", authorityString(canon, p))
	}
	return nil
}

// authorityString 把 host + 端口还原成可读的 authority 形态（错误信息用）。
func authorityString(host string, port int) string {
	if port == portNoPort {
		return host
	}
	if strings.Contains(host, ":") { // IPv6 字面量
		return "[" + host + "]:" + strconv.Itoa(port)
	}
	return host + ":" + strconv.Itoa(port)
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
