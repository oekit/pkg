package pagination

// WalkEach 流式分页行走测试（#211）：短页停止、yield 提前终止、budget
// 哨兵、fetch 错误包装、ctx 贯穿（#286）、伪翻页无进展检测（#285）、
// 与 Walk 的分工。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// pagedFetcher 构造一个按页返回 1..total 的 fetch 闭包，记录实际请求的页号。
func pagedFetcher(total, perPage int, requested *[]int) func(context.Context, int) ([]int, error) {
	return func(_ context.Context, page int) ([]int, error) {
		if requested != nil {
			*requested = append(*requested, page)
		}
		start := (page - 1) * perPage
		if start >= total {
			return nil, nil
		}
		end := start + perPage
		if end > total {
			end = total
		}
		out := make([]int, 0, end-start)
		for i := start; i < end; i++ {
			out = append(out, i+1)
		}
		return out, nil
	}
}

// 全量行走 + 短页停止：total=98、perPage=30 ⇒ 4 页（30/30/30/8），
// 第 4 页是短页，**不再请求第 5 页**。
func TestWalkEach_ShortPageStops(t *testing.T) {
	var requested []int
	var got []int
	err := WalkEach(context.Background(), 50, pagedFetcher(98, 30, &requested), func(v int) bool {
		got = append(got, v)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 98 || got[0] != 1 || got[97] != 98 {
		t.Fatalf("应流出全部 98 条, got len=%d", len(got))
	}
	if fmt.Sprint(requested) != "[1 2 3 4]" {
		t.Fatalf("短页后不应再请求, requested=%v", requested)
	}
}

// yield 返回 false 提前终止整个行走（不是跳页）——「找到即停」。
func TestWalkEach_YieldFalseStops(t *testing.T) {
	var requested []int
	found := -1
	err := WalkEach(context.Background(), 50, pagedFetcher(1000, 10, &requested), func(v int) bool {
		if v == 25 { // 第 3 页中部
			found = v
			return false
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if found != 25 {
		t.Fatalf("应找到 25, got %d", found)
	}
	if len(requested) != 3 {
		t.Fatalf("终止后不应再请求后续页, requested=%v", requested)
	}
}

// budget 耗尽返回 ErrBudgetExceeded：行走未自然结束（短页），结果不完整。
// fetch 恒返整页但内容页页不同（页号进内容）——它同时钉住「等长不同内容的
// 失控页序列不触发无进展误判」（#285 口径：无进展只认逐值相同）。
func TestWalkEach_BudgetExceeded(t *testing.T) {
	pages := 0
	err := WalkEach(context.Background(), 3, func(_ context.Context, page int) ([]int, error) {
		pages++
		return []int{page, page, page}, nil // 恒返整页且内容随页变化
	}, func(int) bool { return true })
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("want ErrBudgetExceeded, got %v", err)
	}
	if pages != 3 {
		t.Fatalf("应恰好请求 budget=3 页, got %d", pages)
	}
}

// budget 恰好覆盖到短页：不报预算哨兵。
func TestWalkEach_BudgetCoversShortPage(t *testing.T) {
	var requested []int
	err := WalkEach(context.Background(), 4, pagedFetcher(98, 30, &requested), func(int) bool { return true })
	if err != nil {
		t.Fatalf("预算覆盖自然结束不应报错: %v", err)
	}
	if len(requested) != 4 {
		t.Fatalf("requested=%v", requested)
	}
}

// budget <= 0 表示无预算（不建议）：短页仍自然终止。
func TestWalkEach_ZeroBudgetUnlimited(t *testing.T) {
	var requested []int
	err := WalkEach(context.Background(), 0, pagedFetcher(70, 30, &requested), func(int) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(requested) != 3 { // 30/30/10——第 3 页短页结束
		t.Fatalf("无预算应走到短页, requested=%v", requested)
	}
}

// 首页为空：直接结束、零流出、nil 错误。
func TestWalkEach_EmptyFirstPage(t *testing.T) {
	yielded := 0
	err := WalkEach(context.Background(), 10, func(context.Context, int) ([]int, error) {
		return nil, nil
	}, func(int) bool {
		yielded++
		return true
	})
	if err != nil || yielded != 0 {
		t.Fatalf("want (nil err, 0 条), got (%v, %d)", err, yielded)
	}
}

// fetch 错误中止行走，错误带页码上下文且 errors.Is 链路保持。
func TestWalkEach_FetchErrorAborts(t *testing.T) {
	boom := errors.New("boom")
	calls := 0
	err := WalkEach(context.Background(), 10, func(_ context.Context, page int) ([]int, error) {
		calls++
		if page == 2 {
			return nil, boom
		}
		return []int{1}, nil
	}, func(int) bool { return true })
	if !errors.Is(err, boom) {
		t.Fatalf("want boom 链路, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("错误应中止行走, calls=%d", calls)
	}
	if !strings.Contains(err.Error(), "page") {
		t.Fatalf("错误应带页码上下文, got %v", err)
	}
}

// 非法入参哨兵。
func TestWalkEach_BadInput(t *testing.T) {
	fetch := func(context.Context, int) ([]int, error) { return nil, nil }
	yield := func(int) bool { return true }
	if err := WalkEach(context.Background(), 10, nil, yield); !errors.Is(err, ErrNilFetcher) {
		t.Fatalf("want ErrNilFetcher, got %v", err)
	}
	if err := WalkEach(context.Background(), 10, fetch, nil); !errors.Is(err, ErrNilYielder) {
		t.Fatalf("want ErrNilYielder, got %v", err)
	}
}

// 页序保持：条目严格按页序流出。
func TestWalkEach_OrderPreserved(t *testing.T) {
	var got []string
	err := WalkEach(context.Background(), 10, func(_ context.Context, page int) ([]string, error) {
		if page > 2 {
			return nil, nil
		}
		return []string{fmt.Sprintf("p%d-a", page), fmt.Sprintf("p%d-b", page)}, nil
	}, func(s string) bool {
		got = append(got, s)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "p1-a,p1-b,p2-a,p2-b"; strings.Join(got, ",") != want {
		t.Fatalf("页序错乱: %s", strings.Join(got, ","))
	}
}

// ---- ctx 贯穿（#286）----

// ctx 取消：页间检查即时中止（错误带页码上下文、errors.Is 链路保持）。
func TestWalkEach_CtxCanceledStopsBetweenPages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	err := WalkEach(ctx, 50, func(_ context.Context, page int) ([]int, error) {
		calls++
		if page == 2 {
			cancel() // 第 2 页在飞时取消
		}
		return []int{page}, nil
	}, func(int) bool { return true })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled 链路, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("取消后不应再请求第 3 页, calls=%d", calls)
	}
	if !strings.Contains(err.Error(), "page") {
		t.Fatalf("ctx 错误应带页码上下文, got %v", err)
	}
}

// ctx 贯穿：fetch 收到的就是 WalkEach 入参那个 ctx（下游 SDK 透传即获得
// 页内取消）；nil ctx 兜底 Background 不 panic。
func TestWalkEach_FetchReceivesWalkCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var got context.Context
	if err := WalkEach(ctx, 1, func(c context.Context, _ int) ([]int, error) {
		got = c
		return nil, nil // 空首页自然收尾
	}, func(int) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if got != ctx {
		t.Fatal("fetch 应回收 WalkEach 入参的同一 ctx（ctx 贯穿）")
	}
	// nil ctx 兜底（仓规总则②：nil = 显式回退默认值）
	var nilCtx context.Context = nil
	calls := 0
	err := WalkEach(nilCtx, 1, func(c context.Context, _ int) ([]int, error) {
		calls++
		if c == nil {
			t.Fatal("兜底后的 ctx 不应为 nil")
		}
		return nil, nil // 空首页：自然收尾
	}, func(int) bool { return true })
	if err != nil || calls != 1 {
		t.Fatalf("nil ctx 应兜底 Background 正常行走, got (%v, %d)", err, calls)
	}
}

// ---- 伪翻页（无进展）检测（#285）----

// 伪分页 server（Forgejo 15.0.1 现场形态）：忽略 page 参数，每页返回同一批
// 全量数据（同长、非空、同内容）。此前空转到 budget 耗尽报错丢全部；现在
// 第 2 页无进展即按列表末尾收手——nil 错误、恰好流出一份首页数据（重复页
// 不再流出）、不再打第 3 页。
func TestWalkEach_PseudoPaginationStopsAtNoProgress(t *testing.T) {
	batch := []int{101, 102, 103}
	var requested []int
	var got []int
	err := WalkEach(context.Background(), 50, func(_ context.Context, page int) ([]int, error) {
		requested = append(requested, page)
		return batch, nil // 忽略 page：恒返同一批
	}, func(v int) bool {
		got = append(got, v)
		return true
	})
	if err != nil {
		t.Fatalf("伪翻页应按列表末尾收手（返回已收集数据优于报错丢全部）, got %v", err)
	}
	if fmt.Sprint(requested) != "[1 2]" {
		t.Fatalf("应在第 2 页无进展即收手, requested=%v", requested)
	}
	if fmt.Sprint(got) != "[101 102 103]" {
		t.Fatalf("应恰好流出一份首页数据、重复页不流出, got=%v", got)
	}
}

// 正常等长页序列不误判：4 个**等长**满页（内容页页不同）+ 空页收尾——
// 长度判定识别不了伪翻页，无进展检测也不能反过来把等长页序列误杀
// （用内容不同的等长页区分，正是 issue 的判别口径）。
func TestWalkEach_EqualLengthDistinctPagesNoMisfire(t *testing.T) {
	var requested []int
	var got []int
	err := WalkEach(context.Background(), 50, pagedFetcher(8, 2, &requested), func(v int) bool {
		got = append(got, v)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 || got[0] != 1 || got[7] != 8 {
		t.Fatalf("等长不同内容页应全部流出, got=%v", got)
	}
	if fmt.Sprint(requested) != "[1 2 3 4 5]" {
		t.Fatalf("应穿过全部 4 个等长满页到空页收尾（不被无进展误停）, requested=%v", requested)
	}
}

// fetch 原地复用底层缓冲（pooled buffer 形态）：上一页快照必须逐值拷贝，
// 否则别名比较恒等——每个等长页都会被误判成伪翻页在第 2 页误停。
func TestWalkEach_BufferReuseFetchNoMisfire(t *testing.T) {
	buf := make([]int, 2)
	var requested []int
	var got []int
	err := WalkEach(context.Background(), 50, func(_ context.Context, page int) ([]int, error) {
		requested = append(requested, page)
		if page > 3 {
			return buf[:1], nil // 短页收尾
		}
		buf[0] = page * 10
		buf[1] = page*10 + 1
		return buf, nil
	}, func(v int) bool {
		got = append(got, v)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(requested) != "[1 2 3 4]" {
		t.Fatalf("复用缓冲的等长页序列不应被别名误停, requested=%v", requested)
	}
	if len(got) != 7 { // 10,11,20,21,30,31,40
		t.Fatalf("应流出全部 7 条, got=%v", got)
	}
}

// ---- 指针元素池化 fetch（#318）----

// pooledRelease 模池化上游返回的结构体（带未导出字段：整体 memcpy 路径——
// reflect 逐字段 Set 对未导出字段会 panic，#318 顺带钉住不 panic）。
type pooledRelease struct {
	ID    int
	title string
}

// 指针元素 + 原地覆写：页 1 [1,2] → 页 2 [3,4]（同一批槽位覆写）。快照若只
// 浅拷贝指针，页 2 items 与 prev 持有同一批指针 ⇒ DeepEqual 恒真 ⇒ 被误判成
// 伪翻页在第 2 页收尾，页 2 起的真实数据静默丢弃（err=nil）。
func TestWalkEach_PooledPointerFetchKeepsYielding(t *testing.T) {
	slots := make([]pooledRelease, 2)
	next := 0
	fetch := func(_ context.Context, page int) ([]*pooledRelease, error) {
		n := 2
		if page > 2 {
			n = 1 // 短页收尾
		}
		out := make([]*pooledRelease, n)
		for i := 0; i < n; i++ {
			next++
			slots[i] = pooledRelease{ID: next, title: fmt.Sprintf("r%d", next)}
			out[i] = &slots[i]
		}
		return out, nil
	}
	var ids []int
	err := WalkEach(context.Background(), 10, fetch, func(r *pooledRelease) bool {
		ids = append(ids, r.ID)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids) != "[1 2 3 4 5]" {
		t.Fatalf("页 2 起不得被别名误判丢弃, ids=%v", ids)
	}
}

// 指针元素的无进展检测不被深拷贝破坏：池化 fetch 每页覆写同一批槽位但
// **值**相同（「忽略 page 参数」的服务器形态，#285 的指针版）——按值比较
// 仍判伪翻页，第 2 页收尾。
func TestWalkEach_PooledPointerNoProgressStillDetected(t *testing.T) {
	slots := make([]pooledRelease, 2)
	calls := 0
	fetch := func(_ context.Context, page int) ([]*pooledRelease, error) {
		calls++
		for i := range slots {
			slots[i] = pooledRelease{ID: 1} // 每页同值覆写
		}
		out := make([]*pooledRelease, len(slots))
		for i := range slots {
			out[i] = &slots[i]
		}
		return out, nil
	}
	count := 0
	err := WalkEach(context.Background(), 10, fetch, func(*pooledRelease) bool {
		count++
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("逐值相同的指针页应判伪翻页收尾, calls=%d", calls)
	}
	if count != 2 {
		t.Fatalf("重复页不应再流出, yielded=%d", count)
	}
}
