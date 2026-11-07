package pagination

import (
	pkgerrors "github.com/oekit/pkg/errors"
)

// ErrBudgetExceeded 是 WalkEach 的页数预算哨兵（#211）：行走因 budget 上限
// 停止而非自然取完（短页）。调用方 errors.Is 判定后自行决定加大预算重扫
// 或接受部分结果——被预算截断的行走**不完整**，静默当全量用是消费方的
// 事故（上游生态的无预算全量扫描形态后来自己都要在 doc 里提醒「列表多
// 时每次都是全量扫描」，预算是防失控护栏，不是可选优化）。
var ErrBudgetExceeded = pkgerrors.NewBizError(1, "pagination: 页数预算耗尽（行走未自然结束）")

// ErrNilYielder 是 WalkEach 的 yield 回调为 nil 哨兵（同 ErrNilFetcher 口径）。
var ErrNilYielder = pkgerrors.NewBizError(1, "pagination: yield 不能为 nil")

// WalkEach 按页行走第三方分页 API 的**流式形态**（#211）——与 Walk
// （翻页聚合单源）同族两个入口：
//
//   - Walk：收集全量返回（[]T, error），短页按 pageSize 判定，适合「取
//     全量再处理」；
//   - WalkEach：逐条经 yield 回调流出，返回 false **提前终止整个行走**
//     （不是跳页）——「找到即停」的搜索型消费（按 tag 找 release）不必
//     扫全量；适合大结果集与流式处理。
//
// 语义（三家下游独立实现收敛出的取舍）：
//
//   - 页号从 1 起、逐页递增；fetch(page) 返回该页条目（回调式而非接口式
//     ——消费方的 fetch 都是闭包，捕获各自的 SDK client 与 perPage）；
//   - **短页停止**：len(items) < 上一页长度 即最后一页——不多打一次空页
//     请求；首页为空直接结束；
//   - budget 是页数上限（防失控护栏）：fetch 页数达到 budget 仍未走到
//     短页时返回 ErrBudgetExceeded。budget <= 0 表示无预算（不建议——
//     上游恒返整页时行走不终止；对照 Walk 的 MaxWalkPages 硬防呆）；
//   - yield 返回 false 提前终止，返回 nil（主动终止不是错误）；
//   - fetch 返回错误中止行走，错误带页码上下文包装（errors.Is 链路保持）；
//   - 不做并发预取（同 Walk——上游 API 多数按页序有 token/游标依赖）。
//
// fetch/yield 为 nil 返回 ErrNilFetcher / ErrNilYielder。游标制 API 的
// 行走是另一个域，不在本原语范围。需要 ctx 取消语义的调用方在 fetch /
// yield 闭包内自行检查（本形态的 fetch 不携带 ctx——它与 Walk 的差别
// 恰在「行走过程不占用 ctx 生命周期」）。
//
// 用法：
//
//	var found *Release
//	err := pagination.WalkEach(50,
//	    func(page int) ([]*Release, error) { return client.ListReleases(ctx, page, 100) },
//	    func(r *Release) bool {
//	        if hasTag(r, want) {
//	            found = r
//	            return false // 找到即停
//	        }
//	        return true
//	    },
//	)
func WalkEach[T any](budget int, fetch func(page int) ([]T, error), yield func(T) bool) error {
	if fetch == nil {
		return ErrNilFetcher
	}
	if yield == nil {
		return ErrNilYielder
	}

	prevLen := -1
	for page := 1; budget <= 0 || page <= budget; page++ {
		items, err := fetch(page)
		if err != nil {
			return pkgerrors.Wrap(err, "pagination.WalkEach fetch", "page", page)
		}
		for _, item := range items {
			if !yield(item) {
				return nil // 调用方主动终止
			}
		}
		// 短页即最后一页（首页空也在此结束）
		if prevLen >= 0 && len(items) < prevLen {
			return nil
		}
		if len(items) == 0 {
			return nil
		}
		prevLen = len(items)
	}
	return pkgerrors.Wrap(ErrBudgetExceeded, "pagination.WalkEach", "budget", budget)
}
