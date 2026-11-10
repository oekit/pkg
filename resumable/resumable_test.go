package resumable

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestManager 造一对内存后端的 Manager。
func newTestManager() (*Manager, *MemSessionStore, *MemChunkStore) {
	sessions := NewMemSessionStore()
	chunks := NewMemChunkStore()
	return NewManager(sessions, chunks), sessions, chunks
}

// initSession 建一个默认会话：10 字节、4 字节一片 ⇒ 3 片（4/4/2）。
func initSession(t *testing.T, m *Manager) *Session {
	t.Helper()
	s, uploaded, err := m.Init(context.Background(), InitReq{
		Owner: "u1", Hash: "h1", Size: 10, ChunkSize: 4,
	}, nil)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if uploaded {
		t.Fatalf("Init: unexpected 秒传命中")
	}
	return s
}

// fillAll 传满 3 片（4/4/2）。
func fillAll(t *testing.T, m *Manager, id string) {
	t.Helper()
	for i, data := range []string{"AAAA", "BBBB", "cc"} {
		if err := m.PutChunk(context.Background(), id, i, strings.NewReader(data), 4); err != nil {
			t.Fatalf("PutChunk %d: %v", i, err)
		}
	}
}

func TestNewManagerNilPanics(t *testing.T) {
	for _, tc := range []struct {
		s SessionStore
		c ChunkStore
	}{
		{nil, NewMemChunkStore()},
		{NewMemSessionStore(), nil},
		{nil, nil},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("NewManager(%v,%v) should panic", tc.s, tc.c)
				}
			}()
			NewManager(tc.s, tc.c)
		}()
	}
}

func TestInit(t *testing.T) {
	ctx := context.Background()
	m, _, _ := newTestManager()

	// 正常建立：字段就位、状态收片中、id 生成。
	s, uploaded, err := m.Init(ctx, InitReq{Owner: "u1", Hash: "h", Size: 10, ChunkSize: 4}, nil)
	if err != nil || uploaded {
		t.Fatalf("Init = %v, %v, %v", s, uploaded, err)
	}
	if s.ID == "" || s.Status != StatusUploading || s.NumChunks() != 3 {
		t.Fatalf("session = %+v", s)
	}
	if len(s.Received) != 0 || !s.CreatedAt.Equal(s.UpdatedAt) {
		t.Fatalf("session = %+v（初始无分片、建立即 UpdatedAt）", s)
	}

	// 显式 id / 空对象（Size 0，无分片）。
	s2, _, err := m.Init(ctx, InitReq{ID: "fixed", Owner: "u2", Hash: "h2", Size: 0, ChunkSize: 3}, nil)
	if err != nil || s2.ID != "fixed" || s2.NumChunks() != 0 {
		t.Fatalf("Init fixed = %+v, %v", s2, err)
	}

	// 参数非法（构造期 fail-fast 的错误路径）。
	for _, req := range []InitReq{
		{Owner: "u", Hash: "", Size: 1, ChunkSize: 1},
		{Owner: "u", Hash: "h", Size: -1, ChunkSize: 1},
		{Owner: "u", Hash: "h", Size: 1, ChunkSize: 0},
	} {
		if _, _, err := m.Init(ctx, req, nil); !errors.Is(err, ErrBadInit) {
			t.Errorf("Init(%+v) err = %v, want ErrBadInit", req, err)
		}
	}

	// 撞 id。
	if _, _, err := m.Init(ctx, InitReq{ID: "fixed", Hash: "h3", Size: 1, ChunkSize: 1}, nil); !errors.Is(err, ErrSessionExists) {
		t.Errorf("Init 撞 id err = %v, want ErrSessionExists", err)
	}
}

func TestInitDedup(t *testing.T) {
	ctx := context.Background()
	m, _, _ := newTestManager()

	// 秒传命中：不建会话。
	s, uploaded, err := m.Init(ctx, InitReq{Hash: "h", Size: 1, ChunkSize: 1}, func(context.Context, string) (bool, error) {
		return true, nil
	})
	if !uploaded || s != nil || err != nil {
		t.Fatalf("秒传 = (%v, %v, %v), want (nil, true, nil)", s, uploaded, err)
	}

	// 钩子错误原样上抛。
	want := errors.New("store down")
	if _, _, got := m.Init(ctx, InitReq{Hash: "h", Size: 1, ChunkSize: 1}, func(context.Context, string) (bool, error) {
		return false, want
	}); !errors.Is(got, want) {
		t.Fatalf("dedup err = %v, want %v", got, want)
	}

	// 未命中：建会话。
	s, uploaded, err = m.Init(ctx, InitReq{Hash: "h", Size: 1, ChunkSize: 1}, func(context.Context, string) (bool, error) {
		return false, nil
	})
	if err != nil || uploaded || s == nil {
		t.Fatalf("未命中 = (%v, %v, %v)", s, uploaded, err)
	}
}

func TestPutChunk(t *testing.T) {
	ctx := context.Background()
	m, _, _ := newTestManager()
	s := initSession(t, m) // 3 片：4/4/2

	// 满片（非末片恰好 ChunkSize）。
	if err := m.PutChunk(ctx, s.ID, 0, strings.NewReader("AAAA"), 4); err != nil {
		t.Fatalf("PutChunk: %v", err)
	}
	// 末片短于 ChunkSize。
	if err := m.PutChunk(ctx, s.ID, 2, strings.NewReader("cc"), 2); err != nil {
		t.Fatalf("PutChunk last: %v", err)
	}
	// 幂等重传（覆盖，进度不重）。
	if err := m.PutChunk(ctx, s.ID, 0, strings.NewReader("AAAA"), 4); err != nil {
		t.Fatalf("PutChunk 重传: %v", err)
	}
	got, _, _, err := m.ListParts(ctx, s.ID)
	if err != nil || !slices.Equal(got, []int{0, 2}) {
		t.Fatalf("ListParts received = %v, %v", got, err)
	}

	// 超长：want+1 字节即拒（且不登记）。
	if err := m.PutChunk(ctx, s.ID, 1, strings.NewReader("BBBBB"), 5); !errors.Is(err, ErrChunkTooLarge) {
		t.Fatalf("超长 err = %v, want ErrChunkTooLarge", err)
	}
	// 偏短：报 ErrChunkSize（写后存活确认）。
	if err := m.PutChunk(ctx, s.ID, 1, strings.NewReader("BB"), 4); !errors.Is(err, ErrChunkSize) {
		t.Fatalf("偏短 err = %v, want ErrChunkSize", err)
	}
	// 两种失败都不登记进度。
	if recv, _, _, _ := m.ListParts(ctx, s.ID); !slices.Equal(recv, []int{0, 2}) {
		t.Fatalf("失败分片不得登记: %v", recv)
	}

	// 序号越界 / 负数。
	for _, idx := range []int{3, -1, 99} {
		if err := m.PutChunk(ctx, s.ID, idx, strings.NewReader("AAAA"), 4); !errors.Is(err, ErrBadChunkIndex) {
			t.Errorf("idx %d err = %v, want ErrBadChunkIndex", idx, err)
		}
	}
	if err := m.PutChunk(ctx, s.ID, 0, nil, 4); err == nil {
		t.Errorf("nil reader = nil, want error")
	}

	// maxChunk 低于本片应有长度：调用方防线报错（不静默截断）。
	if err := m.PutChunk(ctx, s.ID, 1, strings.NewReader("BBBB"), 3); !errors.Is(err, ErrChunkTooLarge) {
		t.Fatalf("maxChunk 过低 err = %v, want ErrChunkTooLarge", err)
	}

	// 状态门：非收片态不收片。
	if cerr := m.sessions.Abandon(ctx, s.ID); cerr != nil {
		t.Fatal(cerr)
	}
	if err := m.PutChunk(ctx, s.ID, 1, strings.NewReader("BBBB"), 4); !errors.Is(err, ErrSessionAborted) {
		t.Fatalf("中止态收片 err = %v, want ErrSessionAborted", err)
	}
	// 不存在的会话。
	if err := m.PutChunk(ctx, "nope", 0, strings.NewReader("a"), 1); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("缺失会话 err = %v, want ErrSessionNotFound", err)
	}
}

func TestListPartsReconcile(t *testing.T) {
	ctx := context.Background()
	m, _, chunks := newTestManager()
	s := initSession(t, m)

	// 初始：全缺。
	recv, missing, st, err := m.ListParts(ctx, s.ID)
	if err != nil || len(recv) != 0 || !slices.Equal(missing, []int{0, 1, 2}) || st != StatusUploading {
		t.Fatalf("初始 = %v %v %v %v", recv, missing, st, err)
	}

	// 传 0、2；随后字节被偷走（模拟存储丢件）——登记了但字节丢的分片
	// 必须回缺片清单，否则断点查询会把客户端骗去 Complete 才炸。
	for i, data := range map[int]string{0: "AAAA", 2: "cc"} {
		if cerr := m.PutChunk(ctx, s.ID, i, strings.NewReader(data), 4); cerr != nil {
			t.Fatal(cerr)
		}
	}
	if cerr := chunks.DeletePrefix(ctx, s.ID); cerr != nil {
		t.Fatal(cerr)
	}
	if cerr := m.PutChunk(ctx, s.ID, 0, strings.NewReader("AAAA"), 4); cerr != nil { // 只补回 0
		t.Fatal(cerr)
	}
	recv, missing, _, err = m.ListParts(ctx, s.ID)
	if err != nil || !slices.Equal(recv, []int{0}) || !slices.Equal(missing, []int{1, 2}) {
		t.Fatalf("对账 = %v %v %v", recv, missing, err)
	}

	// 字节在但没登记（写后登记前崩溃）：不算已收（登记是收片成功的一部分）。
	if cerr := chunks.Put(ctx, s.ID, 1, strings.NewReader("BBBB")); cerr != nil {
		t.Fatal(cerr)
	}
	recv, missing, _, err = m.ListParts(ctx, s.ID)
	if err != nil || !slices.Equal(recv, []int{0}) || !slices.Equal(missing, []int{1, 2}) {
		t.Fatalf("未登记不算已收 = %v %v %v", recv, missing, err)
	}
}

// brokenStatStore 注入 Stat 故障，验证「读不到 ≠ 没有」。
type brokenStatStore struct {
	*MemChunkStore
	statErr error
}

func (b *brokenStatStore) Stat(ctx context.Context, id string, idx int) (int64, error) {
	if b.statErr != nil {
		return 0, b.statErr
	}
	return b.MemChunkStore.Stat(ctx, id, idx)
}

func TestListPartsStoreError(t *testing.T) {
	ctx := context.Background()
	sessions := NewMemSessionStore()
	store := &brokenStatStore{MemChunkStore: NewMemChunkStore(), statErr: errors.New("disk on fire")}
	m := NewManager(sessions, store)
	s := initSession(t, m)

	_, _, _, err := m.ListParts(ctx, s.ID)
	if err == nil || errors.Is(err, ErrChunkNotFound) {
		t.Fatalf("存储故障 err = %v, want 原样上抛（不给「重传能修好」的假答案）", err)
	}
}

func TestComplete(t *testing.T) {
	ctx := context.Background()
	m, _, chunks := newTestManager()
	s := initSession(t, m)
	fillAll(t, m, s.ID)

	var merged bytes.Buffer
	var gotSess *Session
	err := m.Complete(ctx, s.ID, func(_ context.Context, sess *Session, r io.Reader) error {
		gotSess = sess
		_, err := io.Copy(&merged, r)
		return err
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if merged.String() != "AAAABBBBcc" {
		t.Fatalf("合并字节 = %q, want 按序拼接", merged.String())
	}
	if gotSess.Status != StatusMerging || gotSess.ID != s.ID {
		t.Fatalf("merge 收到的会话 = %+v", gotSess)
	}

	// 终态就位、分片已清。
	_, _, st, err := m.ListParts(ctx, s.ID)
	if err != nil || st != StatusDone {
		t.Fatalf("完成后状态 = %v %v", st, err)
	}
	if _, err := chunks.Stat(ctx, s.ID, 0); !errors.Is(err, ErrChunkNotFound) {
		t.Fatalf("完成后分片未清: %v", err)
	}

	// 幂等完成。
	var called bool
	if err := m.Complete(ctx, s.ID, func(context.Context, *Session, io.Reader) error {
		called = true
		return nil
	}); err != nil || called {
		t.Fatalf("幂等 Complete = %v (merge 被调: %v)", err, called)
	}
}

func TestCompleteMissingChunks(t *testing.T) {
	ctx := context.Background()
	m, _, _ := newTestManager()
	s := initSession(t, m)
	_ = m.PutChunk(ctx, s.ID, 0, strings.NewReader("AAAA"), 4)
	// 缺 1、2：不合并、抢占回滚。
	err := m.Complete(ctx, s.ID, func(context.Context, *Session, io.Reader) error {
		t.Error("缺片不得进 merge")
		return nil
	})
	if !errors.Is(err, ErrMissingChunks) {
		t.Fatalf("缺片 Complete err = %v, want ErrMissingChunks", err)
	}

	// 回滚后可补传重试（会话不换）。
	if cerr := m.PutChunk(ctx, s.ID, 1, strings.NewReader("BBBB"), 4); cerr != nil {
		t.Fatal(cerr)
	}
	if cerr := m.PutChunk(ctx, s.ID, 2, strings.NewReader("cc"), 2); cerr != nil {
		t.Fatal(cerr)
	}
	if err := m.Complete(ctx, s.ID, func(context.Context, *Session, io.Reader) error { return nil }); err != nil {
		t.Fatalf("补传后 Complete: %v", err)
	}
}

func TestCompleteMergeErrorReleasesClaim(t *testing.T) {
	ctx := context.Background()
	m, _, _ := newTestManager()
	s := initSession(t, m)
	fillAll(t, m, s.ID)

	boom := errors.New("object store timeout")
	if err := m.Complete(ctx, s.ID, func(context.Context, *Session, io.Reader) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("merge 失败 err = %v, want %v", err, boom)
	}
	// 抢占回滚：状态回收片中，分片还在，重试即成功。
	_, _, st, err := m.ListParts(ctx, s.ID)
	if err != nil || st != StatusUploading {
		t.Fatalf("回滚后状态 = %v %v", st, err)
	}
	n := 0
	if err := m.Complete(ctx, s.ID, func(context.Context, *Session, io.Reader) error { n++; return nil }); err != nil || n != 1 {
		t.Fatalf("重试 Complete = %v (merge 次数 %d)", err, n)
	}
}

// TestCompleteConcurrentClaim 钉住合并抢占：并发 Complete 恰好一个赢家。
func TestCompleteConcurrentClaim(t *testing.T) {
	ctx := context.Background()
	m, _, _ := newTestManager()
	s := initSession(t, m)
	fillAll(t, m, s.ID)

	const workers = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	merges := 0
	results := make([]error, workers)
	start := make(chan struct{})
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = m.Complete(ctx, s.ID, func(context.Context, *Session, io.Reader) error {
				mu.Lock()
				merges++
				mu.Unlock()
				time.Sleep(20 * time.Millisecond) // 拉长合并窗口，逼出并发对局
				return nil
			})
		}(i)
	}
	close(start)
	wg.Wait()

	if merges != 1 {
		t.Fatalf("merge 被调 %d 次, want 恰好 1 次", merges)
	}
	winners := 0
	for _, err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, ErrMergeInProgress):
		default:
			t.Errorf("并发 Complete err = %v, want nil 或 ErrMergeInProgress", err)
		}
	}
	if winners == 0 {
		t.Errorf("没有赢家")
	}
}

// TestPutChunkConcurrentProgress 钉住 CAS 进度：并发登记不丢不重
// （go test -race 下跑）。
func TestPutChunkConcurrentProgress(t *testing.T) {
	ctx := context.Background()
	m, _, _ := newTestManager()
	const size, chunk = 100, 1
	s, _, err := m.Init(ctx, InitReq{Hash: "h", Size: size, ChunkSize: chunk}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range size {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// 同片并发双写：幂等覆盖 + 进度去重。
			_ = m.PutChunk(ctx, s.ID, i, strings.NewReader("x"), chunk)
			_ = m.PutChunk(ctx, s.ID, i, strings.NewReader("x"), chunk)
		}(i)
	}
	wg.Wait()
	recv, missing, _, err := m.ListParts(ctx, s.ID)
	if err != nil || len(recv) != size || len(missing) != 0 {
		t.Fatalf("并发登记后 = %d 收，缺 %v (%v)", len(recv), missing, err)
	}
}

func TestAbort(t *testing.T) {
	ctx := context.Background()
	m, _, chunks := newTestManager()
	s := initSession(t, m)
	_ = m.PutChunk(ctx, s.ID, 0, strings.NewReader("AAAA"), 4)

	// 归属不符：不中止、不清分片。
	if err := m.Abort(ctx, s.ID, "someone-else"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("归属不符 err = %v, want ErrNotOwner", err)
	}
	if _, err := chunks.Stat(ctx, s.ID, 0); err != nil {
		t.Fatalf("归属不符不得清分片: %v", err)
	}

	// 正常中止：状态终态 + 分片清掉。
	if err := m.Abort(ctx, s.ID, "u1"); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	_, _, st, err := m.ListParts(ctx, s.ID)
	if err != nil || st != StatusAborted {
		t.Fatalf("中止后 = %v %v", st, err)
	}
	if _, err := chunks.Stat(ctx, s.ID, 0); !errors.Is(err, ErrChunkNotFound) {
		t.Fatalf("中止后分片未清: %v", err)
	}

	// 幂等中止（再收口一次分片也无妨）。
	if err := m.Abort(ctx, s.ID, "u1"); err != nil {
		t.Fatalf("幂等 Abort = %v", err)
	}

	// 完成态不可中止。
	s2 := initSession(t, m)
	fillAll(t, m, s2.ID)
	_ = m.Complete(ctx, s2.ID, func(context.Context, *Session, io.Reader) error { return nil })
	if err := m.Abort(ctx, s2.ID, "u1"); !errors.Is(err, ErrSessionDone) {
		t.Fatalf("完成态中止 err = %v, want ErrSessionDone", err)
	}

	// 合并中不可中止。
	s3 := initSession(t, m)
	fillAll(t, m, s3.ID)
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = m.Complete(ctx, s3.ID, func(context.Context, *Session, io.Reader) error {
			<-release
			return nil
		})
	}()
	// 等赢家进合并态。
	for {
		_, _, st, _ := m.ListParts(ctx, s3.ID)
		if st == StatusMerging {
			break
		}
		time.Sleep(time.Millisecond)
	}
	abortErr := m.Abort(ctx, s3.ID, "u1")
	close(release)
	<-done
	if !errors.Is(abortErr, ErrMergeInProgress) {
		t.Fatalf("合并中中止 err = %v, want ErrMergeInProgress", abortErr)
	}
}

func TestSweep(t *testing.T) {
	ctx := context.Background()
	m, sessions, chunks := newTestManager()

	// 三个会话：两个过期（一个收片中、一个已中止）、一个新鲜。
	old1 := initSession(t, m)
	old2 := initSession(t, m)
	fresh := initSession(t, m)
	for _, s := range []*Session{old1, old2} {
		_ = m.PutChunk(ctx, s.ID, 0, strings.NewReader("AAAA"), 4)
	}
	_ = m.sessions.Abandon(ctx, old2.ID)

	// 时钟：把两个会话的 UpdatedAt 拨老（模拟时间流逝）。
	sessions.mu.Lock()
	for _, id := range []string{old1.ID, old2.ID} {
		sessions.sessions[id].sess.UpdatedAt = time.Now().Add(-48 * time.Hour)
	}
	sessions.mu.Unlock()

	n, err := m.Sweep(ctx, 24*time.Hour)
	if err != nil || n != 2 {
		t.Fatalf("Sweep = (%d, %v), want (2, nil)", n, err)
	}
	for _, id := range []string{old1.ID, old2.ID} {
		if _, err := sessions.Get(ctx, id); !errors.Is(err, ErrSessionNotFound) {
			t.Errorf("过期会话 %s 仍在", id)
		}
		if _, err := chunks.Stat(ctx, id, 0); !errors.Is(err, ErrChunkNotFound) {
			t.Errorf("过期会话 %s 分片仍在", id)
		}
	}
	if _, _, st, err := m.ListParts(ctx, fresh.ID); err != nil || st != StatusUploading {
		t.Errorf("新鲜会话被动 = %v %v", st, err)
	}

	// 负时长：报错。
	if _, err := m.Sweep(ctx, -time.Second); err == nil {
		t.Errorf("Sweep(-1s) = nil, want error")
	}
}

func TestMemSessionStoreStateGates(t *testing.T) {
	ctx := context.Background()
	st := NewMemSessionStore()
	s := &Session{ID: "a", Hash: "h", Size: 4, ChunkSize: 2, Status: StatusUploading, CreatedAt: time.Now()}

	if cerr := st.Create(ctx, s); cerr != nil {
		t.Fatal(cerr)
	}
	if err := st.Create(ctx, s); !errors.Is(err, ErrSessionExists) {
		t.Errorf("重复 Create err = %v, want ErrSessionExists", err)
	}
	if cerr := st.AppendReceived(ctx, "a", []int{0, 0}); cerr != nil { // 幂等登记
		t.Fatal(cerr)
	}

	// 读侧快照隔离：改返回值不污染内部。
	got, err := st.Get(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	got.Received[0] = 99
	got.Status = StatusDone
	again, _ := st.Get(ctx, "a")
	if again.Received[0] != 0 || again.Status != StatusUploading {
		t.Errorf("快照被污染: %+v", again)
	}

	// 抢占：0→1 恰好一次。
	won, err := st.ClaimForMerge(ctx, "a")
	if err != nil || !won {
		t.Fatalf("ClaimForMerge = %v %v", won, err)
	}
	if won, _ := st.ClaimForMerge(ctx, "a"); won {
		t.Errorf("二次抢占 = true, want false")
	}
	// 状态门：合并中不登记；终态各有各的哨兵。
	if err := st.AppendReceived(ctx, "a", []int{1}); !errors.Is(err, ErrMergeInProgress) {
		t.Errorf("合并中登记 err = %v", err)
	}
	if cerr := st.Finish(ctx, "a"); cerr != nil {
		t.Fatal(cerr)
	}
	if err := st.Finish(ctx, "a"); err == nil {
		t.Errorf("二次 Finish = nil, want error")
	}
	if err := st.ReleaseClaim(ctx, "a"); err == nil {
		t.Errorf("完成态 ReleaseClaim = nil, want error")
	}
	if err := st.AppendReceived(ctx, "a", []int{1}); !errors.Is(err, ErrSessionDone) {
		t.Errorf("完成态登记 err = %v", err)
	}
	if err := st.Abandon(ctx, "a"); !errors.Is(err, ErrSessionDone) {
		t.Errorf("完成态中止 err = %v", err)
	}
	if cerr := st.Delete(ctx, "a"); cerr != nil {
		t.Fatal(cerr)
	}
	if err := st.Delete(ctx, "a"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("二次 Delete err = %v", err)
	}
}

func TestMemSessionStoreReleaseAndAbandon(t *testing.T) {
	ctx := context.Background()
	st := NewMemSessionStore()
	_ = st.Create(ctx, &Session{ID: "a", Status: StatusUploading})

	if err := st.ReleaseClaim(ctx, "a"); err == nil {
		t.Errorf("收片态 ReleaseClaim = nil, want error")
	}
	_, _ = st.ClaimForMerge(ctx, "a")
	if err := st.ReleaseClaim(ctx, "a"); err != nil {
		t.Errorf("合并态 ReleaseClaim = %v", err)
	}
	if won, _ := st.ClaimForMerge(ctx, "a"); !won {
		t.Errorf("回滚后应可再抢占")
	}
	if err := st.Abandon(ctx, "a"); err != nil { // 合并态可中止（store 面放行，Manager 面把关）
		t.Errorf("Abandon = %v", err)
	}
	if err := st.Abandon(ctx, "a"); err != nil { // 幂等
		t.Errorf("幂等 Abandon = %v", err)
	}
}

func TestMemChunkStore(t *testing.T) {
	ctx := context.Background()
	c := NewMemChunkStore()

	if cerr := c.Put(ctx, "a", 0, strings.NewReader("xyz")); cerr != nil {
		t.Fatal(cerr)
	}
	if size, err := c.Stat(ctx, "a", 0); err != nil || size != 3 {
		t.Fatalf("Stat = %d %v", size, err)
	}
	rc, err := c.Open(ctx, "a", 0)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(data) != "xyz" {
		t.Fatalf("Open = %q", data)
	}
	// 以写代换。
	_ = c.Put(ctx, "a", 0, strings.NewReader("X"))
	if size, _ := c.Stat(ctx, "a", 0); size != 1 {
		t.Fatalf("覆盖后 Stat = %d", size)
	}
	if _, err := c.Stat(ctx, "a", 1); !errors.Is(err, ErrChunkNotFound) {
		t.Errorf("缺失 Stat err = %v, want ErrChunkNotFound", err)
	}
	if _, err := c.Open(ctx, "a", 1); !errors.Is(err, ErrChunkNotFound) {
		t.Errorf("缺失 Open err = %v, want ErrChunkNotFound", err)
	}
	// DeletePrefix 幂等。
	if cerr := c.DeletePrefix(ctx, "a"); cerr != nil {
		t.Fatal(cerr)
	}
	if err := c.DeletePrefix(ctx, "a"); err != nil {
		t.Errorf("幂等 DeletePrefix = %v", err)
	}
}

func TestStatusString(t *testing.T) {
	for st, want := range map[Status]string{
		StatusUploading: "uploading",
		StatusMerging:   "merging",
		StatusDone:      "done",
		StatusAborted:   "aborted",
		Status(42):      "status(42)",
	} {
		if got := st.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", st, got, want)
		}
	}
}

// TestChunkStreamEmpty 钉住 merge 入参的空会话口径：0 分片 = 空流。
func TestChunkStreamEmpty(t *testing.T) {
	ctx := context.Background()
	m, _, _ := newTestManager()
	s, _, err := m.Init(ctx, InitReq{Hash: "h", Size: 0, ChunkSize: 3}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var merged bytes.Buffer
	if err := m.Complete(ctx, s.ID, func(_ context.Context, _ *Session, r io.Reader) error {
		_, err := io.Copy(&merged, r)
		return err
	}); err != nil {
		t.Fatalf("空对象 Complete: %v", err)
	}
	if merged.Len() != 0 {
		t.Fatalf("空对象合并出 %q", merged.String())
	}
}

// TestPutChunkStatError 存储写入故障要如实上抛（不吞成进度）。
func TestPutChunkStatError(t *testing.T) {
	ctx := context.Background()
	store := &brokenStatStore{MemChunkStore: NewMemChunkStore()}
	m := NewManager(NewMemSessionStore(), store)
	s := initSession(t, m)

	boom := fmt.Errorf("stat boom")
	store.statErr = boom
	err := m.PutChunk(ctx, s.ID, 0, strings.NewReader("AAAA"), 4)
	if !errors.Is(err, boom) {
		t.Fatalf("PutChunk Stat 故障 err = %v, want 上抛 %v", err, boom)
	}
}
