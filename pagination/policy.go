package pagination

import "math"

// OverMaxPolicy 描述「请求页长超过上限」时的处理语义。
//
// 两种语义都真实存在于消费方，且**响应条数不同**（可观测差异，不能顺手统一）：
//
//   - OverMaxClamp：钳到 MaxPageSize（pkg/pagination 现行语义）。
//   - OverMaxFallback：回退到 DefaultPageSize。
type OverMaxPolicy int

const (
	// OverMaxClamp 把超限的页长钳到 MaxPageSize。
	// 这是 OverMaxPolicy 的零值，也是 pkg/pagination 的现行语义。
	OverMaxClamp OverMaxPolicy = iota

	// OverMaxFallback 把超限的页长回退到 DefaultPageSize。
	OverMaxFallback
)

// Policy 是服务级分页口径——把「默认页长 / 上限 / 超限语义」从包级常量里提出来，
// 让每个服务声明自己的口径，而不是各写一份归一化逻辑。
//
// 零值退回包级默认（DefaultPage / DefaultPageSize / MaxPageSize / OverMaxClamp），
// 因此 `Policy{}` 与包级常量口径完全一致。
//
// 字段语义：
//
//   - DefaultPage：<= 0 时取包级 DefaultPage。
//   - DefaultPageSize：<= 0 时取包级 DefaultPageSize。
//   - MaxPageSize：== 0 时取包级 MaxPageSize；< 0 表示**不限上限**（如「默认 20、不设上限」
//     的服务）；> 0 时为该服务的页长上限。
//   - OverMax：页长超上限时的语义；未知取值按 OverMaxClamp 处理。
//
// 例：默认 50、上限 200、超限回退默认（等价于把口径写死在服务里）：
//
//	Policy{DefaultPageSize: 50, MaxPageSize: 200, OverMax: OverMaxFallback}
//
// 例：默认 20、不限上限：
//
//	Policy{DefaultPageSize: 20, MaxPageSize: -1}
type Policy struct {
	// DefaultPage 是非法页码（<= 0）时归一化到的页码。
	DefaultPage int
	// DefaultPageSize 是非法页长（<= 0）时归一化到的页长。
	DefaultPageSize int
	// MaxPageSize 是页长上限；< 0 表示不限上限。
	MaxPageSize int
	// OverMax 是页长超上限时的处理语义。
	OverMax OverMaxPolicy
}

// defaultPage 返回口径的默认页码。
func (p Policy) defaultPage() int {
	if p.DefaultPage <= 0 {
		return DefaultPage
	}
	return p.DefaultPage
}

// defaultPageSize 返回口径的默认页长。
func (p Policy) defaultPageSize() int {
	if p.DefaultPageSize <= 0 {
		return DefaultPageSize
	}
	return p.DefaultPageSize
}

// maxPageSize 返回口径的页长上限；<= 0 表示不限上限。
func (p Policy) maxPageSize() int {
	if p.MaxPageSize == 0 {
		return MaxPageSize
	}
	return p.MaxPageSize
}

// unlimited 报告该口径是否不限页长上限。
func (p Policy) unlimited() bool {
	return p.maxPageSize() < 0
}

// Normalize 按本口径归一化页码与页长，返回归一化后的 (page, pageSize)。
//
// 规则：
//   - page <= 0 ⇒ 口径的默认页码。
//   - pageSize <= 0 ⇒ 口径的默认页长。
//   - pageSize 超上限 ⇒ 按 OverMax 语义处理：钳到上限（OverMaxClamp）
//     或回退到默认页长（OverMaxFallback）；口径不限上限时原样返回。
//
// 纯函数，不修改入参。
func (p Policy) Normalize(page, pageSize int) (int, int) {
	if page <= 0 {
		page = p.defaultPage()
	}
	if pageSize <= 0 {
		pageSize = p.defaultPageSize()
	}
	if p.unlimited() {
		return page, pageSize
	}
	maxSize := p.maxPageSize()
	if pageSize <= maxSize {
		return page, pageSize
	}
	if p.OverMax == OverMaxFallback {
		return page, p.defaultPageSize()
	}
	return page, maxSize
}

// Offset 返回按本口径归一化后的偏移量。
// 归一化结果溢出（int 溢出成负数）时返回 0（回退到第一页）。
func (p Policy) Offset(page, pageSize int) int {
	page, pageSize = p.Normalize(page, pageSize)
	// 溢出保护：**乘法前**判，而不是乘法后看结果是否为负——int 溢出回绕后的
	// 结果有一半是正数（实测 page=1<<62+2、pageSize=500 ⇒ 真值 2.3e18 超出
	// int64 ⇒ 回绕成 +500，被原判据放过去 ⇒ 偏移量完全错误）。
	if page > 1 && pageSize > 0 && page-1 > math.MaxInt/pageSize {
		return 0
	}
	offset := (page - 1) * pageSize
	if offset < 0 {
		return 0
	}
	return offset
}

// Limit 返回按本口径归一化后的限制条数。
func (p Policy) Limit(page, pageSize int) int {
	_, pageSize = p.Normalize(page, pageSize)
	return pageSize
}

// NormalizeLimit 按「最多返回多少条」的口径归一化条数。
//
// 与 Normalize 的区别只在**口径维度**：Normalize 说的是「第几页、每页几条」
// （两个参数）；本方法说的是「最多返回多少条」——**没有页的概念**，如工具的
// limit 参数（语义是行数），或任何「只要前 N 条」的场景。
//
// 规则（页长档语义完全一致，字段复用 DefaultPageSize / MaxPageSize / OverMax，
// 不另起一套几乎一样的口径）：
//   - n <= 0 ⇒ 口径的默认条数。
//   - n 超上限 ⇒ 按 OverMax 语义处理：钳到上限（OverMaxClamp）或回退到默认条数
//     （OverMaxFallback）；口径不限上限时原样返回。
//
// 等价于 Limit(1, n)，但不必传一个恒为 1 的页码——把「第 1 页的页长」当「条数」
// 用是拧的：读代码的人会先愣一下才反应过来这里没有页。
func (p Policy) NormalizeLimit(n int) int {
	return p.Limit(1, n)
}
