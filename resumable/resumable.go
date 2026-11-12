package resumable

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

// 状态机与存储面的哨兵错误（errors.Is 可判）。
var (
	// ErrSessionNotFound 会话不存在。
	ErrSessionNotFound = errors.New("resumable: 会话不存在")
	// ErrSessionExists 会话 id 已存在（Create 撞 id）。
	ErrSessionExists = errors.New("resumable: 会话已存在")
	// ErrChunkNotFound 分片不存在（Stat/Open 未命中）。
	ErrChunkNotFound = errors.New("resumable: 分片不存在")
	// ErrSessionDone 会话已完成——完成态不可再收片/合并/中止。
	ErrSessionDone = errors.New("resumable: 会话已完成")
	// ErrSessionAborted 会话已中止。
	ErrSessionAborted = errors.New("resumable: 会话已中止")
	// ErrMergeInProgress 合并进行中：合并抢占的输家、或对合并中的会话
	// 收片/中止。输家可轮询 [Manager.ListParts] 的状态等赢家收场。
	ErrMergeInProgress = errors.New("resumable: 合并进行中")
	// ErrNotOwner 归属不符：Abort 的 owner 与会话归属不一致。
	ErrNotOwner = errors.New("resumable: 归属不符")
	// ErrBadChunkIndex 分片序号越界（不在 [0, NumChunks) 内）。
	ErrBadChunkIndex = errors.New("resumable: 分片序号越界")
	// ErrChunkSize 分片字节数与会话数学不符（偏短/残缺）。
	ErrChunkSize = errors.New("resumable: 分片字节数不符")
	// ErrChunkTooLarge 分片字节数超过上限（会话分片大小或调用方 maxChunk）。
	ErrChunkTooLarge = errors.New("resumable: 分片字节数超过上限")
	// ErrMissingChunks 完成时分片不齐（收到的缺清单见包装上下文）。
	ErrMissingChunks = errors.New("resumable: 分片不齐")
	// ErrBadInit 会话参数非法（Hash 空、Size 负、ChunkSize 非正等）。
	ErrBadInit = errors.New("resumable: 会话参数非法")
)

// Status 会话状态机的状态。状态迁移是单点语义（[SessionStore] 保证原子）：
//
//	StatusUploading --ClaimForMerge--> StatusMerging --Finish--> StatusDone
//	StatusUploading --Abandon--> StatusAborted
//	StatusMerging   --ReleaseClaim--> StatusUploading（校验/合并失败回滚）
type Status int

const (
	// StatusUploading 收片中：唯一可登记分片的状态。
	StatusUploading Status = iota
	// StatusMerging 合并中：已被 [SessionStore.ClaimForMerge] 抢占。
	StatusMerging
	// StatusDone 完成：合并已交付，终态。
	StatusDone
	// StatusAborted 中止：终态（可再清分片）。
	StatusAborted
)

// String 给日志/调试用。
func (s Status) String() string {
	switch s {
	case StatusUploading:
		return "uploading"
	case StatusMerging:
		return "merging"
	case StatusDone:
		return "done"
	case StatusAborted:
		return "aborted"
	}
	return fmt.Sprintf("status(%d)", int(s))
}

// Session 是一个上传会话的事实面（[SessionStore] 的读侧快照——真值在
// store，返回值是拷贝，改它不改 store）。
type Session struct {
	// ID 会话 id（Init 生成或调用方给定）。
	ID string
	// Owner 归属键（调用方语义：用户 id、租户键……），Abort 按它校验。
	Owner string
	// Hash 全量内容摘要（去重键候选，秒传钩子的入参）。
	Hash string
	// Size 全量字节数（>= 0；0 表示空对象，无分片）。
	Size int64
	// ChunkSize 分片大小（> 0）；末片可短于它。
	ChunkSize int64
	// Status 当前状态。
	Status Status
	// CreatedAt / UpdatedAt 会话建立 / 最后进展时刻（Sweep 的过期判据）。
	CreatedAt time.Time
	UpdatedAt time.Time
	// Received 是**已登记且存活确认过**的分片序号（升序快照）。
	// 断点对账的双源之一：状态是进度快照，字节是真值（见 ListParts）。
	Received []int
}

// NumChunks 返回分片总数（Size==0 时为 0）。
//
// 计算口径（#407）：`(Size-1)/ChunkSize + 1`——先加后除的写法
// `(Size+ChunkSize-1)/ChunkSize` 的**分子**在 Size 接近 MaxInt64 时先溢出
// 回绕到负半区 ⇒ 分片数 ≤ 0 ⇒ reconcile 的 `for i := range n` 空过（齐全
// 校验形同虚设）、merge 收到 0 字节流、Complete 返回 nil 并交付 0 字节对象。
// 先减后除数学等价（Size >= 1 时）且分子恒非负，不存在溢出形态。
//
// 结果按 int 表示宽度饱和（32 位平台上 int64 分片数可能超出 int 范围——
// 饱和到 MaxInt 而不是回绕出负数； Size/ChunkSize 组合超出 int 表示范围
// 本身就是不可执行的会话，reconcile/Complete 的逐片对账会如实把它算成
// 缺片，不会静默空过）。
func (s *Session) NumChunks() int {
	if s.Size == 0 || s.ChunkSize <= 0 {
		return 0
	}
	return int(min((s.Size-1)/s.ChunkSize+1, int64(math.MaxInt)))
}

// chunkLen 返回 idx 号分片的应有字节数（前置：idx 合法）——非末片是
// ChunkSize、末片是余数，一个 min() 说完。
func (s *Session) chunkLen(idx int) int64 {
	return min(s.ChunkSize, s.Size-int64(idx)*s.ChunkSize)
}

// clone 深拷贝快照（store 读侧纪律：返回值与内部状态零共享）。
func (s *Session) clone() *Session {
	c := *s
	if s.Received != nil {
		c.Received = append([]int(nil), s.Received...)
	}
	return &c
}

// InitReq 建会话的请求。
type InitReq struct {
	// ID 会话 id；空则由 [Manager.Init] 生成（crypto/rand 16 字节 hex）。
	ID string
	// Owner 归属键（必填语义由调用方定，Abort/Sweep 靠它分清「谁的会话」）。
	Owner string
	// Hash 全量内容摘要——秒传钩子的去重键（必填非空）。
	Hash string
	// Size 全量字节数（>= 0）。
	Size int64
	// ChunkSize 分片大小（> 0）。
	ChunkSize int64
}

// DedupResolver 是秒传钩子：内容摘要已存在（对象已完整落库）返回 true，
// Init 即回 (nil, true, nil)——不再建会话、不再收片。业务差异在去重键语义
// （按 hash、按 hash+size、按租户隔离……），所以做成钩子而不是内置规则。
type DedupResolver func(ctx context.Context, hash string) (bool, error)

// MergeFunc 是合并落存储的钩子：r 为**全部分片按序号拼接**的字节流
// （流式打开，不整对象进内存），把 r 写进业务存储即完成交付。合并成功后
// Manager 负责 Finish 与分片回收；merge 返回 error 时会话回滚回收片态
// （ReleaseClaim），调用方可修正后重试 Complete。业务差异只在落哪（对象
// 存储、文件、去重库……），并发正确性（谁来合并、缺片不合并、孤儿收口）
// 由状态机单点兜住。
type MergeFunc func(ctx context.Context, sess *Session, r io.Reader) error

// ChunkStore 是分片字节的存储后端（接口化：本地盘、对象存储、内存皆可）。
// 实现契约：
//   - Put 以写代换（同 id+idx 整体覆盖，不追加）；
//   - Stat 回已写字节数，缺失报 [ErrChunkNotFound]（实现的读侧错误经
//     errors.Is 可判这个哨兵，其他错误原样上抛——「读不到」与「没有」
//     必须可分）；
//   - Open 回已写字节的读侧（缺失报 [ErrChunkNotFound]），调用方负责 Close；
//   - DeletePrefix 删掉该会话的全部分片，**幂等**（没有即成功）——
//     孤儿清扫靠它收口。
type ChunkStore interface {
	Put(ctx context.Context, id string, idx int, r io.Reader) error
	Stat(ctx context.Context, id string, idx int) (int64, error)
	Open(ctx context.Context, id string, idx int) (io.ReadCloser, error)
	DeletePrefix(ctx context.Context, id string) error
}

// SessionStore 是会话状态机的存储后端。实现契约（并发正确性的四类坑都
// 压在这份接口上，实现必须单点兜住）：
//   - **CAS 进度**：AppendReceived 只在 [StatusUploading] 态登记、已登记
//     序号幂等；并发登记不丢、不重；
//   - **合并抢占**：ClaimForMerge 的 Uploading→[StatusMerging] 是原子
//     0→1 迁移，恰好一个赢家（输家拿 false）；
//   - **状态单向**：Finish 只从 Merging→[StatusDone]；ReleaseClaim 只从
//     Merging→Uploading（合并失败回滚，重试不换会话）；Abandon 把非终态
//     打到 [StatusAborted]（Aborted 幂等，Done 不可中止）；
//   - **读侧快照**：Get/List 返回深拷贝，调用方改写不污染内部状态。
type SessionStore interface {
	Create(ctx context.Context, s *Session) error
	Get(ctx context.Context, id string) (*Session, error)
	List(ctx context.Context) ([]*Session, error)
	AppendReceived(ctx context.Context, id string, idx []int) error
	ClaimForMerge(ctx context.Context, id string) (bool, error)
	ReleaseClaim(ctx context.Context, id string) error
	Finish(ctx context.Context, id string) error
	Abandon(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
}

// Manager 把状态机与两类存储串成上传会话面：会话创建、分片登记、断点
// 查询、完成、中止、孤儿清扫。构造后并发安全（互斥在两个 store 里）。
type Manager struct {
	sessions SessionStore
	chunks   ChunkStore
}

// NewManager 构造 Manager。任一 store 为 nil 立即 panic（仓规总则①：
// 构造期非法值 fail-fast——nil store 到第一个调用点才炸就晚了）。
func NewManager(sessions SessionStore, chunks ChunkStore) *Manager {
	if sessions == nil || chunks == nil {
		panic("resumable: NewManager: store 不可为 nil")
	}
	return &Manager{sessions: sessions, chunks: chunks}
}

// Init 建会话（或命中秒传）：
//
//	dedup 命中 → (nil, true, nil)：内容已存在，调用方直接复用既有对象；
//	否则按 req 建会话（StatusUploading）并返回 (会话, false, nil)。
//
// req.ID 空则生成；Hash 空、Size<0、ChunkSize<=0 报 [ErrBadInit]。
func (m *Manager) Init(ctx context.Context, req InitReq, dedup DedupResolver) (*Session, bool, error) {
	if req.Hash == "" || req.Size < 0 || req.ChunkSize <= 0 {
		return nil, false, fmt.Errorf("%w: Hash=%q Size=%d ChunkSize=%d", ErrBadInit, req.Hash, req.Size, req.ChunkSize)
	}
	if dedup != nil {
		hit, err := dedup(ctx, req.Hash)
		if err != nil {
			return nil, false, err
		}
		if hit {
			return nil, true, nil
		}
	}
	id := req.ID
	if id == "" {
		var err error
		if id, err = newSessionID(); err != nil {
			return nil, false, err
		}
	}
	now := time.Now()
	s := &Session{
		ID:        id,
		Owner:     req.Owner,
		Hash:      req.Hash,
		Size:      req.Size,
		ChunkSize: req.ChunkSize,
		Status:    StatusUploading,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := m.sessions.Create(ctx, s); err != nil {
		return nil, false, err
	}
	return s.clone(), false, nil
}

// PutChunk 登记一片（幂等分片 + CAS 进度 + 写后存活确认）：
//
//   - 只收 [StatusUploading] 态的会话（合并中/完成/中止各有各的哨兵）；
//   - idx 必须落在 [0, NumChunks)，否则 [ErrBadChunkIndex]；
//   - 字节数必须**恰好**是会话数学给的应有长度（非末片 = ChunkSize、
//     末片 = 余数）：超长报 [ErrChunkTooLarge]、偏短/残缺报 [ErrChunkSize]
//     ——宁可让客户端重传一片，也不让「大小不对的分片」流进合并；
//   - maxChunk 是调用方防线（客户端声称的分片大小不可信时的护栏）：
//     低于本片应有长度直接报 [ErrChunkTooLarge]，读取上限取会话数学；
//   - 写后存活确认：Put 之后 Stat 复核字节数才登记进度——「写了」不等于
//     「写住了」，断点对账把「登记了但字节丢了」的分片算进 missing。
func (m *Manager) PutChunk(ctx context.Context, id string, idx int, r io.Reader, maxChunk int64) error {
	if r == nil {
		return fmt.Errorf("resumable: PutChunk: nil reader")
	}
	sess, err := m.sessions.Get(ctx, id)
	if err != nil {
		return err
	}
	if upErr := uploadingOnly(sess); upErr != nil {
		return upErr
	}
	n := sess.NumChunks()
	if idx < 0 || idx >= n {
		return fmt.Errorf("%w: %d（共 %d 片）", ErrBadChunkIndex, idx, n)
	}
	want := sess.chunkLen(idx)
	if maxChunk < want {
		return fmt.Errorf("%w: 调用方上限 %d < 本片应有 %d", ErrChunkTooLarge, maxChunk, want)
	}
	// 流式写入：只读 want+1 字节，多一字节即超长（不整片进内存）。
	lr := &io.LimitedReader{R: r, N: want + 1}
	if putErr := m.chunks.Put(ctx, id, idx, lr); putErr != nil {
		return fmt.Errorf("resumable: 写分片 %d: %w", idx, putErr)
	}
	if lr.N == 0 {
		return fmt.Errorf("%w: 分片 %d 超过 %d 字节", ErrChunkTooLarge, idx, want)
	}
	size, err := m.chunks.Stat(ctx, id, idx)
	if err != nil {
		return fmt.Errorf("resumable: 分片 %d 写后存活确认: %w", idx, err)
	}
	if size != want {
		return fmt.Errorf("%w: 分片 %d 写 %d 字节，应有 %d", ErrChunkSize, idx, size, want)
	}
	return m.sessions.AppendReceived(ctx, id, []int{idx})
}

// Complete 完成会话：合并抢占 → 齐全校验 → merge 交付 → 收尾清分片。
//
//   - 合并抢占（0→1 原子）：并发 Complete 恰好一个赢家进合并，输家拿
//     [ErrMergeInProgress]（可轮询等赢家收场）；[StatusDone] 态再来是
//     幂等 nil，[StatusAborted] 态报 [ErrSessionAborted]；
//   - 齐全校验：逐片「已登记 ∧ 字节存活 ∧ 字节数吻合」双源对账，缺一片
//     都不合并——回滚回收片态（ReleaseClaim）并报 [ErrMissingChunks]，
//     客户端补传后可直接重试（会话不换）；
//   - merge 失败同样回滚回收片态（瞬态故障重试不丢已传分片）；
//   - merge 成功即 Finish（[StatusDone]）并删分片（合并结果已交付业务
//     存储，分片留着就是孤儿字节）；Finish 失败时会话留在合并态由
//     [Manager.Sweep] 收口（merge 已交付，不重跑）。
func (m *Manager) Complete(ctx context.Context, id string, merge MergeFunc) error {
	if merge == nil {
		return fmt.Errorf("resumable: Complete: nil MergeFunc")
	}
	sess, err := m.sessions.Get(ctx, id)
	if err != nil {
		return err
	}
	switch sess.Status {
	case StatusDone:
		return nil // 幂等完成
	case StatusAborted:
		return ErrSessionAborted
	case StatusMerging:
		return ErrMergeInProgress
	}
	won, err := m.sessions.ClaimForMerge(ctx, id)
	if err != nil {
		return err
	}
	if !won {
		return ErrMergeInProgress // 抢占输了：赢家正在合并
	}
	// 抢占后重新取快照：抢占前登记的最后一片以新快照为准。
	sess, err = m.sessions.Get(ctx, id)
	if err != nil {
		return errors.Join(err, m.sessions.ReleaseClaim(ctx, id))
	}

	received, missing, err := m.reconcile(ctx, sess)
	if err != nil {
		return errors.Join(err, m.sessions.ReleaseClaim(ctx, id))
	}
	if len(missing) > 0 {
		return errors.Join(
			fmt.Errorf("%w: 已收 %v，缺 %v", ErrMissingChunks, received, missing),
			m.sessions.ReleaseClaim(ctx, id))
	}

	cs := &chunkStream{ctx: ctx, store: m.chunks, id: id, n: sess.NumChunks()}
	if err := merge(ctx, sess.clone(), cs); err != nil {
		return errors.Join(
			fmt.Errorf("resumable: 合并交付失败: %w", err),
			cs.Close(),
			m.sessions.ReleaseClaim(ctx, id))
	}
	if err := cs.Close(); err != nil {
		// 交付已完成：不回滚（回滚会重跑 merge），错误如实上抛。
		return err
	}
	if err := m.sessions.Finish(ctx, id); err != nil {
		return err // 会话留在合并态，Sweep 收口（merge 已交付，不重跑）
	}
	return m.chunks.DeletePrefix(ctx, id)
}

// Abort 中止会话（归属校验）：校验 owner 后把会话打到 [StatusAborted]
// 并清掉全部分片。[StatusAborted] 态再来是幂等收口（再清一次分片）；
// [StatusDone] 不可中止（[ErrSessionDone]）；合并中不可中止
// （[ErrMergeInProgress]——等赢家收场再决定删不删）。
func (m *Manager) Abort(ctx context.Context, id string, owner string) error {
	sess, err := m.sessions.Get(ctx, id)
	if err != nil {
		return err
	}
	if sess.Owner != owner {
		return fmt.Errorf("%w: 会话 %s 属于 %q", ErrNotOwner, id, sess.Owner)
	}
	switch sess.Status {
	case StatusDone:
		return ErrSessionDone
	case StatusMerging:
		return ErrMergeInProgress
	case StatusUploading:
		if err := m.sessions.Abandon(ctx, id); err != nil {
			return err
		}
	}
	return m.chunks.DeletePrefix(ctx, id)
}

// ListParts 断点查询（续传对账）：返回已收/缺片双清单与会话状态。
//
// 对账口径是**双源取交**：「已登记 ∧ 字节存活 ∧ 字节数吻合」才算已收——
// 登记了但字节丢/残缺的分片必须回缺片清单（客户端重传），登记了却拿去
// 断点续传会在 Complete 才炸、把断点查询变成谎言。读不到（存储故障）不
// 归为缺片：错误原样上抛，不给客户端「重传能修好」的假答案。
func (m *Manager) ListParts(ctx context.Context, id string) (received, missing []int, status Status, err error) {
	sess, err := m.sessions.Get(ctx, id)
	if err != nil {
		return nil, nil, 0, err
	}
	received, missing, err = m.reconcile(ctx, sess)
	if err != nil {
		return nil, nil, sess.Status, err
	}
	return received, missing, sess.Status, nil
}

// Sweep 孤儿清扫的单点语义：把 UpdatedAt 早于 now-olderThan 的会话整批
// 收口——非终态先 Abandon、分片全删、记录删除（过期会话/无主分片/合并
// 卡死都归它）。返回成功清扫的会话数；失败的继续清扫、错误用 errors.Join
// 聚合上抛（清扫不因单个会话失败而中断）。olderThan < 0 报错。
func (m *Manager) Sweep(ctx context.Context, olderThan time.Duration) (int, error) {
	if olderThan < 0 {
		return 0, fmt.Errorf("resumable: Sweep: 负时长 %v", olderThan)
	}
	all, err := m.sessions.List(ctx)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-olderThan)
	var errs []error
	done := 0
	for _, s := range all {
		if !s.UpdatedAt.Before(cutoff) {
			continue
		}
		var one []error
		if s.Status != StatusDone && s.Status != StatusAborted {
			one = append(one, m.sessions.Abandon(ctx, s.ID))
		}
		one = append(one, m.chunks.DeletePrefix(ctx, s.ID), m.sessions.Delete(ctx, s.ID))
		sweepErr := errors.Join(one...)
		if sweepErr != nil {
			errs = append(errs, fmt.Errorf("resumable: 清扫会话 %s: %w", s.ID, sweepErr))
			continue
		}
		done++
	}
	return done, errors.Join(errs...)
}

// reconcile 做断点对账（双源取交）：idx 既在 sess.Received（登记）又被
// Stat 确认存活且字节数吻合 → received；其余 → missing。Stat 出非
// [ErrChunkNotFound] 的错误视为存储故障，原样上抛（不给假答案）。
func (m *Manager) reconcile(ctx context.Context, sess *Session) (received, missing []int, err error) {
	registered := make(map[int]bool, len(sess.Received))
	for _, i := range sess.Received {
		registered[i] = true
	}
	n := sess.NumChunks()
	for i := range n {
		want := sess.chunkLen(i)
		size, err := m.chunks.Stat(ctx, sess.ID, i)
		switch {
		case errors.Is(err, ErrChunkNotFound):
			missing = append(missing, i)
		case err != nil:
			return nil, nil, err
		case size != want:
			missing = append(missing, i) // 残缺片按缺处理（重传覆盖）
		case !registered[i]:
			missing = append(missing, i) // 字节在但没登记：不算已收（登记是收片成功的一部分）
		default:
			received = append(received, i)
		}
	}
	return received, missing, nil
}

// uploadingOnly 把非收片态翻译成对应哨兵（收片面的统一状态门）。
func uploadingOnly(sess *Session) error {
	switch sess.Status {
	case StatusUploading:
		return nil
	case StatusMerging:
		return ErrMergeInProgress
	case StatusDone:
		return ErrSessionDone
	}
	return ErrSessionAborted
}

// chunkStream 把全部分片按序号拼成一个流（merge 的入参）：逐片懒打开、
// 读尽即关、顺序推进，不整对象进内存。Close 关掉当前片（幂等）。
type chunkStream struct {
	ctx   context.Context
	store ChunkStore
	id    string
	idx   int
	n     int
	cur   io.ReadCloser
}

func (cs *chunkStream) Read(p []byte) (int, error) {
	for {
		if cs.cur == nil {
			if cs.idx >= cs.n {
				return 0, io.EOF
			}
			rc, err := cs.store.Open(cs.ctx, cs.id, cs.idx)
			if err != nil {
				return 0, err
			}
			cs.cur = rc
		}
		n, err := cs.cur.Read(p)
		if err == io.EOF {
			cerr := cs.cur.Close()
			cs.cur = nil
			cs.idx++
			if n > 0 {
				return n, cerr // 尾随错误下一轮再报
			}
			if cerr != nil {
				return 0, cerr
			}
			continue
		}
		return n, err
	}
}

// Close 收掉当前打开的分片（幂等）。
func (cs *chunkStream) Close() error {
	if cs.cur == nil {
		return nil
	}
	err := cs.cur.Close()
	cs.cur = nil
	return err
}

// newSessionID 生成会话 id（crypto/rand 16 字节 hex，32 字符）。
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("resumable: 生成会话 id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
