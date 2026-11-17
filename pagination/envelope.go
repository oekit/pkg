package pagination

import "math"

// 分页响应信封的计算原语：Pages（总页数）与 Page（回显页码）两个字段的
// 口径单源（gormx#248）。此前 gormx.Paginate 就地各写一份——pages 用
// float64 Ceil、溢出回显用就地判据；下游（gormx 等）消费本原语后删除
// 各自的副本，信封计算不再漂移。

// offsetOverflows 报告 (page-1)*pageSize 是否超出 int 表示范围——**乘法前**
// 判，而不是乘法后看结果是否为负：int 溢出回绕后的结果有一半是**正数**
// （实测 page=1<<62+2、pageSize=500 ⇒ 真值 2.3e18 超出 int64 ⇒ 回绕成
// +500，被「回绕后为负」的原判据放过去 ⇒ 偏移量完全错误）。page 来自
// query/form 参数（外部可控）⇒ 不能靠「回绕后是否为负」兜底。
//
// Policy.Offset / PageRequest.Offset / ServedPage 三处共用同一判据
// （#233 溢出口径单源，gormx#248 下沉），任何一侧修判据不会漂移出另外两处。
func offsetOverflows(page, pageSize int) bool {
	return page > 1 && pageSize > 0 && page-1 > math.MaxInt/pageSize
}

// ComputePages 由 total 与 pageSize 计算响应信封的总页数（向上取整）。
//
// 规则：
//   - total <= 0 或 pageSize <= 0 ⇒ 0（无数据无页）；
//   - 整数运算向上取整（商 + 余数判进位），不用 float64——float64 对超出
//     2^53 的 total 丢失精度（±1 被舍入吞掉 ⇒ 尾页差一页），整数运算对
//     全域 int64 精确；
//   - 结果超出 int 表示范围时饱和到 math.MaxInt（32 位平台上 total 极大而
//     pageSize 很小时才会触达），不回绕成负数。
//
// pageSize 传**实际生效**的页长（调用方先归一化，如 Policy.Normalize /
// PageRequest.Normalize）；本函数不做归一化。
func ComputePages(total int64, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	ps := int64(pageSize)
	pages := total / ps
	if total%ps > 0 {
		pages++
	}
	if pages > math.MaxInt {
		return math.MaxInt
	}
	return int(pages)
}

// ServedPage 返回响应信封应回显的页码：偏移量溢出（offsetOverflows，与
// Policy.Offset 同一判据）时**实际服务的是第一页**——回显落回实际服务的页，
// 而不是自称一个从未被服务的超大页码（#233：信封说 page=999999、内容却是
// 第一页的自相矛盾回显）。
//
// 入参是即将回显的 page/pageSize（Paginate 流程中已归一化）；本函数只做
// 溢出回退、不做归一化——page <= 0 原样返回（归一化是调用方的职责，
// Policy.Normalize / PageRequest.Normalize）。
func ServedPage(page, pageSize int) int {
	if offsetOverflows(page, pageSize) {
		return DefaultPage
	}
	return page
}
