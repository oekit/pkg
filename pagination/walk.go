package pagination

import (
	"context"

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
	// 整页）或数据量真的越界，两者都需要人来看。
	ErrTooManyPages = pkgerrors.NewBizError(1, "pagination: 页数超出上限（疑似上游恒返整页）")
)

// Walk 翻页聚合单源：以固定 pageSize 从第 1 页起逐页调 fetch，把全部元素
// 按页序拼接返回。终止条件是**短页即末页**——某页返回的元素数少于 pageSize
// （含 0）即为最后一页，此后不再请求。
//
// 契约（fetch 一侧）：
//   - page 从 1 计数，fetch 自行把页码与页长翻译成具体 API 的查询参数；
//   - fetch 必须**诚实短页**：末页返回实际条数而非凑满 pageSize。上游 API
//     若在越界页恒返整页，终止条件永不满足，将撞 MaxWalkPages 上限返回
//     ErrTooManyPages——这是防呆，不是功能；
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
		if len(items) > 0 {
			all = append(all, items...)
		}
		if len(items) < pageSize {
			return all, nil
		}
	}
}
