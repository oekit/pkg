package container

import "math"

// Ring 定长环形缓冲（非并发安全）：生产快于消费最多保留最新 n 个元素，
// 超出后 Add 覆盖最旧值且保序 FIFO（有界有损，适合"只要最新 n 条"的场景，
// 如最近请求采样、滑动日志）。
//
// 逻辑序归基（pkg#354）：read/write 是只增的逻辑序，32 位平台 int 在高频
// 写入下数小时即耗尽 int32、回绕成负数，负数取模得负下标直接 panic。Add
// 在 write 逼近字长上限前把两个计数器同时减去 read 的最大 n 整数倍基点
// ——槽位映射（x%n）对减去 n 的整数倍不变，物理元素无需搬动；归基后
// read 落回 [0, n)、write 落回不超过积压+n，32 位平台可长驻。
type Ring[T any] struct {
	elements []T
	read     int // 下一个待取下标（逻辑序）
	write    int // 下一个写入下标（逻辑序）
	n        int
}

// ringRenormThreshold 逻辑序归基触发点：write 达到 math.MaxInt/2 即归基。
// 32 位平台触发于 2^30 次写入（10 万/s 约 3 小时一次，代价是两次减法）；
// 64 位平台为 2^61 次，实际不可达。
const ringRenormThreshold = math.MaxInt / 2

// NewRing 创建容量为 n 的环形缓冲。n <= 0 属于编程错误，直接 panic。
func NewRing[T any](n int) *Ring[T] {
	if n <= 0 {
		panic("container: ring size must be positive")
	}
	return &Ring[T]{elements: make([]T, n), n: n}
}

// renormalize 把 read/write 归基到 0<=read<n：减去 read 的最大 n 整数倍基点。
// 两个计数器必须同时减——Len 与 FIFO 序都依赖 write-read 的差值不变。
func (r *Ring[T]) renormalize() {
	if r.write < ringRenormThreshold {
		return
	}
	base := r.read - r.read%r.n // n 的整数倍：减去后槽位映射（x%n）不变
	r.read -= base              // 归基后 read ∈ [0, n)
	r.write -= base             // 归基后 write ≤ 积压 + n，仍 ≥ read
}

// Add 写入元素；缓冲已满时覆盖最旧值并前移读指针——保序 FIFO，
// 任何时刻 Take 得到的都是最新的至多 n 条（最旧值静默丢失）。
func (r *Ring[T]) Add(v T) {
	r.renormalize() // 必须在 write 自增前：回绕成负数后取模即负下标（pkg#354）
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
