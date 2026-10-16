package collectionx

import (
	"sync"
)

// Queue 无界阻塞 FIFO 队列（并发安全）：多生产者/多消费者，
// Take 阻塞直至有元素或队列关闭。Close 后 Put 直接 panic（与
// 向已关闭 channel 发送的语义一致）。
type Queue[T any] struct {
	mu       sync.Mutex
	notEmpty *sync.Cond
	items    []T
	head     int
	closed   bool
}

// NewQueue 创建空队列。
func NewQueue[T any]() *Queue[T] {
	q := &Queue[T]{}
	q.notEmpty = sync.NewCond(&q.mu)
	return q
}

// Put 入队；队列已关闭时 panic（编程错误，与 channel 语义一致）。
func (q *Queue[T]) Put(v T) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		panic("collectionx: put on closed queue")
	}
	q.items = append(q.items, v)
	q.notEmpty.Signal()
}

// Take 出队一个元素，阻塞直至有元素或队列关闭；
// 队列关闭且已取空返回零值与 false。
func (q *Queue[T]) Take() (T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == q.head && !q.closed {
		q.notEmpty.Wait()
	}
	return q.popLocked()
}

// TryTake 尝试出队，不阻塞；无元素或已关闭返回零值与 false。
func (q *Queue[T]) TryTake() (T, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.popLocked()
}

// Len 返回当前积压元素数。
func (q *Queue[T]) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items) - q.head
}

// Close 关闭队列：唤醒全部阻塞的 Take（取完余量后返回 false）；
// 幂等。关闭后 Put 会 panic。
func (q *Queue[T]) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	q.notEmpty.Broadcast()
}

// popLocked 出队一个元素（调用方持有锁）；空返回零值与 false。
// 头指针过半时收缩底层数组，避免长期运行下切片无限增长。
func (q *Queue[T]) popLocked() (T, bool) {
	var zero T
	if len(q.items) == q.head {
		return zero, false
	}
	v := q.items[q.head]
	q.items[q.head] = zero // 释放引用，防大对象滞留
	q.head++
	if q.head == len(q.items) {
		q.items = q.items[:0]
		q.head = 0
	} else if q.head > len(q.items)/2 {
		q.items = append(q.items[:0], q.items[q.head:]...)
		q.head = 0
	}
	return v, true
}
