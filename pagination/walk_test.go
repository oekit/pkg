package pagination

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	pkgerrors "github.com/oekit/pkg/errors"
)

// 全量页 + 短页收尾：1..98 共 98 条、页长 30 → 4 页（30/30/30/8）。
func TestWalk_ShortPageEnds(t *testing.T) {
	got, err := Walk(context.Background(), 30, func(_ context.Context, page int) ([]int, error) {
		start := (page - 1) * 30
		end := start + 30
		if end > 98 {
			end = 98
		}
		out := make([]int, 0, end-start)
		for i := start; i < end; i++ {
			out = append(out, i+1)
		}
		return out, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 98 || got[0] != 1 || got[97] != 98 {
		t.Fatalf("聚合结果异常: len=%d head=%d tail=%d", len(got), got[0], got[97])
	}
}

// 空数据集：第 1 页即空页，返回空切片不报错。
func TestWalk_Empty(t *testing.T) {
	got, err := Walk(context.Background(), 10, func(_ context.Context, _ int) ([]int, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want 空, got %v", got)
	}
}

// 整除边界：恰好 2 页满页后第 3 页空。两页内容必须不同——#285 起「逐值相同
// 的整页」已是伪翻页（列表末尾）的合法语义，等长页序列靠内容区分。
func TestWalk_ExactPages(t *testing.T) {
	pages := 0
	got, err := Walk(context.Background(), 5, func(_ context.Context, page int) ([]int, error) {
		pages++
		switch page {
		case 1:
			return []int{1, 2, 3, 4, 5}, nil
		case 2:
			return []int{6, 7, 8, 9, 10}, nil
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 || got[0] != 1 || got[9] != 10 || pages != 3 {
		t.Fatalf("len=%d head=%d tail=%d pages=%d, want 10/1/10/3", len(got), got[0], got[9], pages)
	}
}

// 页失败即中止，不带半截结果。（页内容随页变化——逐值相同的满页已是
// 伪翻页语义，会在第 3 页的 boom 之前无进展收手，#285 口径）
func TestWalk_PageErrorAborts(t *testing.T) {
	boom := errors.New("boom")
	_, err := Walk(context.Background(), 2, func(_ context.Context, page int) ([]int, error) {
		if page == 3 {
			return nil, boom
		}
		return []int{page, page + 1}, nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("want boom 链路, got %v", err)
	}
}

// ctx 取消透传。
func TestWalk_CtxCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Walk(ctx, 2, func(_ context.Context, _ int) ([]int, error) {
		return []int{1, 2}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// 非法入参哨兵。
func TestWalk_BadInput(t *testing.T) {
	if _, err := Walk[int](context.Background(), 0, func(_ context.Context, _ int) ([]int, error) { return nil, nil }); !errors.Is(err, ErrBadPageSize) {
		t.Fatalf("want ErrBadPageSize, got %v", err)
	}
	if _, err := Walk[int](context.Background(), 10, nil); !errors.Is(err, ErrNilFetcher) {
		t.Fatalf("want ErrNilFetcher, got %v", err)
	}
}

// 契约违例防呆：上游越界页恒返**内容不同**的整页 → 撞 MaxWalkPages 而非
// 死循环（内容随页变化的整页不受 #285 无进展检测影响——它只认逐值相同）。
func TestWalk_TooManyPages(t *testing.T) {
	// MaxWalkPages 全跑一遍太慢，用小页数验证防呆路径存在即可——
	// 直接走循环语义：恒返整页时 Walk 必然报 ErrTooManyPages。
	_, err := Walk(context.Background(), 1, func(_ context.Context, page int) ([]int, error) {
		return []int{page}, nil // 永远满页且内容随页变化
	})
	if !errors.Is(err, ErrTooManyPages) {
		t.Fatalf("want ErrTooManyPages, got %v", err)
	}
}

// ---- 伪翻页（无进展）检测（#285）----

// 伪分页 server（Forgejo 15.0.1 现场形态）：忽略 page 参数恒返同一批整页。
// 此前撞 MaxWalkPages 报 ErrTooManyPages 丢掉全部数据；现在第 2 页无进展即
// 按列表末尾收手——返回已收集数据（首页即全量）、恰好 2 页即止。
func TestWalk_PseudoPaginationReturnsCollected(t *testing.T) {
	batch := []int{7, 8, 9}
	calls := 0
	got, err := Walk(context.Background(), 3, func(_ context.Context, _ int) ([]int, error) {
		calls++
		return batch, nil // 忽略 page：恒返同一批
	})
	if err != nil {
		t.Fatalf("伪翻页应按列表末尾收手返回已收集数据, got %v", err)
	}
	if len(got) != 3 || got[0] != 7 || got[2] != 9 {
		t.Fatalf("应返回首页全量且不拼接重复页, got=%v", got)
	}
	if calls != 2 {
		t.Fatalf("应恰好 2 页即止, calls=%d", calls)
	}
}

// 正常等长页序列不误判：4 个等长满页（内容页页不同）+ 空页收尾。
func TestWalk_EqualLengthDistinctPagesNoMisfire(t *testing.T) {
	calls := 0
	got, err := Walk(context.Background(), 2, func(_ context.Context, page int) ([]int, error) {
		calls++
		if page > 4 {
			return nil, nil
		}
		return []int{page * 10, page*10 + 1}, nil // 等长、内容不同
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 || got[0] != 10 || got[7] != 41 {
		t.Fatalf("等长不同内容页应全部聚合, got=%v", got)
	}
	if calls != 5 { // 4 个等长满页 + 第 5 页空收尾
		t.Fatalf("不应被无进展检测误停, calls=%d", calls)
	}
}

// fetch 原地复用底层缓冲（pooled buffer 形态）：别名比较会恒等误判，
// 上一页快照逐值拷贝后不再误停。
func TestWalk_BufferReuseFetchNoMisfire(t *testing.T) {
	buf := make([]int, 2)
	calls := 0
	got, err := Walk(context.Background(), 2, func(_ context.Context, page int) ([]int, error) {
		calls++
		if page > 3 {
			return buf[:1], nil // 短页收尾
		}
		buf[0] = page * 10
		buf[1] = page*10 + 1
		return buf, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 4 { // 3 个复用缓冲的等长满页 + 短页收尾
		t.Fatalf("复用缓冲不应被别名误停, calls=%d", calls)
	}
	if len(got) != 7 { // 10,11,20,21,30,31,30
		t.Fatalf("应聚合全部条目, len=%d", len(got))
	}
}

// 页序保持：多页拼接严格按页序。
func TestWalk_OrderPreserved(t *testing.T) {
	got, err := Walk(context.Background(), 3, func(_ context.Context, page int) ([]string, error) {
		if page > 3 {
			return nil, nil
		}
		return []string{fmt.Sprintf("p%d-a", page), fmt.Sprintf("p%d-b", page), fmt.Sprintf("p%d-c", page)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "p1-a,p1-b,p1-c,p2-a,p2-b,p2-c,p3-a,p3-b,p3-c"
	if strings.Join(got, ",") != want {
		t.Fatalf("页序错乱: %s", strings.Join(got, ","))
	}
	if pkgerrors.Cause(nil) != nil { // 编译期引用保持（errors 单源纪律）
		t.Fatal("unreachable")
	}
}

// Walk + 指针元素池化 fetch（#318）：已收集条目必须与 fetch 缓冲脱钩——
// 浅拷贝时页 2 覆写页 1 槽位，返回值变成「页 1 槽位、页 2 内容」，页 1
// 数据丢失（实测 Walk collected=2 ids=[3 4]）。
func TestWalk_PooledPointerFetchCollectsAllPages(t *testing.T) {
	slots := make([]pooledRelease, 2)
	next := 0
	calls := 0
	fetch := func(_ context.Context, page int) ([]*pooledRelease, error) {
		calls++
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
	got, err := Walk(context.Background(), 2, fetch)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("应走到短页收尾, calls=%d", calls)
	}
	if len(got) != 5 {
		t.Fatalf("应聚合全部 5 条（不被别名误判丢页）, len=%d ids=%v", len(got), releaseIDs(got))
	}
	for i, r := range got {
		if r.ID != i+1 {
			t.Fatalf("条目 %d 的 ID = %d, want %d（聚合内容不得被后续页覆写）", i, r.ID, i+1)
		}
	}
}

func releaseIDs(rs []*pooledRelease) []int {
	out := make([]int, len(rs))
	for i, r := range rs {
		out[i] = r.ID
	}
	return out
}
