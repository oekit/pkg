package stringx

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 本文件提供按「形态」脱敏的遮蔽器 Masker。
//
// 定位：stringx.NewReplacer 只认**字面量**模式，stringx.NewMask 只按**词表**
// 遮罩，二者都无法表达「按形态认出敏感信息 + 不可逆打码 + 保留定位线索」。
// Masker 补上这一层：调用方给出正则规则表，Masker 负责按形态打码。
//
// # 掩码格式
//
//	«name:****»           Keep=0（全掩）
//	head«name:****»tail   Keep=N（首尾各保留 N 个字符作定位线索）
//
// name 是规则的形态标签（Rule.Name），原样出现在输出里供审计读；掩码主体
// 恒为 4 个 '*'，长度固定，不随原值长度变化。
//
// # Keep 语义（保留位数是封顶）
//
// Keep 是「保留位数」的**封顶**：首尾各保留 Keep 个字符，保留的字符总数与
// 原值长度无关——原值越长，被隐藏的部分越多，掩码因此不可复原。
//   - n >= 2*Keep：保留首 Keep 个字符 + 尾 Keep 个字符。
//   - n <  2*Keep：实际保留位数收缩为 min(Keep, n/2)（首尾相同、互不重叠），
//     按实际长度处理，不补位、不越界；n < 2 时退化为全掩。
//
// # 规则顺序与相互污染
//
// 规则表按注册顺序给出，但打码是**单次扫描合并**，不是逐条规则顺序替换：
// 从左到右扫描输入文本，每个位置比较所有规则在该处的匹配，取
//  1. 起点最靠左的匹配；
//  2. 起点相同时取**最长**的匹配；
//  3. 长度也相同时取**注册顺序靠前**的规则。
//
// 命中后该段文本被替换为掩码，扫描位置跳到该段之后，**替换结果不再参与
// 任何规则的匹配**——因此规则 N 输出的形态标签不会被规则 N+1 二次处理
// （逐条顺序替换的实现会有这个自污染问题）。
//
// 提示：形态标签 Name 会出现在输出文本里。请让 Name 与其它规则的匹配
// 模式（尤其是「键名表」）错开，避免审计时把标签误读成原始内容。
//
// # 规则表由调用方完全提供
//
// 库不内置任何业务词表、密钥前缀族或正则：Rule.Re 与 Rule.Name 全部由
// 调用方给出，库只负责匹配与打码。
//
// 并发：Masker 构造后只读，Mask 可安全并发调用。

const (
	// maskLabelOpen 形态标签左定界符（U+00AB）。
	maskLabelOpen = '«'
	// maskLabelClose 形态标签右定界符（U+00BB）。
	maskLabelClose = '»'
	// maskBody 掩码主体，固定 4 个 '*'，不随原值长度变化。
	maskBody = "****"
)

// Rule 一条脱敏规则。
//
// 规则表由调用方完全提供，库不内置任何词表：Re 负责「按形态认出」待脱敏
// 内容，Name 是形态标签（会出现在掩码里供审计读），Keep 控制保留的定位
// 线索位数。
type Rule struct {
	// Name 形态标签，如 "url-userinfo"；会原样出现在掩码里供审计读。
	// 不能为空、不能与同一规则表内其它规则的 Name 重复，也不能含定界符
	// '«' / '»' 或控制字符（否则输出无法被稳定解析）。
	Name string
	// Re 匹配待脱敏内容的正则。命中的**整段**文本被替换为掩码
	// （本库不区分捕获组，捕获组只影响匹配范围，不影响替换范围）。
	// 为 nil 时 NewMasker 报错。零宽匹配（空匹配）会被忽略。
	Re *regexp.Regexp
	// Keep 掩码保留首尾各几个字符作定位线索；0 = 全掩。
	// 语义是**封顶**：保留的字符数不随原值长度变化（见包内说明）。
	// 负数视为非法，NewMasker 报错。
	Keep int
}

// Masker 形态脱敏器。
//
// 构造后只读，可安全并发使用。
type Masker struct {
	rules []maskRule
}

// maskRule 内部规则表示（构造后不变）。
type maskRule struct {
	name string
	re   *regexp.Regexp
	keep int
}

// maskState 单次 Mask 调用内的规则扫描缓存（不是 Masker 的状态）。
type maskState struct {
	start int  // 缓存匹配的起点（绝对下标）
	end   int  // 缓存匹配的终点（绝对下标，不含）
	valid bool // 缓存是否有效
	dead  bool // 该规则在当前位置之后已无匹配，可直接跳过
}

// NewMasker 创建形态脱敏器。
//
// 规则表由调用方完全提供，库不内置任何业务词表。返回 error 的情形：
//   - Re == nil；
//   - Name 为空；
//   - Name 重复（形态标签重复会让审计结果有歧义）；
//   - Name 含定界符 '«' / '»' 或控制字符（输出将无法稳定解析）；
//   - Keep 为负数。
//
// 规则表为空（nil）时返回可用的空脱敏器，Mask 原样返回输入。
func NewMasker(rules []Rule) (*Masker, error) {
	m := &Masker{rules: make([]maskRule, 0, len(rules))}
	seen := make(map[string]int, len(rules))
	for i, r := range rules {
		if r.Re == nil {
			return nil, fmt.Errorf("stringx: 规则 %d 的 Re 为 nil", i)
		}
		if r.Name == "" {
			return nil, fmt.Errorf("stringx: 规则 %d 的 Name 为空", i)
		}
		if r.Keep < 0 {
			return nil, fmt.Errorf("stringx: 规则 %q 的 Keep 为负数（%d）", r.Name, r.Keep)
		}
		if err := validMaskName(r.Name); err != nil {
			return nil, fmt.Errorf("stringx: 规则 %d 的 Name 非法: %w", i, err)
		}
		if prev, dup := seen[r.Name]; dup {
			return nil, fmt.Errorf("stringx: 规则 %q 重复（索引 %d 与 %d）", r.Name, prev, i)
		}
		seen[r.Name] = i
		m.rules = append(m.rules, maskRule{name: r.Name, re: r.Re, keep: r.Keep})
	}
	return m, nil
}

// validMaskName 校验形态标签：不能含定界符或控制字符。
func validMaskName(name string) error {
	for _, r := range name {
		switch {
		case r == maskLabelOpen || r == maskLabelClose:
			return fmt.Errorf("形态标签不能含定界符 %q/%q", maskLabelOpen, maskLabelClose)
		case unicode.IsControl(r):
			return fmt.Errorf("形态标签不能含控制字符 %U", r)
		}
	}
	return nil
}

// Mask 对 text 做形态脱敏，返回打码后的文本。
//
// 打码是单次扫描合并：命中某条规则后，该段文本被替换为
// head + «name:****» + tail，替换结果不再参与任何规则的匹配。
// 未命中任何规则的文本原样保留。
func (m *Masker) Mask(text string) string {
	if m == nil || len(m.rules) == 0 || text == "" {
		return text
	}
	// 扫描缓存是「每次调用」的局部状态：Masker 本身保持只读，可并发使用。
	states := make([]maskState, len(m.rules))
	var b strings.Builder
	b.Grow(len(text))
	pos := 0
	for pos <= len(text) {
		bestRule := -1
		bestStart, bestEnd := 0, 0
		for i := range m.rules {
			s, e, ok := m.rules[i].find(text, pos, &states[i])
			if !ok {
				continue
			}
			// 起点最左优先；起点相同取最长；长度相同保留先注册者
			// （严格大于才替换，故等长时先注册者胜出）。
			if bestRule == -1 || s < bestStart || (s == bestStart && e > bestEnd) {
				bestRule, bestStart, bestEnd = i, s, e
			}
		}
		if bestRule == -1 {
			break
		}
		// 命中起点之前的文本没有任何规则可匹配（原样保留）。
		b.WriteString(text[pos:bestStart])
		b.WriteString(m.maskValue(bestRule, text[bestStart:bestEnd]))
		pos = bestEnd
	}
	b.WriteString(text[pos:])
	return b.String()
}

// find 返回该规则在 pos 及其之后的**最左非空匹配**（绝对下标区间）。
//
// 语义等价于「每次都从 pos 起重新查找最左匹配」：缓存只在缓存匹配起点
// 仍 >= pos 时复用，否则按 pos 重搜；因此规则之间互相覆盖后不会残留
// 过期的匹配位置。零宽匹配（空匹配）被忽略——掩码零宽匹配没有意义，
// 且会让扫描位置无法前进。
func (r *maskRule) find(text string, pos int, st *maskState) (int, int, bool) {
	if st.dead {
		return 0, 0, false
	}
	if st.valid && st.start >= pos {
		return st.start, st.end, true
	}
	s := pos
	for s <= len(text) {
		loc := r.re.FindStringIndex(text[s:])
		if loc == nil {
			st.dead = true
			st.valid = false
			return 0, 0, false
		}
		absStart, absEnd := s+loc[0], s+loc[1]
		if absEnd > absStart {
			st.start, st.end, st.valid = absStart, absEnd, true
			return absStart, absEnd, true
		}
		// 空匹配：按 rune 边界前移一位再找（避免切进多字节字符中间）。
		_, size := utf8.DecodeRuneInString(text[s:])
		if size <= 0 {
			size = 1
		}
		s += size
	}
	st.dead = true
	st.valid = false
	return 0, 0, false
}

// maskValue 生成一段匹配文本的掩码表示。
//
// Keep 是封顶：实际保留位 = min(Keep, n/2)（n 为原值 rune 数），首尾相同
// 且互不重叠——短于 2*Keep 的值按实际长度收缩，不补位、不越界。
func (m *Masker) maskValue(idx int, matched string) string {
	r := &m.rules[idx]
	keep := r.keep
	if keep > 0 {
		runes := []rune(matched)
		if n := len(runes); n/2 < keep {
			keep = n / 2
		}
		if keep > 0 {
			head := string(runes[:keep])
			tail := string(runes[len(runes)-keep:])
			return head + string(maskLabelOpen) + r.name + ":" + maskBody + string(maskLabelClose) + tail
		}
	}
	return string(maskLabelOpen) + r.name + ":" + maskBody + string(maskLabelClose)
}
