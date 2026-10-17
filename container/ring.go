package container

// Ring 定长环形缓冲（非并发安全）：生产快于消费最多保留最新 n 个元素，
// 超出后 Add 覆盖最旧值且保序 FIFO（有界有损，适合"只要最新 n 条"的场景，
// 如最近请求采样、滑动日志）。
type Ring[T any] struct {
	elements []T
	read     int // 下一个待取下标（逻辑序）
	write    int // 下一个写入下标（逻辑序）
	n        int
}

// NewRing 创建容量为 n 的环形缓冲。n <= 0 属于编程错误，直接 panic。
func NewRing[T any](n int) *Ring[T] {
	if n <= 0 {
		panic("container: ring size must be positive")
	}
	return &Ring[T]{elements: make([]T, n), n: n}
}

// Add 写入元素；缓冲已满时覆盖最旧值并前移读指针——保序 FIFO，
// 任何时刻 Take 得到的都是最新的至多 n 条（最旧值静默丢失）。
func (r *Ring[T]) Add(v T) {
	if r.write-r.read >= r.n {
		r.read++ // 满了：最旧值被覆盖，读指针同步前移保持 FIFO
	}
	r.elements[r.write%r.n] = v
	r.write++
}

// Take 按 FIFO 取出元素；已取空返回零值与 false。
func (r *Ring[T]) Take() (T, bool) {
	var zero T
	if r.read == r.write {
		return zero, false
	}
	v := r.elements[r.read%r.n]
	r.read++
	return v, true
}

// Len 返回当前积压未取的元素数（不会超过容量 n）。
func (r *Ring[T]) Len() int {
	return r.write - r.read
}
