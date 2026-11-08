package pagination

import (
	"context"
	"reflect"

	pkgerrors "github.com/oekit/pkg/errors"
)

// MaxWalkPages 单次 Walk 的页数上限：按最小可用页长 1 计也远超真实 API 的
// 数据量，唯一目的是把「上游恒返整页」这类契约违例从死循环变成明确报错。
const MaxWalkPages = 10000

var (
	// ErrBadPageSize pageSize 非法（<=0）。
	ErrBadPageSize = pkgerrors.NewBizError(1, "pagination: pageSize 必须为正数")
	// ErrNilFetcher fetch 回调为 nil。
	ErrNilFetcher = pkgerrors.NewBizError(1, "pagination: fetch 不能为 nil")
	// ErrTooManyPages 页数超出 MaxWalkPages——上游违反短页契约（越界页恒返
	// **内容不同**的整页）或数据量真的越界，两者都需要人来看。「恒返同一批
	// 整页」的伪翻页形态由无进展检测收口（#285，返回已收集数据不报错）。
	ErrTooManyPages = pkgerrors.NewBizError(1, "pagination: 页数超出上限（疑似上游恒返整页）")
)

// Walk 翻页聚合单源：以固定 pageSize 从第 1 页起逐页调 fetch，把全部元素
// 按页序拼接返回。终止条件有三：
//
//   - **短页即末页**——某页返回的元素数少于 pageSize（含 0）即为最后一页，
//     此后不再请求；
//   - **伪翻页（无进展）即末尾**（#285）：第 2 页起与上一页逐值相同
//     （reflect.DeepEqual）——「忽略 page 参数」的服务器（实测 Forgejo
//     15.0.1 的 issue 评论列表）每页返回同一批全量数据，长度判定对它与
//     正常等长页序列不可区分，唯一可靠信号是逐值相同。此时视为列表已到
//     末尾，返回已收集数据（首页即全量，部分数据优于空转到 MaxWalkPages
//     报错丢全部）；「忽略 page 但遵守 limit」的形态会截断到首页，doc
//     显式声明该取舍。上一页快照为逐值拷贝，fetch 复用底层缓冲不会造成
//     别名误判；正常等长页序列（内容页页不同）不误判。
//
// 契约（fetch 一侧）：
//   - page 从 1 计数，fetch 自行把页码与页长翻译成具体 API 的查询参数；
//   - fetch 必须**诚实短页**：末页返回实际条数而非凑满 pageSize。上游 API
//     若在越界页恒返**内容不同**的整页（数据量真的越界或变体契约违例），
//     短页与无进展判定都不满足，将撞 MaxWalkPages 上限返回 ErrTooManyPages
//     ——这是防呆，不是功能；
//   - 任何一页 fetch 失败立即返回该页错误（带页码上下文），已聚合的部分
//     不返回——调用方要么拿到全量要么拿到错误，没有半截结果；
//   - 逐页做 ctx 取消检查，长翻页调用方传值即可获得取消语义。
//
// 不做并发预取：上游 API 多数按页序有 token/游标依赖，收益不值得复杂度。
// 此前 zentao-mini 的 client_* 五个「翻页直到 len<pageSize」循环各写一份
// （页长 100/200/500 各异），下沉为一份原语（上游 zentao-mini#22）。
func Walk[T any](ctx context.Context, pageSize int, fetch func(ctx context.Context, page int) ([]T, error)) ([]T, error) {
	if pageSize <= 0 {
		return nil, ErrBadPageSize
	}
	if fetch == nil {
		return nil, ErrNilFetcher
	}

	var all []T
	var prev []T
	for page := 1; ; page++ {
		if page > MaxWalkPages {
			return nil, pkgerrors.Wrap(ErrTooManyPages, "pagination.Walk", "pages", page-1, "page_size", pageSize)
		}
		if err := ctx.Err(); err != nil {
			return nil, pkgerrors.Wrap(err, "pagination.Walk ctx", "page", page)
		}

		items, err := fetch(ctx, page)
		if err != nil {
			return nil, pkgerrors.Wrap(err, "pagination.Walk fetch", "page", page, "page_size", pageSize)
		}
		// 伪翻页（无进展）检测（#285）：同页重复 = 列表已到尾（首页即
		// 全量），返回已收集数据优于整体报错；重复页不再拼接（不产出重复条目）
		if page > 1 && reflect.DeepEqual(items, prev) {
			return all, nil
		}
		if len(items) > 0 {
			all = append(all, items...)
		}
		if len(items) < pageSize {
			return all, nil
		}
		// 上一页快照逐值拷贝：fetch 原地复用底层缓冲时别名比较会恒等误判
		prev = append(prev[:0], items...)
	}
}
