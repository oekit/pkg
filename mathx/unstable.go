package mathx

import (
	"math/rand"
	"time"
)

// Unstable 围绕基准值产生 ±deviation 比例的随机抖动。
// 典型用途是打散缓存 TTL：同批写入的 key 若 TTL 完全一致，
// 会形成周期性的集中失效（缓存雪崩），用抖动值错开即可缓解。
//
//	u := mathx.NewUnstable(0.1)
//	ttl := u.AroundDuration(30 * time.Minute) // ∈ [27min, 33min]
//
// 零值不可直接使用，经 NewUnstable 创建（deviation 夹到 [0,1]）。
type Unstable struct {
	deviation float64
}

// NewUnstable 创建抖动器。deviation 夹到 [0,1]：负值取 0，大于 1 取 1
// （deviation>1 会让结果越过 0 甚至变号，对 TTL/数量类语义无意义）。
func NewUnstable(deviation float64) Unstable {
	if deviation < 0 {
		deviation = 0
	} else if deviation > 1 {
		deviation = 1
	}
	return Unstable{deviation: deviation}
}

// Around 返回 n ∈ [n(1-dev), n(1+dev)] 的随机整数。
// deviation=0 恒等返回。适用于 TTL、数量等正数语义。
func (u Unstable) Around(n int) int {
	if u.deviation == 0 {
		return n
	}
	return int(u.AroundFloat64(float64(n)))
}

// AroundDuration 返回 d ∈ [d(1-dev), d(1+dev)] 的随机时长。
// deviation=0 恒等返回。
func (u Unstable) AroundDuration(d time.Duration) time.Duration {
	if u.deviation == 0 {
		return d
	}
	return time.Duration(u.AroundFloat64(float64(d)))
}

// AroundFloat64 返回 v ∈ [v(1-dev), v(1+dev)] 的随机浮点数。
// deviation=0 恒等返回。
func (u Unstable) AroundFloat64(v float64) float64 {
	if u.deviation == 0 {
		return v
	}
	return v * (1 + u.deviation*(rand.Float64()*2-1))
}
