package pathx

import (
	"fmt"
	"strings"
)

// 本文件提供 URL path 低基数模板化原语（ID 段折叠）。
//
// 定位：按 path 切 metrics 标签（host/method/path 维度）是标准需求，但 path
// 里的 ID 段（issue 号 / commit SHA / node id / 数字主键）直接进标签会基数
// 爆炸——每个对象一条时间序列，metrics 系统被打爆。每个做 HTTP metrics 的
// 项目都在各自手搓「ID 段折叠」，启发式口径互不相同；本原语把「段分类 +
// 折叠 + 保留清单」收成一处（下游参考形态：go-git-platform
// transport/metrics.ClassifyPath——七平台 git SDK 的 metrics 现场）。
//
// 与本包既有原语的关系：MatchesPath / SanitizeRel / IsSafeFileName 问的是
// 「路径匹配吗 / 合法吗」（判定与清洗，面向文件系统），Classify 问的是
// 「这段里哪些段是高基数 ID」（折叠改写，面向标签与模板）——三者共享
// 「按 '/' 分段」的口径，语义各归其位。
//
// # 折叠启发式（缺省）
//
//   - 纯 ASCII 数字段 → 折叠（issue/PR/project/user 数字主键）；
//   - hex 形态且长度 ≥7 的段 → 折叠（commit SHA / 短 SHA），大小写不限；
//     7 是 git 短 SHA 的业界展示下限，更短的 hex 串（"cafe" / "abc123"）
//     误伤面陡增，保留原样（有界词表）比折叠安全；
//   - UUID 形态段 → **缺省不折叠**（'-' 非 hex，缺省启发式看不见它——这是
//     已知基数泄漏面），WithClassifyUUID 显式打开；
//   - 其余词段（repo 名 / 动词段 "pulls"）→ 原样保留。
//
// 「非 ID 词段原样保留」是最重要的取舍，不是省事：repo 名这类段每部署
// 有界（几十个），且正是运维要切的维度；天真的「非数字全折叠」会把有用
// 维度一起抹掉。反过来，「词形态的高基数段」（用户名等）缺省同样保留——
// 这是已知边界：WithClassifyIDFunc 可按部署补充折叠判定，WithClassifyKeep
// 可把 ID 形态的有意义段（仓库名恰好叫 "2024"）显式钉住。
//
// # 边界与口径
//
//   - 输入是**纯 path**（req.URL.Path 形态），不是完整 URL；查询串不归本
//     原语管（要进标签由调用方自行决定——query 参数是另一个基数爆炸面）；
//   - 空串归一为 "/"：空标签没有基数意义，固定值让无 path 的请求聚合到
//     一条序列；首尾多余 '/' 归一（"/repos/o/" 与 "/repos/o" 是同一维度）；
//     输出恒为 '/' 起头的绝对形态；
//   - 中间空段（"a//b"）原样保留——折叠只针对 ID 段，不做路径清洗；
//   - hex 启发式对纯 a-f 英文词有极小误伤面（"defaced" 恰 7 字符全在 hex
//     字母集内会被折叠）——API path 段里这类词罕见，换取 SHA 段全覆盖是
//     合算的交易；
//   - 带符号数字形态（"-1"）不折叠：URL 里的数字 ID 实践中非负，带符号
//     段几乎总是哨兵值，折叠面宁可小。
//
// 并发：PathClassifier 构造后只读，Classify 可安全并发调用。

const (
	// defaultPlaceholder 缺省占位符——Prometheus 标签示例 / OpenAPI 风格。
	defaultPlaceholder = "{id}"

	// hexIDMinLen hex 形态折叠的最短长度：git 短 SHA 的业界展示下限。
	hexIDMinLen = 7
)

// PathClassifier 把 URL path 逐段分类、折叠成低基数模板的可配置分类器。
//
// 缺省启发式见文件头注释；零配置分类器等价包级 ClassifyPath。构造后只读。
type PathClassifier struct {
	placeholder string
	keep        map[string]struct{}
	uuid        bool
	idFunc      func(string) bool
}

// ClassifyOption 是 PathClassifier 的可选配置。
//
// 命名带 Classify 前缀而非裸 Option：本包与 pathx 其它导出符号同处一个
// import 组，裸 Option 会与 co-import 的其它包的 Option 撞名（矩阵硬规则）。
type ClassifyOption func(*PathClassifier)

// WithClassifyPlaceholder 自定义折叠段的占位符（原样输出，如 "{id}"、":id"、
// "<id>"——`{id}` 是 Prometheus 标签 / OpenAPI 惯例，`:id` 是 Gin/chi 路由
// 参数惯例）。空串或含 '/' 报错（NewPathClassifier fail-fast）：占位符替换
// 整个 path 段，为空会静默丢失标记、含 '/' 会拆出新的段边界，输出模板都
// 不再自洽。
func WithClassifyPlaceholder(p string) ClassifyOption {
	return func(c *PathClassifier) { c.placeholder = p }
}

// WithClassifyKeep 把段加入**保留清单**：命中的段原样保留，优先级压过一切
// 折叠判定（缺省启发式、UUID、补充判定都不例外）。用于钉住 ID 形态但语义
// 有界的段——仓库名恰好叫 "2024"、版本段恰好是长 hex 等。段按 URL path
// 段原文精确匹配（区分大小写、不去空白）；多次调用累加；空串忽略（空段
// 永不参与折叠，保留项无意义）。
func WithClassifyKeep(segs ...string) ClassifyOption {
	return func(c *PathClassifier) {
		for _, s := range segs {
			if s == "" {
				continue
			}
			c.keep[s] = struct{}{}
		}
	}
}

// WithClassifyUUID 打开 UUID 形态段的折叠（8-4-4-4-12 hex，大小写不限）。
// 缺省关闭——UUID 段在多数部署的 path 里不出现，而误折叠一个有界词段的
// 代价是抹掉维度，默认从严；但 API 以 UUID 为主键的部署（订单号、跟踪 ID）
// 不打开就是基数泄漏面。
func WithClassifyUUID() ClassifyOption {
	return func(c *PathClassifier) { c.uuid = true }
}

// WithClassifyIDFunc 追加一段**补充 ID 判定**：fn(seg) 为 true 的段折叠。
// OR 语义叠加在缺省启发式之上、不替换（数字 / hex≥7 缺省判定照常生效），
// 用于缺省启发式认不出的「词形态高基数段」——如多租户 path 里的用户名
// 段。保留清单优先级更高（WithClassifyKeep 命中的段不看本判定）。
// fn 为 nil 时回退默认（不扩展，不算非法）。
func WithClassifyIDFunc(fn func(string) bool) ClassifyOption {
	return func(c *PathClassifier) {
		if fn != nil {
			c.idFunc = fn
		}
	}
}

// NewPathClassifier 构造 PathClassifier。占位符非法（空串 / 含 '/'）时
// 报错（fail-fast，不带病运行）；其余配置项无非法值。
func NewPathClassifier(opts ...ClassifyOption) (*PathClassifier, error) {
	c := &PathClassifier{
		placeholder: defaultPlaceholder,
		keep:        map[string]struct{}{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	if c.placeholder == "" {
		return nil, fmt.Errorf("pathx: 占位符不能为空")
	}
	if strings.Contains(c.placeholder, "/") {
		return nil, fmt.Errorf("pathx: 非法占位符 %q：占位符替换整个 path 段，不能含 '/'", c.placeholder)
	}
	return c, nil
}

// Classify 把 path 折叠成低基数模板：
//
//	c.Classify("/api/v4/projects/1234/repository/commits")
//	// /api/v4/projects/{id}/repository/commits
//	c.Classify("/repos/octocat/hello/pulls/42/files")
//	// /repos/octocat/hello/pulls/{id}/files
//	c.Classify("/repos/my-org/my-repo")
//	// /repos/my-org/my-repo（repo 名有界且正是要切的维度，保留）
//
// 段级判定顺序：空段原样保留 → 保留清单命中原样保留 → 补充判定或缺省
// 启发式命中折叠为占位符 → 其余原样保留。输入输出口径见文件头「边界与
// 口径」。
func (c *PathClassifier) Classify(path string) string {
	if path == "" {
		return "/"
	}
	segs := strings.Split(strings.Trim(path, "/"), "/")
	for i, seg := range segs {
		if seg == "" {
			continue
		}
		if _, kept := c.keep[seg]; kept {
			continue
		}
		if c.isID(seg) {
			segs[i] = c.placeholder
		}
	}
	return "/" + strings.Join(segs, "/")
}

// isID 段级折叠判定：补充判定（调用方注入）OR 缺省启发式。
func (c *PathClassifier) isID(seg string) bool {
	if c.idFunc != nil && c.idFunc(seg) {
		return true
	}
	if isASCIIDigits(seg) {
		return true
	}
	if len(seg) >= hexIDMinLen && isHexRun(seg) {
		return true
	}
	return c.uuid && isUUIDShape(seg)
}

// ClassifyPath 用缺省分类器把 path 折叠成低基数模板——等价
// NewPathClassifier() 后 Classify（零选项构造不可能失败，故包级直出）：
//
//	pathx.ClassifyPath("/api/v4/projects/1234/repository/commits")
//	// /api/v4/projects/{id}/repository/commits
//	pathx.ClassifyPath("/repos/octocat/hello/pulls/42/files")
//	// /repos/octocat/hello/pulls/{id}/files
//	pathx.ClassifyPath("/repos/my-org/my-repo")
//	// /repos/my-org/my-repo（repo 名保留）
//
// 需要调启发式边界（UUID / 保留清单 / 占位符风格 / 补充判定）时用
// NewPathClassifier。典型接线（metrics path 标签）：
//
//	label := pathx.ClassifyPath(req.URL.Path)
func ClassifyPath(path string) string {
	return defaultClassify.Classify(path)
}

// defaultClassify 是 ClassifyPath 用的缺省分类器：零选项构造不可能失败，
// 构造后只读，可安全并发。
var defaultClassify = func() *PathClassifier {
	c, err := NewPathClassifier()
	if err != nil {
		panic("pathx: 缺省分类器构造失败: " + err.Error())
	}
	return c
}()

// isASCIIDigits 判定纯 ASCII 数字段（不含符号与空白）。刻意不用
// strconv.Atoi：它接受 "+1"/"-1" 带符号形态，而 URL 里的数字 ID 实践中
// 非负（见文件头「边界与口径」）。
func isASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// isHexRun 判定 hex 形态段（[0-9a-fA-F]+，大小写不限，长度下限由调用方控）。
func isHexRun(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if !isHexByte(s[i]) {
			return false
		}
	}
	return true
}

// isUUIDShape 判定 UUID 规范形态：8-4-4-4-12 hex，连字符位置固定，
// 大小写不限。连字符不在 hex 字母集内，缺省 hex 判定看不见 UUID——
// 这是 WithClassifyUUID 存在的理由。
func isUUIDShape(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := range len(s) {
		switch i {
		case 8, 13, 18, 23:
			if s[i] != '-' {
				return false
			}
		default:
			if !isHexByte(s[i]) {
				return false
			}
		}
	}
	return true
}

// isHexByte 单字符 hex 判定。
func isHexByte(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}
