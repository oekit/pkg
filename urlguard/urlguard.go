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
// IPv6 特殊用途段与 NAT64 段同样阻断：64:ff9b::/96（NAT64 well-known prefix——
// 64:ff9b::7f00:1 即 127.0.0.1 的 IPv6 写法）、64:ff9b:1::/48（NAT64 local-use）、
// 100::/64（discard-only）、2001::/32（Teredo）、2001:2::/48（benchmarking）、
// 2001:10::/28（ORCHID）、2001:20::/28（ORCHIDv2）、2001:db8::/32（文档段）、
// 2002::/16（6to4——可内嵌任意 IPv4，含私网/回环）。
//
// exotic IPv4 字面量形态（0x7f000001 / 0177.0.0.1 / 127.1 / 2130706433）一律拒绝：
// 标准库 net.ParseIP 不认它们，但宽松解析器（cgo resolver 的 inet_aton 语义、
// 浏览器 URL 解析器）会当地址——写面放行等于把判定推给拨号层，构成回环绕过路径。
//
// IPv6 zone 后缀（http://[::1%25lo]/、http://[fe80::1%25eth0]/）同样按地址部分
// 判定：net.ParseIP 不认 zone，不剥掉就等于整段跳过 IP 字面量判定——loopback、
// 链路本地、ULA、IPv4-mapped 全能以 zone 形态绕过。zone 只决定走哪块网卡，
// 不改变连的是哪个地址。
//
// 构造态豁免：WithAllowPrivate / WithAllowCIDRs / WithAllowDomains。
// **精确 host 白名单**：WithExactHosts —— "host 必须完全等于管理员配的那个 host"
// 这类判据在放行型模型下没有对应表达（后缀豁免只能放宽域及其全部子域，无法钉死
// 到具体 host），它与豁免维度独立可叠加，且命中后仍走 scheme 与私网判定。
//
// **自定义判定钩子**：WithCheckIPFunc / WithCheckHostFunc —— 放行型模型表达不了
// 消费方常见的限制型策略（白名单只放行我的网段 / 黑名单屏蔽这些网段 / 域名
// 白黑名单）。注入后消费方的策略成为**单一判定源**，且同时作用于「预检」与
// 「建连后端点复核」两处——重解析窗口（DNS rebinding / TOCTOU）由消费方自己
// 的策略闭合，不必在 urlguard 之外再叠一层自有端点判定。判定顺序：内置在前、
// 自定义在后；构造态豁免只豁免内置判定，不再跳过自定义判定。
//
// Guard 在 New 之后只读，可并发共享。仅依赖标准库（pkg 零第三方依赖红线）。
//
// # 已知边界
//
//   - 代理：DialFunc 的签名拿不到「目标 host」（http.Transport.Proxy 生效时
//     DialContext 收到的是**代理**地址），因此拨号层校验的是代理的 IP、真实目标的
//     DNS 从头到尾没被 IP 级校验过。走代理时必须先过 CheckURL（层 1），
//     且代理自身不在阻断网段内。
//   - 自定义传输：端点复核只认 TCP / UDP / IP 三种 net.Addr；其他实现
//     （自定义 conn）取不到 IP ⇒ 该次端点复核跳过。
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
	"::/96",           // IPv4-compatible（::127.0.0.1 即 127.0.0.1 的 IPv6 写法；RFC 4291 已废弃）
	"fe80::/10",       // IPv6 链路本地
	"fc00::/7",        // IPv6 ULA
	"fec0::/10",       // IPv6 site-local（RFC 3879 已废弃，企业网历史在用）
	"ff00::/8",        // IPv6 组播
	"64:ff9b::/96",    // NAT64 well-known prefix（64:ff9b::7f00:1 即 127.0.0.1 的 IPv6 写法）
	"64:ff9b:1::/48",  // NAT64 local-use prefix（RFC 8215，本机翻译前缀）
	"100::/64",        // discard-only（非全球可达）
	"2001::/32",       // Teredo（可内嵌 IPv4）
	"2001:2::/48",     // benchmarking（非全球可达）
	"2001:10::/28",    // ORCHID（非全球可达）
	"2001:20::/28",    // ORCHIDv2（非全球可达）
	"2001:db8::/32",   // 文档段（不该出现在出站目标）
	"2002::/16",       // 6to4（可内嵌任意 IPv4，含私网/回环）
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

	// 自定义判定钩子（pkg#91）：消费方自己的出站策略成为**单一判定源**。
	//
	// checkIPFunc 作用于所有 IP 判定点——拨号层预检（解析出的每个 IP）、
	// 建连后的真实端点核对（RemoteAddr）、以及 URL 层的 IP 字面量判定：
	// 消费方的策略因此在「预检」与「建连后端点复核」两处都生效，重解析窗口
	// （DNS rebinding / TOCTOU）由**同一套策略**闭合。
	//
	// checkHostFunc 作用于所有 host 判定点——CheckHost、CheckURL、以及拨号层
	// 的目标 host。
	checkIPFunc   func(net.IP) error
	checkHostFunc func(host string) error
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

// WithCheckIPFunc 注入**自定义 IP 判定**（pkg#91）：返回 nil = 放行，非 nil
// = 拒绝。
//
// 为什么需要这一档：urlguard 是**纯放行型**防护——缺省阻断私网/loopback/
// 保留段与约定域名后缀，除此之外的公网地址一律放行。它现有的三个 IP 维度
// 都无法表达消费方常见的出站策略：
//
//   - WithAllowCIDRs：语义是「豁免私网阻断」，不是「限制到这些网段」——
//     白名单外的任意公网 IP 仍默认放行；
//   - blockedCIDRs：固定私网清单，没有运行时追加拒绝项的入口；
//   - 整个包没有端口维度。
//
// 而消费方的出站防护通常是**两段式**：写面校验（请求发起前解析一次 DNS 按
// 自己的策略判定 URL）+ 建连层复核（拨号时解析、再按同一套策略判定**真实
// 连到的端点 IP**）。第 2 段不是冗余——第 1 段解析 DNS 与真正建连之间存在
// 重解析窗口（DNS rebinding / TOCTOU），攻击者可在窗口内把 A 记录切到内网
// 地址：校验层看到公网 IP、建连落到内网 IP。
//
// 注入本钩子后，消费方的策略成为**单一判定源**：拨号层预检与建连后的真实
// 端点核对用的是同一个判定（Guard.DialFunc 已有的骨架：解析 → 预检 → 建连
// → 以真实 conn.RemoteAddr() 复核），重解析窗口由消费方自己的策略闭合，
// 不必在 urlguard 之外再叠一层自有端点判定。
//
// 判定顺序：**内置判定在前、自定义判定在后**。内置的私网/保留段阻断仍是
// 兜底（fail-closed），自定义判定在其之后叠加（可白名单也可黑名单）。
//
// ⚠️ 构造态豁免的语义收敛（pkg#91）：WithAllowPrivate / WithAllowCIDRs
// 只豁免**内置**的私网/保留段阻断，**不再跳过自定义判定**——注入钩子后，
// 豁免不再意味着"整个 IP 判定被短路"。未注入钩子时行为与既有完全一致。
//
// 入参是**归一化后**的 IP：IPv4-mapped IPv6（::ffff:10.0.0.1）已归一化为
// IPv4（与内置判定同一形态），消费方无需自己处理 mapped 绕过。
//
// 钩子可能被调用多次：拨号层对解析出的**每个** IP 各调一次（预检），
// 建连成功后对真实端点再调一次（复核），URL 层的 IP 字面量判定也会调。
// 钩子应是无副作用的纯判定。
func WithCheckIPFunc(fn func(net.IP) error) Option {
	return func(g *Guard) { g.checkIPFunc = fn }
}

// WithCheckHostFunc 注入**自定义 host 判定**（pkg#91）：返回 nil = 放行，
// 非 nil = 拒绝。语义与 WithCheckIPFunc 同构，只是维度是 host 而非 IP。
//
// 为什么需要这一档：host 维度同样只有「放宽型」入口——WithAllowDomains 是
// "放宽某些域的私网/约定域阻断"，WithExactHosts 是"只允许这些 authority"，
// 没有"限制到这些域"的常规白/黑名单入口（域名后缀豁免表达不了黑名单）。
//
// 判定顺序：内置判定在前（精确白名单 → 字面量判定），自定义判定在后。
//
// 入参是**归一化后**的 host（小写、去尾点、去 IPv6 方括号），与 CheckHost
// 的入参口径一致。IP 字面量同样以字符串形式传入（本维度不区分地址与域名）。
//
// 钩子可能被调用多次：CheckHost / CheckURL / 拨号层各判定一次。钩子应是
// 无副作用的纯判定。
func WithCheckHostFunc(fn func(host string) error) Option {
	return func(g *Guard) { g.checkHostFunc = fn }
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
//
// 判定顺序（pkg#91）：**内置判定在前、自定义判定在后**——内置的私网/保留段
// 阻断是兜底（fail-closed），自定义判定（WithCheckIPFunc）在其之后叠加。
// 构造态豁免（WithAllowPrivate / WithAllowCIDRs）只豁免内置判定，不再跳过
// 自定义判定。
func (g *Guard) CheckIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("urlguard: nil ip")
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4 // ::ffff:a.b.c.d 归一化，防 mapped 绕过
	}
	// 1. 内置判定：缺省阻断网段（构造态豁免只作用在这一层）
	if !g.allowPrivate && !g.inAllowCIDRs(ip) {
		for _, n := range blockedCIDRs {
			if n.Contains(ip) {
				return fmt.Errorf("urlguard: %s is in blocked range %s", ip, n)
			}
		}
	}
	// 2. 自定义判定（消费方策略，单一判定源）
	if g.checkIPFunc != nil {
		if err := g.checkIPFunc(ip); err != nil {
			return fmt.Errorf("urlguard: ip %s rejected by policy: %w", ip, err)
		}
	}
	return nil
}

// inAllowCIDRs 判定 IP 是否落在构造态豁免网段内（WithAllowCIDRs）。
func (g *Guard) inAllowCIDRs(ip net.IP) bool {
	for _, n := range g.allowCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
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
//
// 判定顺序（pkg#91）：内置判定在前（IP 字面量 → 私网/约定域阻断），
// 自定义 host 判定（WithCheckHostFunc）在后。
func (g *Guard) checkHostLiteral(host string) error {
	if host == "" {
		return fmt.Errorf("urlguard: empty host")
	}
	if ip := net.ParseIP(stripIPv6Zone(host)); ip != nil {
		return g.CheckIP(ip)
	}
	if g.allowPrivate {
		return g.checkHostPolicy(host)
	}
	// exotic IPv4 字面量：标准库解析器不认，但宽松解析器（cgo resolver 的
	// inet_aton 语义、浏览器 URL 解析器）会当地址——字面量层直接拒，
	// 不把它推给 DNS（判据见 looksLikeExoticIPv4）。
	if looksLikeExoticIPv4(host) {
		return fmt.Errorf("urlguard: host %q is an exotic IPv4 literal form", host)
	}
	if _, ok := g.allowDomains[host]; ok {
		return g.checkHostPolicy(host)
	}
	for d := range g.allowDomains {
		if strings.HasSuffix(host, "."+d) {
			return g.checkHostPolicy(host)
		}
	}
	for _, suffix := range blockedDomainSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return fmt.Errorf("urlguard: host %q uses blocked domain %q", host, suffix)
		}
	}
	return g.checkHostPolicy(host)
}

// checkHostPolicy 是 host 维度的自定义判定（WithCheckHostFunc，pkg#91）——
// 在内置 host 判定通过之后执行。未注入钩子时恒放行。
func (g *Guard) checkHostPolicy(host string) error {
	if g.checkHostFunc == nil {
		return nil
	}
	if err := g.checkHostFunc(host); err != nil {
		return fmt.Errorf("urlguard: host %q rejected by policy: %w", host, err)
	}
	return nil
}

// ipv4PartKind 是 IPv4 字面量单段的进制种类。
type ipv4PartKind int

const (
	ipv4PartDecimal ipv4PartKind = iota // 十进制
	ipv4PartOctal                       // 八进制（前导 0）
	ipv4PartHex                         // 十六进制（前导 0x）
)

// looksLikeExoticIPv4 判定 host 是否是「宽松解析器会当成 IPv4 地址、但
// net.ParseIP 不认」的 exotic 字面量形态：
//
//	0x7f000001      十六进制 32 位        → 127.0.0.1
//	0177.0.0.1      八进制逐段            → 127.0.0.1
//	127.1           两段式（末段 24 位）  → 127.0.0.1
//	2130706433      十进制 32 位          → 127.0.0.1
//
// 写面放行不等于能连上，但它把判定推给了拨号层：拨号层同样是 net.ParseIP == nil
// ⇒ 走 DNS 解析 ⇒ 这类形态在 cgo resolver（inet_aton 语义）下会被解析成
// 127.0.0.1 ⇒ 写面放行 + 拨号层放行 = 一条回环绕过路径，而本包的定位正是
// 「SSRF 防护的单源判定」。
//
// 口径（fail-closed）：命中即拒，不解码成 IPv4 再走范围判定——cgo resolver
// 语义更宽，字面量层防护必须 fail-closed（同族 oagentkit/guardx/egress 的
// looksLikeExoticIPv4 同款）。正常点分十进制由 net.ParseIP 先判，不走这里。
func looksLikeExoticIPv4(host string) bool {
	parts := strings.Split(host, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return false // inet_aton 最多四段
	}
	nonDecimal := false
	for _, p := range parts {
		kind, ok := parseIPv4Part(p)
		if !ok {
			return false // 不是 IPv4 字面量候选（正常域名走 DNS 路径）
		}
		if kind != ipv4PartDecimal {
			nonDecimal = true
		}
	}
	// 段数 < 4 也是 inet_aton 可解析的形态（末段补齐到 16/24/32 位）；
	// 任一段带八进制/十六进制标记同样是非十进制形态。
	return nonDecimal || len(parts) < 4
}

// parseIPv4Part 判定单段是否为 inet_aton 接受的数字形态，返回其进制种类；
// 不接受时 ok=false（该 host 不是 IPv4 字面量候选）。
func parseIPv4Part(part string) (ipv4PartKind, bool) {
	if part == "" {
		return 0, false
	}
	if len(part) > 2 && (part[0] == '0') && (part[1] == 'x' || part[1] == 'X') {
		for i := 2; i < len(part); i++ {
			if !isHexDigit(part[i]) {
				return 0, false
			}
		}
		return ipv4PartHex, true
	}
	for i := 0; i < len(part); i++ {
		if part[i] < '0' || part[i] > '9' {
			return 0, false
		}
	}
	if len(part) > 1 && part[0] == '0' {
		return ipv4PartOctal, true
	}
	return ipv4PartDecimal, true
}

// isHexDigit 判定是否为十六进制数字字符。
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
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
		// host 维度的自定义判定（pkg#91）：拨号层也过一遍消费方策略——
		// 消费方可能只挂 DialFunc（不挂 CheckURL），此时 host 判定不能缺席。
		if herr := g.checkHostPolicy(canonicalHost(host)); herr != nil {
			return nil, fmt.Errorf("urlguard: dial %q blocked: %w", addr, herr)
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
		// TCP / UDP / IP 三种连接都要复核——此前只认 *net.TCPAddr，UDP 连接
		// 的端点复核被整段跳过（TOCTOU 窗口对 UDP 完全敞开）。
		if ra := conn.RemoteAddr(); ra != nil {
			if ip := addrIP(ra); ip != nil {
				if err := g.CheckIP(ip); err != nil {
					_ = conn.Close()
					return nil, fmt.Errorf("urlguard: dial %q blocked: connected endpoint %s violates guard: %v", addr, ip, err)
				}
			}
		}
		return conn, nil
	}
}

// resolve 解析 host 为全部 IP：IP 字面量直接返回（不做 DNS）；域名走 lookup
// （缺省 net.DefaultResolver）。解析失败原样返回错误。
func (g *Guard) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(stripIPv6Zone(host)); ip != nil {
		return []net.IP{ip}, nil
	}
	// exotic IPv4 字面量：标准库解析器不认 → 本会走 DNS 解析，但宽松解析器
	// （cgo resolver 的 inet_aton 语义）会把它当地址（127.1 → 127.0.0.1）。
	// 字面量层直接拒，不把它推给 DNS——拨号层与写面同源判定。
	if !g.allowPrivate && looksLikeExoticIPv4(host) {
		return nil, fmt.Errorf("urlguard: host %q is an exotic IPv4 literal form", host)
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

// canonicalHost 把 host 归一为判定口径：小写、去空白、去尾点、去 IPv6 方括号。
func canonicalHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return host
}

// stripIPv6Zone 剥掉 IPv6 字面量的 zone（"%eth0" / "%lo"）。
//
// net.ParseIP 明确不认 zone（Go 文档："does not accept zones"），而 url.Parse
// 后的 u.Hostname() 会保留它——`http://[::1%25lo]/` 的 hostname 是 `::1%lo`
// （Go 把 %25 还原成 %）。不剥掉就直接 ParseIP 会得到 nil ⇒ IP 字面量判定
// 整段跳过 ⇒ loopback / 链路本地 / ULA / IPv4-mapped 全部能以 zone 形态绕过
// （实测：[::1%lo]、[fe80::1%eth0]、[fc00::1%eth0]、[::ffff:127.0.0.1%eth0]
// 全部放行，而无 zone 的 [::1] 被正确阻断）。
//
// zone 只决定「走哪块网卡」，不改变「连的是哪个地址」——判定按地址部分做，
// zone 本身不参与阻断结论。剥掉后仍不是合法 IP 的（如普通域名）原样返回。
func stripIPv6Zone(host string) string {
	if i := strings.IndexByte(host, '%'); i >= 0 {
		return host[:i]
	}
	return host
}

// addrIP 从 net.Addr 取 IP：覆盖 TCPAddr / UDPAddr / IPAddr。
//
// 端点复核此前只认 *net.TCPAddr ⇒ UDP 连接（*net.UDPAddr）与 IP 连接
// （*net.IPAddr）的端点复核被整段跳过（实测：端点为 127.0.0.1 的 UDP 连接
// 照样放行）——TOCTOU 窗口对 UDP 完全敞开。
//
// 其他实现（自定义传输、代理连接）取不到 IP，返回 nil——调用方跳过端点复核。
// 这里刻意**不**对未知类型 fail-closed：代理链路的 RemoteAddr 是代理地址
// （本机代理常见 127.0.0.1），一拒就把所有代理用法打死。代理场景的目标
// host 判定缺口见包文档「已知边界」。
func addrIP(ra net.Addr) net.IP {
	switch a := ra.(type) {
	case *net.TCPAddr:
		return a.IP
	case *net.UDPAddr:
		return a.IP
	case *net.IPAddr:
		return a.IP
	}
	return nil
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
