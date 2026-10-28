package mathx

import (
	"cmp"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"sync"
	"time"
)

// rng 是包级随机数生成器，避免每次调用分配新实例。
var (
	rng   = rand.New(rand.NewSource(time.Now().UnixNano()))
	rngMu sync.Mutex
)

// RandIntn 返回 [0, maxInt) 范围内的随机整数。
// 并发安全，使用包级 rng 避免同纳秒碰撞。
func RandIntn(maxInt int) int {
	rngMu.Lock()
	v := rng.Intn(maxInt)
	rngMu.Unlock()
	return v
}

// DivisionN 执行除法并保留 n 位小数（截断，非四舍五入）。
//
// 截断用 math.Trunc 而非 int64 转换：int64(x) 在 x 超出 int64 范围时**回绕**
// （Go 规范未定义该情形，amd64/arm64 实测得到 -9223372036854775808）⇒
// DivisionN(1e18, 1, 6) 实得 9.22e12（期望 1e18）。math.Trunc 同样向零截断，
// 但没有范围限制。
func DivisionN(dividend, divisor float64, n int) float64 {
	if dividend == 0 || divisor == 0 {
		return 0
	}
	m := math.Pow10(n)
	if math.IsInf(m, 0) {
		// n 太大（>308）⇒ 10^n 溢出，这个精度已无从谈起 ⇒ 直接返回商。
		return dividend / divisor
	}
	return math.Trunc((dividend/divisor)*m) / m
}

// Round 对 float64 进行四舍五入，保留 n 位小数。
func Round(x float64, n int) float64 {
	times := math.Pow10(n)
	x = math.Round(x * times)
	return toFixed(x/times, n)
}

// Floor 对 float64 向下取整，保留 n 位小数。
func Floor(x float64, n int) float64 {
	times := math.Pow10(n)
	x = math.Floor(x * times)
	return toFixed(x/times, n)
}

// ProbabilityElems 计算概率分布的累积边界（按**调用方给定的顺序**）。
//
// elems 是概率值切片（所有权重必须 > 0 且有限），precise 是精度基数（>= 1）。
// 返回的 band 满足：band[i] 是 elems[0..i] 的权重累计（单位化后）⇒ 与 elems
// **同序**，Probability 返回的下标可直接用于索引 elems。
//
// ⚠️ 此前本函数内部排序（排序副本）后**丢弃了置换关系** ⇒ 返回的 band 对应的
// 是**排序后**的顺序，而 Probability 返回的是那个顺序里的下标 ⇒ 调用方拿它
// 索引自己的 elems 会选错元素（权重被静默置换）。实测 elems=[0.6,0.1,0.3]
// 得到 band=[10,40,100]（对应排序后的 [0.1,0.3,0.6]）：下标 0 实际指向权重
// 0.6 的元素、却只分到了 0.1 的份额。
//
// 存在 0/负权重、precise < 1 时直接 panic：这两种输入都会让 band 静默退化为
// 全 0，Probability 恒选 index 0——概率路由静默出错比快速失败更危险。
// （precise < 1 时 10^ceil(log10(precise/minW)) < 1/minW ⇒ 最小权重分不到
// 任何一个单位 ⇒ band[0] == 0。）
func ProbabilityElems(elems []float64, precise float64) []int {
	if len(elems) == 0 {
		return nil
	}
	if math.IsNaN(precise) || precise < 1 {
		panic("mathx: ProbabilityElems requires precise >= 1, got " +
			fmt.Sprintf("%v", precise))
	}
	minW := elems[0]
	for _, w := range elems {
		if math.IsNaN(w) || math.IsInf(w, 0) {
			panic("mathx: ProbabilityElems requires finite weights, got " +
				fmt.Sprintf("%v", elems))
		}
		if w <= 0 {
			panic("mathx: ProbabilityElems requires all weights > 0, got " +
				fmt.Sprintf("%v", elems))
		}
		if w < minW {
			minW = w
		}
	}
	// 精度基数：让**最小权重**也至少分到 1 个单位。
	n := math.Ceil(math.Log10(precise / minW))
	p := math.Pow10(int(n))
	band := make([]int, len(elems))
	acc := 0.0
	for i, w := range elems {
		acc += w
		band[i] = int(acc * p)
	}
	if band[0] <= 0 {
		// 浮点边界：理论上 band[0] >= 1，实际可能因舍入落到 0 ⇒ 退化。
		panic("mathx: ProbabilityElems produced a degenerate band (precise too small for weights " +
			fmt.Sprintf("%v", elems) + ")")
	}
	return band
}

// Probability 根据累积概率边界返回随机选中的索引。
// band 为空时返回 (0, 0)。
func Probability(band []int) (index, r int) {
	l := len(band)
	if l == 0 {
		return 0, 0
	}
	max := band[l-1]
	if max <= 0 {
		return 0, 0
	}
	r = RandIntn(max)
	for i, v := range band {
		// 用 < 而不是 <=：band[i] 是「累计到 i 为止」的上界，元素 i 的份额是
		// [band[i-1], band[i])。用 <= 会把边界值算给前一个桶 ⇒ 第一个桶多一个
		// 单位、最后一个桶少一个（实测 band=[1,2] 时第二个桶**永远选不到**：
		// r ∈ {0,1} 都被 `r <= band[0]` 拦在第 0 桶）。
		if r < v {
			return i, r
		}
	}
	return l - 1, max
}

// ToFixed 将 float64 格式化为指定小数位数。
func ToFixed(num float64, precise int) float64 {
	return toFixed(num, precise)
}

func toFixed(num float64, precise int) float64 {
	if precise < 0 {
		// 负精度会让 fmt 产出 "%!.-1f(...)"（非法浮点字面量）⇒ ParseFloat 失败
		// ⇒ 错误被吞掉、静默返回 0（实测 ToFixed(3.456,-1) == 0）。
		panic("mathx: precise must be >= 0, got " + fmt.Sprintf("%d", precise))
	}
	format := fmt.Sprintf("%%.%df", precise)
	fnum, err := strconv.ParseFloat(fmt.Sprintf(format, num), 64)
	if err != nil {
		// precise >= 0 时 fmt 的输出必是合法浮点字面量，走到这里说明有意外——
		// 不吞错，退回原值而不是静默返回 0。
		return num
	}
	return fnum
}

// RangeRandFloat64 返回 [min, max) 范围内的随机浮点数。
func RangeRandFloat64(min, max float64) float64 {
	if min > max {
		min, max = max, min
	}
	r := rand.Float64()
	return min + ((max - min) * r)
}

// Clamp 将 v 限制在 [lo, hi] 范围内。
// 支持 int、float、string 等所有实现了 cmp.Ordered 的类型。
func Clamp[T cmp.Ordered](v, lo, hi T) T {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
