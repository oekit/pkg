package mathx

import "math"

// WilsonCI 通过率的 Wilson 得分置信区间（小样本下比正态近似诚实）。
// 返回 (lo, hi)；total<=0 时返回 (0,0)；结果钳到 [0,1]。
//
// 计数越界（passed<0 或 passed>total）按钳到 [0,total] 处理：越界会让
// p 落到 [0,1] 之外 ⇒ p*(1-p)<0 ⇒ math.Sqrt(负数)=NaN ⇒ 返回 (NaN,NaN)
// （实测 WilsonCI(5,3,1.96)）。钳到合法区间后区间才有意义。
func WilsonCI(passed, total int, z float64) (lo, hi float64) {
	if total <= 0 {
		return 0, 0
	}
	if passed < 0 {
		passed = 0
	}
	if passed > total {
		passed = total
	}
	p := float64(passed) / float64(total)
	n := float64(total)
	denom := 1 + z*z/n
	center := (p + z*z/(2*n)) / denom
	half := z * math.Sqrt(p*(1-p)/n+z*z/(4*n*n)) / denom
	lo, hi = center-half, center+half
	if math.IsNaN(lo) || math.IsNaN(hi) {
		// z 为 NaN 等异常输入：区间无意义 ⇒ 返回零区间而不是 NaN。
		return 0, 0
	}
	if lo < 0 {
		lo = 0
	}
	if hi > 1 {
		hi = 1
	}
	return lo, hi
}

// McNemarExact McNemar 精确检验（双侧）：n01=甲败乙过数，n10=甲过乙败数。
// 返回 p 值；n01+n10=0（无任何翻转）返回 1（无差异）；p 钳到 1。
//
// 负数计数按 0 处理（调用方不该给出负计数，但本函数不返回 error，
// 不能让一个非法计数把结果变成「显著」）。
//
// 大 n 不再溢出：直接累加组合数 C(n,i) 会溢出（实测 C(1200,600) ≈ 1e360 >
// float64 上限）⇒ 累加和与 2^n 同时成 +Inf ⇒ Inf/Inf = NaN ⇒ 返回 NaN
// （实测 McNemarExact(600,600)）。改在**相对权重**空间递推（见
// binomHalfTail），全程不产生大数。
func McNemarExact(n01, n10 int) float64 {
	if n01 < 0 {
		n01 = 0
	}
	if n10 < 0 {
		n10 = 0
	}
	n := n01 + n10
	if n == 0 {
		return 1 // 无任何翻转，无差异
	}
	k := n01
	if n10 < k {
		k = n10
	}
	// p = 2 * P(X <= k)，X ~ Binomial(n, 1/2)
	p := 2 * binomHalfTail(n, k)
	if p > 1 {
		p = 1
	}
	if math.IsNaN(p) {
		return 1
	}
	return p
}

// binomHalfTail 返回 P(X <= k)，X ~ Binomial(n, 1/2)。
//
// 在**相对权重**空间递推：以众数 mode=floor(n/2) 的权重为 1，向两侧递推
//
//	w_{i-1} = w_i · i/(n-i+1)
//	w_{i+1} = w_i · (n-i)/(i+1)
//
// 全部权重之和就是 2^n / C(n,mode)（对 n=1200 约 10^2 量级，float64 完全
// 装得下），归一化后即得概率——全程不碰会溢出的组合数与 2^n。
//
// k >= mode 时用对称性换到下侧算：P(X<=k) = 1 - P(X>=k+1) = 1 - P(X<=n-k-1)，
// 下侧永远是大概率的一半，数值更稳。
func binomHalfTail(n, k int) float64 {
	if k < 0 {
		return 0
	}
	if k >= n {
		return 1
	}
	mode := n / 2

	// 全权重和（与下方 tail 用同一套相对权重）。
	total := 1.0
	w := 1.0
	for i := mode; i > 0; i-- {
		w = w * float64(i) / float64(n-i+1)
		total += w
	}
	w = 1.0
	for i := mode; i < n; i++ {
		w = w * float64(n-i) / float64(i+1)
		total += w
	}
	if total <= 0 {
		return 0
	}

	// lowerTail(kp) = P(X <= kp)，要求 kp <= mode：从众数向下走，只累加
	// 下标 <= kp 的权重。
	lowerTail := func(kp int) float64 {
		w := 1.0
		tail := 0.0
		for i := mode; i >= 0; i-- {
			if i <= kp {
				tail += w
			}
			if i == 0 {
				break
			}
			w = w * float64(i) / float64(n-i+1)
		}
		return tail / total
	}

	if k >= mode {
		return 1 - lowerTail(n-k-1)
	}
	return lowerTail(k)
}
