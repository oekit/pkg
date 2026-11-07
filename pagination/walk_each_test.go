package pagination

// WalkEach 流式分页行走测试（#211）：短页停止、yield 提前终止、budget
// 哨兵、fetch 错误包装、与 Walk 的分工。

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// pagedFetcher 构造一个按页返回 1..total 的 fetch 闭包，记录实际请求的页号。
func pagedFetcher(total, perPage int, requested *[]int) func(page int) ([]int, error) {
	return func(page int) ([]int, error) {
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
	err := WalkEach(50, pagedFetcher(98, 30, &requested), func(v int) bool {
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
	err := WalkEach(50, pagedFetcher(1000, 10, &requested), func(v int) bool {
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
func TestWalkEach_BudgetExceeded(t *testing.T) {
	pages := 0
	err := WalkEach(3, func(page int) ([]int, error) {
		pages++
		return []int{page, page, page}, nil // 恒返整页——永不短页
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
	err := WalkEach(4, pagedFetcher(98, 30, &requested), func(int) bool { return true })
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
	err := WalkEach(0, pagedFetcher(70, 30, &requested), func(int) bool { return true })
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
	err := WalkEach(10, func(int) ([]int, error) { return nil, nil }, func(int) bool {
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
	err := WalkEach(10, func(page int) ([]int, error) {
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
	fetch := func(int) ([]int, error) { return nil, nil }
	yield := func(int) bool { return true }
	if err := WalkEach(10, nil, yield); !errors.Is(err, ErrNilFetcher) {
		t.Fatalf("want ErrNilFetcher, got %v", err)
	}
	if err := WalkEach(10, fetch, nil); !errors.Is(err, ErrNilYielder) {
		t.Fatalf("want ErrNilYielder, got %v", err)
	}
}

// 页序保持：条目严格按页序流出。
func TestWalkEach_OrderPreserved(t *testing.T) {
	var got []string
	err := WalkEach(10, func(page int) ([]string, error) {
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
