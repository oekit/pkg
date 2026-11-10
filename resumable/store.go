package resumable

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"
)

// 两个内存后端是接口的参考实现（纯内存状态面）：单测、嵌入式与「存储
// 交给业务、状态机要个兜底」的场景直接用；持久化后端按 doc.go 的契约另接。

// sessionState 是 MemSessionStore 的内部态：会话 + 登记进度的真值集合。
type sessionState struct {
	sess     Session
	received map[int]bool
}

// MemSessionStore 是 [SessionStore] 的内存实现（并发安全）。
// CAS 进度 / 合并抢占 / 状态单向 / 读侧快照四条契约都在互斥里单点兑现。
type MemSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*sessionState
}

// NewMemSessionStore 构造空的内存会话存储。
func NewMemSessionStore() *MemSessionStore {
	return &MemSessionStore{sessions: make(map[string]*sessionState)}
}

var _ SessionStore = (*MemSessionStore)(nil)

// Create 登记新会话；id 重复报 [ErrSessionExists]。
func (m *MemSessionStore) Create(ctx context.Context, s *Session) error {
	if s == nil {
		return fmt.Errorf("resumable: Create: nil session")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[s.ID]; ok {
		return fmt.Errorf("%w: %s", ErrSessionExists, s.ID)
	}
	m.sessions[s.ID] = &sessionState{sess: *s.clone(), received: make(map[int]bool)}
	return nil
}

// Get 回会话快照（深拷贝）；缺失报 [ErrSessionNotFound]。
func (m *MemSessionStore) Get(ctx context.Context, id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	s := st.sess.clone()
	s.Received = st.receivedSorted()
	return s, nil
}

// List 回全部会话快照（按 id 升序，确定性输出）。
func (m *MemSessionStore) List(ctx context.Context) ([]*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := make([]*Session, 0, len(ids))
	for _, id := range ids {
		st := m.sessions[id]
		s := st.sess.clone()
		s.Received = st.receivedSorted()
		out = append(out, s)
	}
	return out, nil
}

// AppendReceived 登记分片进度（CAS 面）：只在 [StatusUploading] 态收登记，
// 已登记序号幂等；其他状态按对应哨兵报错（登记与收片状态强绑定——
// 合并中/完成后再登记就是状态机破口）。
func (m *MemSessionStore) AppendReceived(ctx context.Context, id string, idx []int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	switch st.sess.Status {
	case StatusUploading:
	case StatusMerging:
		return ErrMergeInProgress
	case StatusDone:
		return ErrSessionDone
	default:
		return ErrSessionAborted
	}
	st.sess.UpdatedAt = time.Now()
	for _, i := range idx {
		st.received[i] = true
	}
	return nil
}

// ClaimForMerge 原子抢占（0→1）：[StatusUploading] → [StatusMerging] 回
// true；其余状态回 false（幂等读，不报错——输家的语义就是「没抢到」）。
func (m *MemSessionStore) ClaimForMerge(ctx context.Context, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.sessions[id]
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	if st.sess.Status != StatusUploading {
		return false, nil
	}
	st.sess.Status = StatusMerging
	st.sess.UpdatedAt = time.Now()
	return true, nil
}

// ReleaseClaim 回滚合并抢占：[StatusMerging] → [StatusUploading]；其余状态
// 报错（回滚只对「抢了没交」的会话有意义）。
func (m *MemSessionStore) ReleaseClaim(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	if st.sess.Status != StatusMerging {
		return fmt.Errorf("resumable: ReleaseClaim: 会话 %s 状态 %s，不在合并态", id, st.sess.Status)
	}
	st.sess.Status = StatusUploading
	st.sess.UpdatedAt = time.Now()
	return nil
}

// Finish 收场：[StatusMerging] → [StatusDone]；其余状态报错。
func (m *MemSessionStore) Finish(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	if st.sess.Status != StatusMerging {
		return fmt.Errorf("resumable: Finish: 会话 %s 状态 %s，不在合并态", id, st.sess.Status)
	}
	st.sess.Status = StatusDone
	st.sess.UpdatedAt = time.Now()
	return nil
}

// Abandon 中止：非终态 → [StatusAborted]；[StatusAborted] 幂等；
// [StatusDone] 不可中止（报错）。
func (m *MemSessionStore) Abandon(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	switch st.sess.Status {
	case StatusDone:
		return ErrSessionDone
	case StatusAborted:
		return nil
	}
	st.sess.Status = StatusAborted
	st.sess.UpdatedAt = time.Now()
	return nil
}

// Delete 删会话记录（含进度）；缺失报 [ErrSessionNotFound]（清扫面把
// 「没删到」当真实失败，不吞）。
func (m *MemSessionStore) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[id]; !ok {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	delete(m.sessions, id)
	return nil
}

// receivedSorted 回登记进度的升序快照。
func (st *sessionState) receivedSorted() []int {
	if len(st.received) == 0 {
		return nil
	}
	out := make([]int, 0, len(st.received))
	for i := range st.received {
		out = append(out, i)
	}
	slices.Sort(out)
	return out
}

// MemChunkStore 是 [ChunkStore] 的内存实现（并发安全）。分片整块驻留
// 内存——流式上限由 [Manager.PutChunk] 的会话数学+maxChunk 把门，
// 直接拿本类型当裸存储用的调用方自限。同 id+idx 以写代换；Open 返回的
// 读器基于不可变字节块，写覆盖不干扰进行中的读。
type MemChunkStore struct {
	mu    sync.Mutex
	parts map[string]map[int][]byte
}

// NewMemChunkStore 构造空的内存分片存储。
func NewMemChunkStore() *MemChunkStore {
	return &MemChunkStore{parts: make(map[string]map[int][]byte)}
}

var _ ChunkStore = (*MemChunkStore)(nil)

// Put 以写代换存一片（读尽 r）。
func (m *MemChunkStore) Put(ctx context.Context, id string, idx int, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	byID := m.parts[id]
	if byID == nil {
		byID = make(map[int][]byte)
		m.parts[id] = byID
	}
	byID[idx] = data
	return nil
}

// Stat 回分片字节数；缺失报 [ErrChunkNotFound]。
func (m *MemChunkStore) Stat(ctx context.Context, id string, idx int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.parts[id][idx]
	if !ok {
		return 0, fmt.Errorf("%w: %s#%d", ErrChunkNotFound, id, idx)
	}
	return int64(len(data)), nil
}

// Open 回分片读器；缺失报 [ErrChunkNotFound]。读器基于写入时的不可变
// 块（覆盖写生成新块，进行中的读不受影响）。
func (m *MemChunkStore) Open(ctx context.Context, id string, idx int) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.parts[id][idx]
	if !ok {
		return nil, fmt.Errorf("%w: %s#%d", ErrChunkNotFound, id, idx)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// DeletePrefix 删掉会话全部分片；没有即成功（幂等——孤儿清扫靠它收口）。
func (m *MemChunkStore) DeletePrefix(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.parts, id)
	return nil
}
