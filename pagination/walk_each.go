package pagination

import (
	"context"
	"reflect"

	pkgerrors "github.com/oekit/pkg/errors"
)

// ErrBudgetExceeded 是 WalkEach 的页数预算哨兵（#211）：行走因 budget 上限
// 停止而非自然取完（短页/无进展）。调用方 errors.Is 判定后自行决定加大预算
// 重扫或接受部分结果——被预算截断的行走**不完整**，静默当全量用是消费方的
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
//   - ctx 贯穿（#286）：WalkEach 逐页把 ctx 传给 fetch 并在页间做
//     ctx.Err() 检查（与 Walk 同形态）——数千页的长行走拿到取消信号即
//     中止（错误带页码上下文，errors.Is 链路保持），不必等 fetch 内部
//     自行失败或预算耗尽；fetch 闭包把 ctx 继续透传给下游 SDK 的调用
//     即获得页内取消；
//   - 页号从 1 起、逐页递增；fetch(ctx, page) 返回该页条目（回调式而非
//     接口式——消费方的 fetch 都是闭包，捕获各自的 SDK client 与 perPage）；
//   - **短页停止**：len(items) < 上一页长度 即最后一页——不多打一次空页
//     请求；首页为空直接结束；
//   - **伪翻页（无进展）停止**（#285）：第 2 页起与上一页逐值相同
//     （reflect.DeepEqual）即视为列表已到末尾（「忽略 page 参数」的服务器
//     形态，如 Forgejo 15.0.1 的 issue 评论列表——首页即全量，这正是它
//     无法分页的原因）——按末尾收手、返回/流出已收集数据，优于空转到
//     预算耗尽后整体报错丢全部；上一页快照为逐值拷贝，fetch 复用底层
//     缓冲（pooled buffer 原地覆写）不会造成别名误判；正常等长页序列
//     （内容页页不同）不误判，无进展收手不算自然结束、不报预算哨兵；
//   - budget 是页数上限（防失控护栏）：fetch 页数达到 budget 仍未走到
//     短页/无进展时返回 ErrBudgetExceeded。budget <= 0 表示无预算（不
//     建议——对照 Walk 的 MaxWalkPages 硬防呆）；
//   - yield 返回 false 提前终止，返回 nil（主动终止不是错误）；
//   - fetch 返回错误中止行走，错误带页码上下文包装（errors.Is 链路保持）；
//   - 不做并发预取（同 Walk——上游 API 多数按页序有 token/游标依赖）。
//
// fetch/yield 为 nil 返回 ErrNilFetcher / ErrNilYielder。ctx 传 nil 视同
// context.Background()（与仓内「nil ctx 兜底」总则一致）。游标制 API 的
// 行走是另一个域，不在本原语范围。
//
// 用法：
//
//	var found *Release
//	err := pagination.WalkEach(ctx, 50,
//	    func(ctx context.Context, page int) ([]*Release, error) {
//	        return client.ListReleases(ctx, page, 100)
//	    },
//	    func(r *Release) bool {
//	        if hasTag(r, want) {
//	            found = r
//	            return false // 找到即停
//	        }
//	        return true
//	    },
//	)
func WalkEach[T any](ctx context.Context, budget int, fetch func(ctx context.Context, page int) ([]T, error), yield func(T) bool) error {
	if ctx == nil { // nil ctx 显式兜底 Background（仓规总则②）
		ctx = context.Background()
	}
	if fetch == nil {
		return ErrNilFetcher
	}
	if yield == nil {
		return ErrNilYielder
	}

	prevLen := -1
	var prev []T
	for page := 1; budget <= 0 || page <= budget; page++ {
		// 页间取消检查（#286）：长行走拿到取消信号即中止
		if err := ctx.Err(); err != nil {
			return pkgerrors.Wrap(err, "pagination.WalkEach ctx", "page", page)
		}
		items, err := fetch(ctx, page)
		if err != nil {
			return pkgerrors.Wrap(err, "pagination.WalkEach fetch", "page", page)
		}
		// 伪翻页（无进展）检测（#285）：同页重复 = 列表已到尾——在
		// yield 之前收手，重复页不再流出（消费者不会收到两份数据）
		if page > 1 && reflect.DeepEqual(items, prev) {
			return nil
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
		// 上一页快照逐值拷贝：fetch 若原地复用底层缓冲（pooled buffer），
		// 别名比较会恒等误判成伪翻页（#285 用例钉住）
		prev = append(prev[:0], items...)
	}
	return pkgerrors.Wrap(ErrBudgetExceeded, "pagination.WalkEach", "budget", budget)
}
