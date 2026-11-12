package urlguard

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
)

// 内置拒绝类别的哨兵（#380 拨号面 / #390 写面）：SSRF 防护原语的调用方需要
// 用 errors.Is 区分「防护拦截」与网络故障——429/500 分流、指标与告警路由都
// 依赖这个判定。此前内置拒绝全是匿名错误，调用方只能解析文案。六类哨兵覆盖
// 写面（CheckURL/CheckHost/CheckIP）与拨号面（DialFunc）的全部内置拒绝类别；
// 错误链全程 %w 透传，链不断、errors.Is 跨包装可判。分类总表见包文档。
var (
	// ErrBlockedRange 目标地址落在缺省阻断网段（私网/loopback/链路本地/
	// 保留段等，清单见 BlockedCIDRSpecs）。构造态豁免（WithAllowPrivate /
	// WithAllowCIDRs / WithAllowDomains）命中时本哨兵不出现。
	ErrBlockedRange = errors.New("urlguard: address in blocked range")

	// ErrBlockedDomain 目标 host 命中阻断域名后缀（.internal/.local/
	// .localdomain/localhost 族）。WithAllowDomains 命中时不出现。
	ErrBlockedDomain = errors.New("urlguard: blocked domain suffix")

	// ErrExoticLiteral 目标 host 是「宽松解析器会当 IPv4 地址、net.ParseIP
	// 不认」的 exotic 字面量形态（0x7f000001 / 0177.0.0.1 / 127.1 /
	// 2130706433 等，含含空段的 fail-closed 形态）。本闸门无条件执行，
	// 不随构造态豁免放开（pkg#236）。
	ErrExoticLiteral = errors.New("urlguard: exotic IPv4 literal")

	// ErrSchemeNotAllowed 目标 URL 的 scheme 不在白名单（缺省 http/https，
	// 可用 WithAllowedSchemes 调整）——「配置配错」类：消费方的出站配置
	// 误写了 ftp:// 之类，与 SSRF 尝试（ErrBlockedRange 等）分流到不同
	// 告警面（#390）。
	ErrSchemeNotAllowed = errors.New("urlguard: scheme not allowed")

	// ErrNonASCIIHost 目标 host 含无法确定性折叠的非 ASCII 码点（IDN/圈号
	// 等，pkg#266 的 fail-closed 口径）——调用方应先自行做 IDNA（punycode）
	// 转换再传入；这是输入形态问题，不是 SSRF 尝试（#390）。
	ErrNonASCIIHost = errors.New("urlguard: host contains non-ASCII characters that cannot be deterministically folded (IDN/全角形式 fail-closed)")

	// ErrNotInExactHosts 目标 authority 未命中 WithExactHosts 精确白名单
	// ——「配置配错」类（白名单清单维护遗漏/环境差异），与防护拦截分流
	// （#390）。
	ErrNotInExactHosts = errors.New("urlguard: authority is not in the exact host allowlist")
)

// blockedCIDRSpecs 是缺省阻断网段的**字符串清单形态**——IANA 特殊用途地址
// 段知识的跨仓单源（core#132：core/network 的 ipv6SpecialCIDRs 与 IPv4 硬编码
// switch 下波改引本清单，不再双仓手工对账）。注释逐条给出来源与理由；新登记
// 条目只改这里，pkg 与 core 两个消费面同源生效。
//
// 固定清单而非可变注册表——需要放行某网段走 WithAllowCIDRs 显式豁免，
// 宁可白名单也不要运行时改黑名单。
var blockedCIDRSpecs = []string{
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
	"3fff::/20",       // 文档段（RFC 9637，IPv6 侧 TEST-NET；IANA 2024 新登记，#297）
	"5f00::/16",       // SRv6 SID（RFC 9602，非全球可达；IANA 2024 新登记，#297）
	"100:0:0:1::/64",  // dummy prefix（RFC 9780；IANA 2025 新登记，#297）
}

// blockedCIDRs 是解析后的缺省阻断网段（判定用）。
var blockedCIDRs = mustParseCIDRs(blockedCIDRSpecs...)

// BlockedCIDRSpecs 返回缺省阻断网段清单的字符串形态（防御性拷贝）。
//
// 这是 IANA 特殊用途地址段知识的**跨仓单源**（core#132）：同一份清单此前在
// 本包与 oekit/core/network 各持一份、靠注释互相提醒手工对账——#297 那次
// 加段就是三处联动。core/network 下波改引本函数（或 ParseCIDRList/IsBlockedIP）
// 后，加段只改这一处。清单语义见包文档「默认阻断范围」。
func BlockedCIDRSpecs() []string {
	return slices.Clone(blockedCIDRSpecs)
}

// IsBlockedIP 报告 ip 是否落在缺省阻断网段内（core#132 的薄判定 API）：
// 纯清单判定，不含任何豁免语义（WithAllowPrivate 等构造态豁免是 Guard
// 实例的维度，与本函数无关）。IPv4-mapped IPv6（::ffff:10.0.0.1）先归一化
// 为 IPv4 再判定，与 Guard.CheckIP 同口径；ip 为 nil 返回 false（nil 不是
// 地址，判定无意义——Guard.CheckIP 对 nil 显式报错，这里只做清单查询）。
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ParseCIDRList 解析 CIDR 字符串清单为 *net.IPNet（core#132 的薄解析 API）：
// 任一网段文法非法即报错（fail-fast，指明是哪一条）。消费方（如 core/network）
// 用 BlockedCIDRSpecs() 取清单、经本函数解析成自己的判定结构，不再各自
// 手写 parse 循环。
func ParseCIDRList(specs ...string) ([]*net.IPNet, error) {
	out := make([]*net.IPNet, 0, len(specs))
	for _, spec := range specs {
		_, n, err := net.ParseCIDR(strings.TrimSpace(spec))
		if err != nil {
			return nil, fmt.Errorf("urlguard: invalid CIDR %q: %w", spec, err)
		}
		out = append(out, n)
	}
	return out, nil
}
