package pagination

import "math"

const (
	// DefaultPage 是默认页码。
	DefaultPage = 1
	// DefaultPageSize 是默认每页条数。
	DefaultPageSize = 20
	// MaxPageSize 是每页最大条数。
	MaxPageSize = 500
)

// PageRequest 表示分页查询请求参数。
type PageRequest struct {
	Page     int `json:"page" query:"page" form:"page"`
	PageSize int `json:"page_size" query:"page_size" form:"page_size"`
}

// NewPageRequest 根据页码和每页条数创建 PageRequest。
func NewPageRequest(page, pageSize int) *PageRequest {
	return &PageRequest{Page: page, PageSize: pageSize}
}

// Normalize 规范化分页参数，为非法值设置默认值，并限制最大每页条数。
func (r *PageRequest) Normalize() {
	if r.Page <= 0 {
		r.Page = DefaultPage
	}
	if r.PageSize <= 0 {
		r.PageSize = DefaultPageSize
	}
	if r.PageSize > MaxPageSize {
		r.PageSize = MaxPageSize
	}
}

// Offset 返回分页查询的偏移量。
// 当计算结果溢出时返回 0（回退到第一页）。
// 只读:不在接收者上落 Normalize 的写 — 共享 PageRequest(如全局默认值)
// 并发调 Offset/Limit 时是数据竞争(2026-09-06 复审修复)。
func (r *PageRequest) Offset() int {
	page, pageSize := r.normalized()
	// 溢出保护：**乘法前**判，而不是乘法后看结果是否为负——
	// int 溢出回绕后的结果有一半是正数（实测 page=1<<62+2、pageSize=500 ⇒
	// 真值 2.3e18 超出 int64 ⇒ 回绕成 +500，被原判据放过去 ⇒ 偏移量完全错误）。
	// page 来自 query/form 参数（外部可控）⇒ 不能靠「回绕后是否为负」兜底。
	if page > 1 && pageSize > 0 && page-1 > math.MaxInt/pageSize {
		return 0
	}
	offset := (page - 1) * pageSize
	if offset < 0 {
		return 0
	}
	return offset
}

// Limit 返回分页查询的限制条数。
func (r *PageRequest) Limit() int {
	_, pageSize := r.normalized()
	return pageSize
}

// normalized 返回归一化后的 (page, pageSize),不修改接收者。
func (r *PageRequest) normalized() (int, int) {
	page, pageSize := r.Page, r.PageSize
	if page <= 0 {
		page = DefaultPage
	}
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return page, pageSize
}
