package container

import (
	"strings"
	"sync"
	"testing"
)

// TestVersionedCell_PublishLoadVersion 基本面：构造值代次 0，每次 Publish
// 恰好 +1，Load 返回的值与代次成对。
func TestVersionedCell_PublishLoadVersion(t *testing.T) {
	c := NewVersionedCell[*int](&initialVal)

	if got := c.Version(); got != 0 {
		t.Fatalf("构造值代次 want 0 got %d", got)
	}
	if v, gen := c.Load(); v != &initialVal || gen != 0 {
		t.Fatalf("未发布时 Load 应返回构造值与代次 0，实得 (%v, %d)", v, gen)
	}

	next := 2
	c.Publish(&next)
	if got := c.Version(); got != 1 {
		t.Fatalf("首次 Publish后代次 want 1 got %d", got)
	}
	if v, gen := c.Load(); v != &next || gen != 1 {
		t.Fatalf("Load 应返回新值与新代次的成对结果，实得 (%v, %d)", v, gen)
	}

	third := 3
	c.Publish(&third)
	if got := c.Version(); got != 2 {
		t.Fatalf("第二次 Publish后代次 want 2 got %d", got)
	}
}

// initialVal 是指针型测试的固定构造值（包级变量保证指针身份稳定可比对）。
var initialVal = 1

// TestVersionedCell_ZeroValueUsable 零值可用（对齐 core/governance 零值
// Store 的口径，core#193）：未发布时 Load 返回零值与代次 0、Version 0；
// 首次 Publish 从 1 起步（nil 单元视作 gen 0），不回卷、不跳代。
func TestVersionedCell_ZeroValueUsable(t *testing.T) {
	var c VersionedCell[*int]

	if v, gen := c.Load(); v != nil || gen != 0 {
		t.Fatalf("零值 cell Load 应返回 (nil, 0)，实得 (%v, %d)", v, gen)
	}
	if got := c.Version(); got != 0 {
		t.Fatalf("零值 cell Version want 0 got %d", got)
	}

	v1 := 1
	c.Publish(&v1)
	if got := c.Version(); got != 1 {
		t.Fatalf("零值 cell 首次 Publish 代次 want 1 got %d", got)
	}
	v2 := 2
	c.Publish(&v2)
	if got := c.Version(); got != 2 {
		t.Fatalf("零值 cell 第二次 Publish 代次 want 2 got %d", got)
	}
	if v, gen := c.Load(); v != &v2 || gen != 2 {
		t.Fatalf("零值 cell 发布后 Load 应成对，实得 (%v, %d)", v, gen)
	}
}

// TestVersionedCell_NilPointerPublishedAsIs 机制层不解读 nil：指针类型的
// nil 会原样发布并推进代次——「nil 忽略」（Snapshot 口径）与「nil→空配置」
// （Store 口径）是包装层策略，在调用 Publish 之前由消费方自行判定。
func TestVersionedCell_NilPointerPublishedAsIs(t *testing.T) {
	c := NewVersionedCell[*int](nil)

	if got := c.Version(); got != 0 {
		t.Fatalf("NewVersionedCell(nil) 构造代次 want 0 got %d", got)
	}
	c.Publish(nil) // 机制层原样发布 nil（T = *int）
	if got := c.Version(); got != 1 {
		t.Fatalf("Publish(nil) 应原样发布并推进代次 want 1 got %d", got)
	}
	if v, gen := c.Load(); v != nil || gen != 1 {
		t.Fatalf("Publish(nil) 后 Load 应返回 (nil, 1)，实得 (%v, %d)", v, gen)
	}
}

// TestVersionedCell_ValueTypeCopiesOnLoad 值类型语义：Publish 存入的是
// 入参的副本，发布方改自己的变量不波及已发布快照（不可变纪律对值类型
// 天然成立，测试钉住的是「Load 返回发布那一刻的值」）。
func TestVersionedCell_ValueTypeCopiesOnLoad(t *testing.T) {
	c := NewVersionedCell(1)
	v := 2
	c.Publish(v)
	v = 99 // 发布之后再改入参
	if v != 99 {
		t.Fatalf("发布方本地改写应生效（证明后续读快照与本地变量无关）")
	}
	if got, gen := c.Load(); got != 2 || gen != 1 {
		t.Fatalf("已发布快照不得被发布方的后续改写波及，实得 (%d, %d)", got, gen)
	}
}

// TestVersionedCell_ConcurrentPublishGenExact 并发发布：P 个发布者各发布
// M 次，每次成功发布恰好推进一代——终态代次必须等于总发布次数（CAS 重试
// 语义下代次既不丢失也不重复）。
func TestVersionedCell_ConcurrentPublishGenExact(t *testing.T) {
	const publishers, perPublisher = 8, 500
	c := NewVersionedCell(0)

	var wg sync.WaitGroup
	for p := range publishers {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := range perPublisher {
				c.Publish(base*perPublisher + i + 1)
			}
		}(p)
	}
	wg.Wait()

	want := uint64(publishers * perPublisher)
	if got := c.Version(); got != want {
		t.Fatalf("并发发布后代次 want %d got %d（CAS 循环不得丢代）", want, got)
	}
}

// TestVersionedCell_ReaderSeesConsistentPairs 并发发布+并发读：读侧拿到的
// (值, 代次) 必须是成对的——每个代次只对应一个发布值，且单读者的代次单调
// 不回卷。-race 下同时压 Publish 与 Load。
func TestVersionedCell_ReaderSeesConsistentPairs(t *testing.T) {
	const publishers, perPublisher = 4, 200
	total := uint64(publishers * perPublisher)
	c := NewVersionedCell[*int](nil)

	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		lastGen := uint64(0)
		seen := make(map[uint64]*int)
		for {
			select {
			case <-stop:
				return
			default:
			}
			v, gen := c.Load()
			if gen < lastGen {
				t.Errorf("读侧代次回卷：last %d cur %d", lastGen, gen)
				return
			}
			lastGen = gen
			if gen > 0 {
				if v == nil {
					t.Errorf("代次 %d 的读出值为 nil：值与代次未成对发布", gen)
					return
				}
				if prev, ok := seen[gen]; ok && prev != v {
					t.Errorf("同一代次 %d 读到两个不同值：单元被撕裂", gen)
					return
				}
				seen[gen] = v
			}
		}
	}()

	var wg sync.WaitGroup
	for p := range publishers {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := range perPublisher {
				n := base*perPublisher + i + 1
				c.Publish(&n)
			}
		}(p)
	}
	wg.Wait()
	close(stop)
	<-readerDone

	if got := c.Version(); got != total {
		t.Fatalf("终态代次 want %d got %d", total, got)
	}
}

// TestVersionedCell_StringNoValue String 只含代次不含值（值里常有密钥）。
func TestVersionedCell_StringNoValue(t *testing.T) {
	c := NewVersionedCell("secret-payload")
	c.Publish("next-secret")
	got := c.String()
	for _, secret := range []string{"secret-payload", "next-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("String 不得泄露值 %q，实得 %q", secret, got)
		}
	}
	if !strings.Contains(got, "gen:1") {
		t.Fatalf("String 应含当前代次 gen:1，实得 %q", got)
	}
}
