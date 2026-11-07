package cache

// 写后失效（write-invalidate）与在途回源的竞态测试（#200）：
//
// 缺陷：GetOrLoad 在途回源期间调用方 Del(key)（写库后失效缓存），
// Source 返回的是 Del 之前的旧值，load() 却无条件回填 ⇒ 旧值重新占据
// 缓存整个 TTL 窗口，失效语义被击穿。
//
// 修正：LocalCache 引入 per-key 世代号——写路径（Set/SetWithTTL/SetNX/
// Del/DelBatch/回填）推进世代；load() 在 Source 返回后、回填前复查世代，
// 不一致则放弃回填（取到的值仍返回给发起读的调用方）。
//
// 全部用 channel 同步时序（Source 进入/放行），不 sleep，-race 下稳定。

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingSource 构造一个首调阻塞、先取数后放行的回源函数：
// 第一次调用先把 current 的值取走（模拟 DB 查询已发生），再等放行——
// 放行前主测 goroutine 可以完成 Del/Set，制造「Source 拿的是失效前旧值、
// 返回发生在失效之后」的竞态窗口。后续调用直接返回 current 当前值。
type blockingSource struct {
	entered chan struct{} // 首次调用已进入（已取数、开始阻塞）
	release chan struct{} // 放行首次调用

	mu      sync.Mutex
	current string
	calls   atomic.Int64
}

func newBlockingSource(initial string) *blockingSource {
	return &blockingSource{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		current: initial,
	}
}

func (b *blockingSource) source(_ context.Context, _ string) (*string, error) {
	n := b.calls.Add(1)
	b.mu.Lock()
	v := b.current // 先取数（查询发生在放行之前）
	b.mu.Unlock()
	if n == 1 {
		close(b.entered)
		<-b.release
	}
	return &v, nil
}

func (b *blockingSource) set(v string) {
	b.mu.Lock()
	b.current = v
	b.mu.Unlock()
}

// loadResult 是一次在途 GetOrLoad 的结果。
type loadResult struct {
	v   *string
	err error
}

// TestSingleFlightCache_DelDuringInFlightLoad_DoesNotBackfillStale 钉住 #200
// 主场景：Del 在回源在途时发生 ⇒ 旧值不回填，下一次读重新回源拿新值。
func TestSingleFlightCache_DelDuringInFlightLoad_DoesNotBackfillStale(t *testing.T) {
	src := newBlockingSource("old")
	inner := NewLocalCache(LocalConfig[string]{TTL: time.Minute, Source: src.source})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache[string](inner)
	ctx := context.Background()

	done := make(chan loadResult, 1)
	go func() {
		v, err := sf.GetOrLoad(ctx, "k")
		done <- loadResult{v, err}
	}()

	<-src.entered  // 回源在途（已取走旧值）
	src.set("new") // 模拟写库
	if err := sf.Del(ctx, "k"); err != nil {
		t.Fatalf("Del: %v", err)
	}
	close(src.release) // Source 返回旧值（取数在 Del 之前）

	r := <-done
	if r.err != nil {
		t.Fatalf("在途回源应成功: %v", r.err)
	}
	if r.v == nil || *r.v != "old" {
		t.Fatalf("在途回源应把它拿到的值返回给发起方, got %v", r.v)
	}

	// 失效必须生效：下一次读不得命中被击穿的旧值，必须重新回源
	v, err := sf.GetOrLoad(ctx, "k")
	if err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	if v == nil || *v != "new" {
		t.Fatalf("Del 后的下一次读应拿到新值 new, got %v", v)
	}
	if got := src.calls.Load(); got != 2 {
		t.Fatalf("旧值不得回填 ⇒ 应回源 2 次, got %d", got)
	}
}

// TestLocalCache_DelDuringInFlightLoad_DoesNotBackfillStale 同场景、内层直连
// （不经装饰器）——世代号在 LocalCache 本体生效。
func TestLocalCache_DelDuringInFlightLoad_DoesNotBackfillStale(t *testing.T) {
	src := newBlockingSource("old")
	c := NewLocalCache(LocalConfig[string]{TTL: time.Minute, Source: src.source})
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	ctx := context.Background()

	done := make(chan loadResult, 1)
	go func() {
		v, err := c.GetOrLoad(ctx, "k")
		done <- loadResult{v, err}
	}()

	<-src.entered
	src.set("new")
	if err := c.Del(ctx, "k"); err != nil {
		t.Fatalf("Del: %v", err)
	}
	close(src.release)
	r := <-done
	if r.err != nil || r.v == nil || *r.v != "old" {
		t.Fatalf("在途回源应返回它拿到的旧值, got (%v, %v)", r.v, r.err)
	}

	v, err := c.GetOrLoad(ctx, "k")
	if err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	if v == nil || *v != "new" {
		t.Fatalf("Del 后的下一次读应拿到新值, got %v", v)
	}
	if got := src.calls.Load(); got != 2 {
		t.Fatalf("旧值不得回填 ⇒ 应回源 2 次, got %d", got)
	}
}

// TestSingleFlightCache_SetDuringInFlightLoad_KeepsNewerValue：在途回源期间
// 调用方显式 Set 新值 ⇒ 回填放弃，显式写入不被旧值覆盖。
func TestSingleFlightCache_SetDuringInFlightLoad_KeepsNewerValue(t *testing.T) {
	src := newBlockingSource("old")
	inner := NewLocalCache(LocalConfig[string]{TTL: time.Minute, Source: src.source})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache[string](inner)
	ctx := context.Background()

	done := make(chan loadResult, 1)
	go func() {
		v, err := sf.GetOrLoad(ctx, "k")
		done <- loadResult{v, err}
	}()

	<-src.entered
	setVal := "explicit"
	if err := sf.Set(ctx, "k", &setVal); err != nil {
		t.Fatalf("Set: %v", err)
	}
	close(src.release)
	if r := <-done; r.err != nil || r.v == nil || *r.v != "old" {
		t.Fatalf("在途回源应返回它拿到的旧值, got (%v, %v)", r.v, r.err)
	}

	// 显式 Set 的值必须在——不被在途回源的旧值回填覆盖
	v, err := sf.GetOrLoad(ctx, "k")
	if err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	if v == nil || *v != setVal {
		t.Fatalf("显式 Set 的值不得被在途回源覆盖, got %v", v)
	}
	if got := src.calls.Load(); got != 1 {
		t.Fatalf("显式写入在场 ⇒ 不应再次回源, got %d 次", got)
	}
}

// TestSingleFlightCache_DelDuringInFlightLoad_SkipsNullMarkerBackfill：null
// marker 回填同样受世代复查约束——失效后的 null marker 不得把下一次读
// 挡在 NullTTL 窗口外。
func TestSingleFlightCache_DelDuringInFlightLoad_SkipsNullMarkerBackfill(t *testing.T) {
	var (
		calls   atomic.Int64
		entered = make(chan struct{})
		release = make(chan struct{})
	)
	src := func(context.Context, string) (*string, error) {
		n := calls.Add(1)
		if n == 1 {
			close(entered)
			<-release
			return nil, nil // 首查：数据不存在
		}
		v := "new" // 失效后重查：数据出现了
		return &v, nil
	}
	inner := NewLocalCache(LocalConfig[string]{TTL: time.Minute, NullTTL: time.Minute, Source: src})
	t.Cleanup(func() { _ = inner.Close(context.Background()) })
	sf := NewSingleFlightCache[string](inner)
	ctx := context.Background()

	done := make(chan loadResult, 1)
	go func() {
		v, err := sf.GetOrLoad(ctx, "k")
		done <- loadResult{v, err}
	}()

	<-entered
	if err := sf.Del(ctx, "k"); err != nil {
		t.Fatalf("Del: %v", err)
	}
	close(release)
	if r := <-done; r.err != nil || r.v != nil {
		t.Fatalf("首查无真值应返回 (nil, nil), got (%v, %v)", r.v, r.err)
	}

	// null marker 不得回填 ⇒ 下一次读重新回源拿新值（而不是被 marker
	// 挡一个 NullTTL 窗口）
	v, err := sf.GetOrLoad(ctx, "k")
	if err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	if v == nil || *v != "new" {
		t.Fatalf("Del 后的下一次读应回源拿新值, got %v", v)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("null marker 不得回填 ⇒ 应回源 2 次, got %d", got)
	}
}
