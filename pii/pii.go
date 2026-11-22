// Package pii PII 价值模式检测与打码（批次二百七十八，对标 octop-harness
//
// 生命周期: Stable（v0.33.0 定级）——只允许 additive 变更（新增导出符号 /
// 新增方法且给零值默认实现 / 新增错误值）；改签名、改语义、改默认值、
// 删导出符号都算 breaking，判定规则与变更流程见 docs/api-compat.md，
// 逐包理由见 api-lifecycle.yaml。
//
// PIIMiddleware 的价值面检测）：在 logredact（oagentkit/guardx/logredact，凭据类
// 值模式：key=xxx/Authorization/JWT/sk- 前缀等）之外补「个人信息价值面」——大陆手机号
// 与 18 位居民身份证。凭据脱敏按形态命中，个人信息没有键名可依（批266「logredact
// 键名盲区」教训的價值面延伸：审计链永久留存+SIEM 外发，工单摘要外发 Jira，
// 值本身即敏感，与键名无关——键位上出现的同类值走同一条判定面，见下）。
//
// 保守检测纪律（octop 同款）：宁可漏打不可错打。身份证须过四重校验——GB/T 2260
// 省级码白名单 + 出生日期合法（含未来拒绝）+ 顺序码非全零 + MOD 11-2 校验码——
// 订单号/流水号/银行卡号等长数字串几乎不可能同时通过；手机号要求两侧不邻接
// 字母数字（Go RE2 无环视断言，边界在代码判定），13 位毫秒时间戳因尾邻数字不
// 会误命中。匹配前先做数字形态归一化（全角 → 半角、去零宽、折叠数字间的分组
// 分隔符；口径与已知边界见 normalize.go），归一化与处置共用一条路径。缺省策略
// mask 保长度（前段+星号+尾段），日志对齐与残留定位友好；其余策略的处置形态由
// 宿主注入的 StrategyKind 决定（见 policy.go）。
//
// 结构面：ApplyValue（按敏感面）递归下探 map[string]any /
// map[string]string / []any / []string——其中 map[string]string 是
// oagentkit/guardx/logredact.RedactValue 的已知结构盲区（alert_svc redactMap
// 绕行注释在案），本包一并收口。**键与值走同一条判定面**：以手机号为键的载荷（手机号 → 订单/
// 客户资料，中文业务系统的一等形态）不再整键明文外泄，键命中与值命中同样产出
// span 级处置与 blocked 信号；普通键逐字不变；不同原键处置出同一形态时按原键
// 字典序加 "#n" 消解——不丢条目，也不依赖 map 的随机迭代序。nil 容器原样保留：
// JSON 的 null 与空容器在审计链上是两种形状，处置过程无权把它们合并成一种。
//
// 本包是零内部依赖的叶子层（architecture policy 模块 id=pii，requires=[]，
// 错误走 oekit/pkg/errors 单 import）：出口面（审计链/事件双出口/工单与告警外发文案）
// 无条件调用本包，并按各自敏感面走 Apply / ApplyValue；开关收敛在 Enabled 与面开关
// （缺省全开）——run.go 从 conf security.pii_value_mask 注入全局开关（缺省开；
// 显式关闭时启动 WARN 显性化）。
package pii

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Kind PII 类型（span 标注与打码策略分派用）。
type Kind string

const (
	KindMobile     Kind = "cn_mobile"      // 大陆手机号（可带 +86/0086 前缀）
	KindResidentID Kind = "cn_resident_id" // 18 位居民身份证（末位 X 大小写均可）
)

// Span 一段命中（s 的字节偏移，[Start,End)）。
type Span struct {
	Kind  Kind
	Start int
	End   int
}

// 手机号：可选 +86/0086 前缀 + 1[3-9] 开头 11 位。裸 86 前缀不认（与数字串
// 无法区分，保守放弃）。
var mobileRe = regexp.MustCompile(`(?:\+86|0086)?1[3-9][0-9]{9}`)

// 身份证粗筛：17 位数字 + 校验位（数字或 X/x）。语义校验在 validResidentID。
var residentIDRe = regexp.MustCompile(`[0-9]{17}[0-9Xx]`)

// gb2260Provinces GB/T 2260 省级码（含 71 台湾、81/82 港澳）。
var gb2260Provinces = map[string]bool{
	"11": true, "12": true, "13": true, "14": true, "15": true,
	"21": true, "22": true, "23": true,
	"31": true, "32": true, "33": true, "34": true, "35": true, "36": true, "37": true,
	"41": true, "42": true, "43": true, "44": true, "45": true, "46": true,
	"50": true, "51": true, "52": true, "53": true, "54": true,
	"61": true, "62": true, "63": true, "64": true, "65": true,
	"71": true, "81": true, "82": true,
}

// idWeights MOD 11-2 加权因子与校验码表（GB 11643-1999）。
var idWeights = [17]int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}

const idCheckChars = "10X98765432"

// Detect 找出 s 中全部 PII span（已完成语义校验与边界判定，重叠时保留更长者）。
// 返回按 Start 升序、互不重叠，偏移是**原串字节偏移**。
//
// 匹配前先做数字形态归一化（全角 → 半角、去零宽、折叠数字间的分组分隔符，
// 口径见 normalize.go），再把命中映射回原串。归一化与处置共用同一条路径：
// Apply 打码、StrategyHash 摘要都按归一化后的值计算，同一值的不同书写形态
// 因此得到同一处置形态与同一摘要。
func Detect(s string) []Span {
	if len(s) < 11 {
		return nil
	}
	norm, origIdx := normalizeForDetect(s)
	if len(norm) < 11 || !strings.ContainsFunc(norm, func(r rune) bool { return r >= '0' && r <= '9' }) {
		return nil
	}
	// 归一化文本偏移 → 原串偏移（恒等映射时直接返回下标）。
	at := func(i int) int {
		if origIdx == nil {
			return i
		}
		return origIdx[i]
	}
	var out []Span
	for _, m := range mobileRe.FindAllStringIndex(norm, -1) {
		if !boundaryOK(norm, m[0], m[1]) {
			continue
		}
		out = append(out, Span{Kind: KindMobile, Start: at(m[0]), End: at(m[1])})
	}
	for _, m := range residentIDRe.FindAllStringIndex(norm, -1) {
		if !boundaryOK(norm, m[0], m[1]) {
			continue
		}
		if !validResidentID(norm[m[0]:m[1]]) {
			continue
		}
		out = append(out, Span{Kind: KindResidentID, Start: at(m[0]), End: at(m[1])})
	}
	return dedupeSpans(out)
}

// boundaryOK 命中两侧不得邻接字母数字（RE2 无环视，代码判定；下划线/连字符等
// 符号边界放行——「tel:13812345678」「工单-11010519491231002X」应打码）。
func boundaryOK(s string, start, end int) bool {
	if start > 0 && isWordByte(s[start-1]) {
		return false
	}
	return end >= len(s) || !isWordByte(s[end])
}

func isWordByte(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// validResidentID 四重校验（保守面）：省码白名单 + 出生日期合法且不在未来 +
// 顺序码非全零 + MOD 11-2 校验码。15 位旧证不检（已停用 20 年，纯 15 位数字
// 误伤面大，octop 同取舍）。
func validResidentID(s string) bool {
	if len(s) != 18 {
		return false
	}
	if !gb2260Provinces[s[:2]] {
		return false
	}
	birth, err := time.Parse("20060102", s[6:14])
	if err != nil || birth.Year() < 1900 || birth.After(time.Now()) {
		return false
	}
	if s[14:17] == "000" {
		return false
	}
	sum := 0
	for i := range 17 {
		d, err := strconv.Atoi(s[i : i+1])
		if err != nil {
			return false
		}
		sum += d * idWeights[i]
	}
	return idCheckChars[sum%11] == strings.ToUpper(s[17:])[0]
}

// dedupeSpans 重叠消解：起点升序、同起点长者优先，贪心保留不重叠集——身份证
// （18 位）吞掉其内部可能的手机号子串命中。
func dedupeSpans(in []Span) []Span {
	if len(in) < 2 {
		return in
	}
	slices.SortFunc(in, func(a, b Span) int {
		if c := cmp.Compare(a.Start, b.Start); c != 0 {
			return c
		}
		return cmp.Compare(b.End-b.Start, a.End-a.Start)
	})
	var out []Span
	lastEnd := -1
	for _, sp := range in {
		if sp.Start >= lastEnd {
			out = append(out, sp)
			lastEnd = sp.End
		}
	}
	return out
}

// maskSpan 保长打码：手机号留前 3 尾 4（+86/0086 前缀保留——归属信息不敏感），
// 身份证留前 4（省市级可见；生日、顺序码、MOD 11-2 校验位全掩）。
//
// 入参先归一化（全角 → 半角、折叠分组分隔符），故同一号码的 `138 1234 5678` /
// `１３８…` / 零宽插入写法都产出同一个 `138****5678`。ASCII 连续形态逐字节保长；
// 带分隔符/全角的形态收敛到归一化后的长度——保留原分隔符只会把已脱敏的
// 号码重新拼回可读形态，没有收益。
//
// 校验位**不保留**（issue #298）：MOD 11-2 校验位原样留下，就成了对
// 「生日 × 顺序码 × 区县」候选的免费验算器——原本候选还需自洽才算命中。
// 留前 4 已够宿主做省市级定位，尾 2 没有等价的定位价值。
func maskSpan(v string, kind Kind) string {
	v = normalizeDigits(v)
	switch kind {
	case KindMobile:
		digits := v
		prefix := ""
		if len(v) > 11 {
			prefix, digits = v[:len(v)-11], v[len(v)-11:]
		}
		return prefix + digits[:3] + "****" + digits[7:]
	case KindResidentID:
		return v[:4] + strings.Repeat("*", 14)
	}
	return strings.Repeat("*", len(v))
}
