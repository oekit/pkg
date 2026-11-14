package sse

import (
	"context"
	"sync"
)

// Source 实时事件源抽象：订阅建立后发生的实时帧经返回的通道送达，
// 返回的取消函数退订（幂等）。宿主自带的跨进程总线实现本接口即可接入
// [NewResponder]；进程内场景直接用 [Hub]（实现本接口）。
type Source interface {
	Subscribe() (<-chan Event, func())
}

// DefaultSubscriberBuffer 每订阅者缓冲的缺省帧数。
const DefaultSubscriberBuffer = 64

// Hub 进程内实时面广播枢纽：宿主在事件落库成功后调用 [Hub.Emit]，
// 应答器经 [Hub.Subscribe]（[Source] 实现）接收实时帧。
//
// 广播不阻塞：慢消费者（缓冲满）被判定掉队、其流立即断开，客户端带
// Last-Event-ID 重连、由回放窗口补漏——见包文档「慢消费者自愈」。
// [Hub.Close] 关闭全部在订流并使 Emit 变为无操作（服务优雅停机时让
// SSE 长连接 orderly 结束）。
type Hub struct {
	mu        sync.RWMutex
	buf       int
	subs      map[*subscriber]struct{}
	closed    bool
	closeOnce sync.Once
}

// subscriber 一条在订流。close 用 Once 防双写 close（掉队断流与退订/
// Hub.Close 可能并发指向同一条流）。
type subscriber struct {
	ch        chan Event
	closeOnce sync.Once
}

func (s *subscriber) close() {
	s.closeOnce.Do(func() { close(s.ch) })
}

// HubOption Hub 的构造选项（仓规禁裸 Option）。
type HubOption func(*Hub)

// WithSubscriberBuffer 设置每订阅者缓冲帧数（缺省 [DefaultSubscriberBuffer]）。
// 非正数 panic（构造期 fail-fast，仓规总则①）。
func WithSubscriberBuffer(n int) HubOption {
	return func(h *Hub) {
		if n <= 0 {
			panic("sse: WithSubscriberBuffer 缓冲必须为正数（fail-fast，仓规总则①）")
		}
		h.buf = n
	}
}

// NewHub 建立广播枢纽。
func NewHub(opts ...HubOption) *Hub {
	h := &Hub{
		buf:  DefaultSubscriberBuffer,
		subs: make(map[*subscriber]struct{}),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Emit 向全部在订流广播一帧。不阻塞、不等待：某订阅者缓冲满即断其流
// （掉队自愈，见包文档）；Hub 已 Close 后 Emit 是无操作。掉队断流须把
// 订阅者从在订表摘除（摘除与遍历同表，须写锁——广播整帧持写锁）。
func (h *Hub) Emit(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for s := range h.subs {
		select {
		case s.ch <- e:
		default:
			s.close() // 缓冲满 = 掉队：断流触发客户端带游标重连补窗
			delete(h.subs, s)
		}
	}
}

// Subscribe 实现 [Source]：建立订阅。通道缓冲 [DefaultSubscriberBuffer]
// （或 [WithSubscriberBuffer] 覆盖），订阅建立后的 Emit 都会送达
// （缓冲满则断流）。
func (h *Hub) Subscribe() (<-chan Event, func()) {
	s := &subscriber{ch: make(chan Event, h.buf)}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		s.close()
		ch := s.ch
		return ch, func() {}
	}
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s.ch, func() { h.unsubscribe(s) }
}

// Subscribers 当前在订流数（观测/测试面）。
func (h *Hub) Subscribers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

// Close 关闭全部在订流（客户端看到流结束、按游标重连）并使 Emit 变为
// 无操作；幂等（sync.Once）。
func (h *Hub) Close(_ context.Context) error {
	h.closeOnce.Do(func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.closed = true
		for s := range h.subs {
			s.close()
		}
		h.subs = make(map[*subscriber]struct{})
	})
	return nil
}

func (h *Hub) unsubscribe(s *subscriber) {
	s.close()
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, s)
}
