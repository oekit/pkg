package mathx

import (
	"cmp"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
)

// RandIntn 返回 [0, maxInt) 范围内的随机整数。
// 并发安全：math/rand/v2 的包级函数每 goroutine 持独立状态、无全局锁
// （#386）——此前手搓包级 rng + rngMu，并发概率路由全部在同一把锁上排队；
// 且 RangeRandFloat64 用的是另一套全局源，同包两套随机源行为不一致。
// 统一收敛到 rand/v2 全局源后双轨制消除。
//
// maxInt <= 0 时 panic（带上下文的快速失败，#229）——不把 math/rand 的
// 裸 panic（"invalid argument to Intn"）透传给调用方；与 ToFixed 对负
// 精度、ProbabilityElems 对非法权重的处理同纪律。
func RandIntn(maxInt int) int {
	if maxInt <= 0 {
		panic("mathx: RandIntn requires maxInt > 0, got " + fmt.Sprintf("%d", maxInt))
	}
	return rand.IntN(maxInt)
}

// DivisionN 执行除法并保留 n 位小数（截断，非四舍五入）。
//
// 截断用 math.Trunc 而非 int64 转换：int64(x) 在 x 超出 int64 范围时**回绕**
// （Go 规范未定义该情形，amd64/arm64 实测得到 -9223372036854775808）⇒
// DivisionN(1e18, 1, 6) 实得 9.22e12（期望 1e18）。math.Trunc 同样向零截断，
// 但没有范围限制。
//
// 溢出防护（#182/#317）查两处而非只查 10^n：m = math.Pow10(n) 溢出（n>308）
// 直接返回商；商×m 这个**乘积**溢出（n=300 时 1e9 即可触发——阈值随 n 降到
// ~1.8e308/10^n）同样返回商。乘积溢出成 ±Inf 后 math.Trunc(+Inf)=+Inf、
// +Inf/m=+Inf，此前被原样放行（#317）。
func DivisionN(dividend, divisor float64, n int) float64 {
	if n < 0 {
		// #467：负精度与 toFixed 同口径 fail-fast——此前静默按负位数计算，
		// n≤-308 时 math.Pow10(n) 下溢为 0 不触发 IsInf 守卫，
		// math.Trunc(prod)/m 产出 NaN（同包两套口径收口成一套）。
		panic("mathx: n must be >= 0, got " + fmt.Sprintf("%d", n))
	}
	if dividend == 0 || divisor == 0 {
		return 0
	}
	m := math.Pow10(n)
	if math.IsInf(m, 0) {
		// n 太大（>308）⇒ 10^n 溢出，这个精度已无从谈起 ⇒ 直接返回商。
		return dividend / divisor
	}
	prod := (dividend / divisor) * m
	if math.IsInf(prod, 0) || math.IsNaN(prod) {
		// 商×m 中间量溢出（#317）：返回商（与 10^n 溢出同处置），不把 ±Inf
		// 当合法结果放行。
		return dividend / divisor
	}
	return math.Trunc(prod) / m
}

// Round 对 float64 进行四舍五入，保留 n 位小数。
//
// 溢出防护（#182/#317）查两处而非只查 10^n：times = math.Pow10(n) 溢出
// （n>308）返回原值；x*times 这个**乘积**溢出（n=300 时 1e9 即可触发——
// 阈值随 n 降到 ~1.8e308/10^n）同样返回原值。乘积溢出成 ±Inf 后
// math.Round(+Inf)=+Inf、+Inf/times=+Inf，而 ParseFloat("+Inf") 竟解析成功
// （err=nil），±Inf 此前被当成合法浮点值原样返回——原值被静默丢弃替换成
// ±Inf，金额/指标进库不报错（#317）。
func Round(x float64, n int) float64 {
	times := math.Pow10(n)
	if math.IsInf(times, 0) {
		return x
	}
	prod := x * times
	if math.IsInf(prod, 0) || math.IsNaN(prod) {
		return x // 中间量溢出 ⇒ 原值返回（与 10^n 溢出同处置，#317）
	}
	x = math.Round(prod)
	return toFixed(x/times, n)
}

// Floor 对 float64 向下取整，保留 n 位小数。
// 溢出防护与 Round 同形（#182/#317）：10^n 溢出或 x*times 乘积溢出都返回原值。
func Floor(x float64, n int) float64 {
	times := math.Pow10(n)
	if math.IsInf(times, 0) {
		return x
	}
	prod := x * times
	if math.IsInf(prod, 0) || math.IsNaN(prod) {
		return x // 中间量溢出 ⇒ 原值返回（与 10^n 溢出同处置，#317）
	}
	x = math.Floor(prod)
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
//
// 权重跨度同样有边界（[#208]）：`总权重/最小权重 × precise` 必须落在 int
// 表示范围内，否则同样 panic。这是整数 band 表示法的**固有**边界而非实现
// 限制——最小桶要 ≥ 1 个单位、最大桶要 ≤ MaxInt，两者不可兼得时无解。
// 实际触发需要总权重比最小权重大 15~18 个数量级，正常的概率路由到不了。
func ProbabilityElems(elems []float64, precise float64) []int {
	if len(elems) == 0 {
		return nil
	}
	if math.IsNaN(precise) || math.IsInf(precise, 0) || precise < 1 {
		panic("mathx: ProbabilityElems requires precise >= 1 and finite, got " +
			fmt.Sprintf("%v", precise))
	}
	minW := elems[0]
	total := 0.0
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
		total += w
	}
	// 精度基数：让**最小权重**也至少分到 1 个单位。
	//
	// n 用 log10 之差而不是 log10(precise/minW)：后者在 minW 是次正规数时
	// **中间量先溢出**——precise=1、minW=1e-320 时 1/1e-320 已是 +Inf，于是
	// n=+Inf，而 `int(+Inf)` 同样是实现相关的（arm64 给 MaxInt64、amd64 给
	// MinInt64）⇒ p 在 arm64 上是 +Inf、在 amd64 上是 0，两个平台走进完全
	// 不同的分支。取对数之差数学等价且不产生溢出中间量（log10(1e-320) 正常
	// 返回 -320）。
	n := math.Ceil(math.Log10(precise) - math.Log10(minW))
	p := math.Pow10(int(n))

	// 动态范围上界（[#208]）：band 是「以最小权重为精度单位」的整数表示，
	// 最大值是 total × p。超出 int 表示范围时 float64 → int 的转换结果是
	// **实现相关的**：实测 arm64 饱和到 MaxInt64，amd64 给 MinInt64。两者都
	// 不是错误信号——
	//
	//   - 饱和会把多个大桶挤成同一个数，比例被静默改写（实测
	//     [1e-30, 1e-30, 1e3] 下第二个桶被高估约 17 个数量级）；
	//   - MinInt64 让 Probability 的 max<=0 分支直接退化到「恒选 index 0」。
	//
	// 这不是「算错了」，而是这个表示法装不下：总权重与最小权重相差超过 int
	// 的动态范围时，没有任何整数 band 能同时表达「最小桶 ≥ 1 个单位」与
	// 「最大桶 ≤ MaxInt」。
	//
	// 故与既有输入校验同口径——快速失败。给出一份静默失真的分布比 panic
	// 危险得多，这正是本函数拒绝 0/负权重与 precise<1 的同一条理由。
	if maxBand := total * p; !(maxBand <= float64(math.MaxInt64)) {
		panic("mathx: ProbabilityElems weights span too wide a dynamic range for an int band: " +
			fmt.Sprintf("total/min ratio %v/%v x precise %v = %v exceeds %v",
				total, minW, precise, maxBand, float64(math.MaxInt64)))
	}
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
	// 防御闸（#317）：ParseFloat 接受 "Inf"/"NaN" 字面量（err=nil），若有限
	// 输入经格式化竟产出 ±Inf/NaN（正常不可能——fmt 对有限值必产出有限
	// 字面量），不能把它当合法结果放行，退回原值。
	if math.IsInf(fnum, 0) || math.IsNaN(fnum) {
		return num
	}
	return fnum
}

// RangeRandFloat64 返回 [min, max) 范围内的随机浮点数。
// 随机源与 RandIntn 同源（math/rand/v2 全局源，#386——此前是 v1 全局源的
// 另一套，两族函数行为口径不一致）。
func RangeRandFloat64(min, max float64) float64 {
	if min > max {
		min, max = max, min
	}
	r := rand.Float64()
	return min + ((max - min) * r)
}

// Clamp 将 v 限制在 [lo, hi] 范围内。
// 支持 int、float、string 等所有实现了 cmp.Ordered 的类型。
//
// lo > hi 时 panic（#228）：上下界颠倒（如配置里 max/min 顺序写反）是
// 编程错误——此前会静默返回落在 [hi, lo] 之外的值，比快速失败更危险；
// 与 ToFixed 对负精度、ProbabilityElems 对非法权重的处理同纪律。
func Clamp[T cmp.Ordered](v, lo, hi T) T {
	if lo > hi {
		panic("mathx: Clamp requires lo <= hi, got lo=" + fmt.Sprintf("%v", lo) +
			", hi=" + fmt.Sprintf("%v", hi))
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
