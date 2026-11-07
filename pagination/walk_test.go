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

// 整除边界：恰好 2 页满页后第 3 页空。
func TestWalk_ExactPages(t *testing.T) {
	pages := 0
	got, err := Walk(context.Background(), 5, func(_ context.Context, page int) ([]int, error) {
		pages++
		if page <= 2 {
			return []int{1, 1, 1, 1, 1}, nil
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 || pages != 3 {
		t.Fatalf("len=%d pages=%d, want 10/3", len(got), pages)
	}
}

// 页失败即中止，不带半截结果。
func TestWalk_PageErrorAborts(t *testing.T) {
	boom := errors.New("boom")
	_, err := Walk(context.Background(), 2, func(_ context.Context, page int) ([]int, error) {
		if page == 3 {
			return nil, boom
		}
		return []int{1, 2}, nil
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

// 契约违例防呆：上游越界页恒返整页 → 撞 MaxWalkPages 而非死循环。
func TestWalk_TooManyPages(t *testing.T) {
	// MaxWalkPages 全跑一遍太慢，用小页数验证防呆路径存在即可——
	// 直接走循环语义：恒返整页时 Walk 必然报 ErrTooManyPages。
	_, err := Walk(context.Background(), 1, func(_ context.Context, page int) ([]int, error) {
		return []int{page}, nil // 永远满页
	})
	if !errors.Is(err, ErrTooManyPages) {
		t.Fatalf("want ErrTooManyPages, got %v", err)
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
