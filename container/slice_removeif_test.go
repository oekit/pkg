package container

// Slice.RemoveIf 原子谓词删除测试（#199）：AppendIfAbsent 的删除侧对偶。

import (
	"strings"
	"sync"
	"testing"
)

type hostEntry struct {
	host string
	port int
}

func TestSlice_RemoveIf_RemovesMatchingAndCounts(t *testing.T) {
	s := NewSlice("a", "b", "a", "c", "a")
	if got := s.RemoveIf(func(v string) bool { return v == "a" }); got != 3 {
		t.Fatalf("应删除 3 个, got %d", got)
	}
	if got := s.Snapshot(); len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("删除后应保留 b,c（注册序）, got %v", got)
	}
}

func TestSlice_RemoveIf_NoMatchKeepsAll(t *testing.T) {
	s := NewSlice(1, 2, 3)
	if got := s.RemoveIf(func(v int) bool { return v > 100 }); got != 0 {
		t.Fatalf("无命中应返回 0, got %d", got)
	}
	if s.Len() != 3 {
		t.Fatalf("无命中不应改动切片, len=%d", s.Len())
	}
}

func TestSlice_RemoveIf_EmptyAndNilPred(t *testing.T) {
	s := NewSlice[int]()
	if got := s.RemoveIf(func(int) bool { return true }); got != 0 {
		t.Fatalf("空切片应返回 0, got %d", got)
	}
	s2 := NewSlice(1, 2)
	if got := s2.RemoveIf(nil); got != 0 {
		t.Fatalf("nil 谓词应 no-op 返回 0, got %d", got)
	}
	if s2.Len() != 2 {
		t.Fatalf("nil 谓词不应改动切片, len=%d", s2.Len())
	}
}

func TestSlice_RemoveIf_CustomPredicate(t *testing.T) {
	// host 别名注销场景（#199 的驱动用例）：按 EqualFold 语义注销
	s := NewSlice(
		hostEntry{host: "GitLab.com", port: 443},
		hostEntry{host: "github.com", port: 443},
		hostEntry{host: "gitee.com", port: 443},
	)
	removed := s.RemoveIf(func(e hostEntry) bool {
		return strings.EqualFold(e.host, "gitlab.com")
	})
	if removed != 1 {
		t.Fatalf("应按大小写不敏感命中删除 1 个, got %d", removed)
	}
	got := s.Snapshot()
	if len(got) != 2 || got[0].host != "github.com" || got[1].host != "gitee.com" {
		t.Fatalf("删除后应保留其余别名（注册序）, got %v", got)
	}
}

func TestSlice_RemoveIf_AllMatchEmpties(t *testing.T) {
	s := NewSlice(1, 2, 3)
	if got := s.RemoveIf(func(int) bool { return true }); got != 3 {
		t.Fatalf("应删除全部 3 个, got %d", got)
	}
	if s.Len() != 0 {
		t.Fatalf("应清空, len=%d", s.Len())
	}
}

// TestSlice_RemoveIf_ConcurrentWithAppend 并发 Append/RemoveIf 不丢不错
// （-race 下稳定）：最终长度 = 基数 + 成功追加数 - 命中删除数。
// 两步组合（Range 快照找下标 + Delete）在这个测试形态下会删错元素——
// 本测试钉住原子语义。
func TestSlice_RemoveIf_ConcurrentWithAppend(t *testing.T) {
	s := NewSlice[int]()
	// 预置 10 个偶数（谓词删除目标）
	for i := 0; i < 10; i++ {
		s.Append(i * 2)
	}

	const writers = 8
	var wg sync.WaitGroup
	wg.Add(writers)
	for w := 0; w < writers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				s.Append(1) // 奇数追加——谓词不命中
			}
		}()
	}
	// 并发谓词删除：只删偶数
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			s.RemoveIf(func(v int) bool { return v%2 == 0 })
		}
	}()
	wg.Wait()

	// 不变量：剩余元素全部是奇数（偶数最终全被删净），数量 = 成功追加数
	total := 0
	s.Range(func(_ int, v int) bool {
		if v%2 == 0 {
			t.Errorf("偶数 %d 不应残留（谓词删除最终删净）", v)
		}
		total++
		return true
	})
	if total != writers*100 {
		t.Fatalf("应保留全部追加的奇数 %d, got %d", writers*100, total)
	}
}
