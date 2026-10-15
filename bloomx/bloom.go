package bloomx

import (
	"math"
	"math/bits"
	"sync"

	"github.com/oekit/pkg/encoding"
)

const (
	// DefaultEstElements 是 estElements 非法（<=0）时归一采用的默认容量。
	DefaultEstElements = 1024
	// DefaultFPRate 是 fpRate 非法（不在 (0,1) 开区间）时归一采用的默认误判率。
	DefaultFPRate = 0.01
	// maxBits 是位数组长度上限（位）——防御极端参数（如 fpRate 取 1e-300）
	// 导致 m 溢出，实际分配在此之前就会因内存不足失败。
	maxBits = 1 << 62
	// hashSalt 是计算 h2 时拼在数据尾部的盐：同一数据两次 FNV-1a 结果恒等，
	// 无法充当独立第二哈希。更换盐值会改变全部哈希结果（过滤器仅在内存
	// 存活，无持久化兼容问题）。
	hashSalt byte = 0x9e
)

// Filter 是进程内布隆过滤器。
// Test 返回 false 表示一定不存在；返回 true 表示可能存在（误判率 <= fpRate）。
// 必须经 New 创建，零值不可用。
type Filter struct {
	mu   sync.RWMutex
	bits []uint64
	m    uint64 // 位数组长度（位，2 的幂）
	k    uint64 // 哈希函数个数
}

// New 创建布隆过滤器。estElements 为预计元素数量，fpRate 为目标误判率
// （如 0.01 表示 1%）。非法入参归一为文档化默认值：estElements<=0 取
// DefaultEstElements，fpRate 不在 (0,1) 取 DefaultFPRate。
//
// 位数组长度 m = ceil(-n·ln p / (ln2)²)，向上取 2 的幂（与奇数 h2 配合
// 保证每条哈希序列完整覆盖位数组，取模同时退化为位掩码），下限 64；
// 哈希函数个数 k = ceil(m/n·ln2)，下限 1。
func New(estElements int, fpRate float64) *Filter {
	if estElements <= 0 {
		estElements = DefaultEstElements
	}
	if fpRate <= 0 || fpRate >= 1 {
		fpRate = DefaultFPRate
	}

	n := float64(estElements)
	ln2 := math.Ln2
	m := math.Ceil(-n * math.Log(fpRate) / (ln2 * ln2))
	if m < 64 {
		m = 64
	}
	if m > maxBits {
		m = maxBits
	}
	// 向上取 2 的幂；m 已封顶 maxBits，移位不会溢出
	mu := uint64(m)
	if mu&(mu-1) != 0 {
		mu = 1 << (64 - bits.LeadingZeros64(mu))
	}
	k := uint64(math.Ceil(float64(mu) / n * ln2))
	if k < 1 {
		k = 1
	}
	return &Filter{
		bits: make([]uint64, mu/64),
		m:    mu,
		k:    k,
	}
}

// Add 添加元素（字符串便捷形式）。
func (f *Filter) Add(s string) {
	f.AddBytes([]byte(s))
}

// AddBytes 添加元素。
// 布隆过滤器只做存在性预检，不返回"是否为新元素"。
func (f *Filter) AddBytes(b []byte) {
	h1, h2 := f.hash(b)
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := uint64(0); i < f.k; i++ {
		f.set((h1 + i*h2) & (f.m - 1))
	}
}

// Test 报告元素是否可能存在（字符串便捷形式）。
func (f *Filter) Test(s string) bool {
	return f.TestBytes([]byte(s))
}

// TestBytes 报告元素是否可能存在。
// false 表示一定不存在；true 表示可能存在（误判率 <= fpRate）。
func (f *Filter) TestBytes(b []byte) bool {
	h1, h2 := f.hash(b)
	f.mu.RLock()
	defer f.mu.RUnlock()
	for i := uint64(0); i < f.k; i++ {
		if !f.get((h1 + i*h2) & (f.m - 1)) {
			return false
		}
	}
	return true
}

// Reset 清空全部位（保留 m/k 配置，可继续复用）。
func (f *Filter) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.bits {
		f.bits[i] = 0
	}
}

// Bits 返回位数组长度（位）。
func (f *Filter) Bits() int {
	return int(f.m)
}

// HashFuncs 返回哈希函数个数 k。
func (f *Filter) HashFuncs() int {
	return int(f.k)
}

// hash 返回 Kirsch-Mitzenmacher 双哈希：h1 = Fnv1a64(data)，
// h2 = Fnv1a64(data+salt) 且强制为奇数。
//
// 两次 FNV 结果在消费前先过 64 位 finalizer（murmur3 fmix64）打散：
// FNV-1a 低位的雪崩质量弱，而索引恰取低位掩码——实测不过 finalizer 时
// 1% 目标误判的过滤器实测 6.6%，且出现被数千元素中十几个共同覆盖的
// 热点位；过 finalizer 后实测 0.28%，接近理论值。
func (f *Filter) hash(b []byte) (h1, h2 uint64) {
	h1 = mix64(encoding.Fnv1a64(b))
	// 拼盐走独立缓冲，避免 append 改写调用方切片的底层数组
	buf := make([]byte, len(b)+1)
	copy(buf, b)
	buf[len(b)] = hashSalt
	h2 = mix64(encoding.Fnv1a64(buf))
	h2 |= 1
	return h1, h2
}

// mix64 是 murmur3 的 64 位 finalizer，把弱雪崩的输入打散到位级均匀。
func mix64(x uint64) uint64 {
	x ^= x >> 33
	x *= 0xff51afd7ed558ccd
	x ^= x >> 33
	x *= 0xc4ceb9fe1a85ec53
	x ^= x >> 33
	return x
}

func (f *Filter) set(bit uint64) {
	f.bits[bit>>6] |= 1 << (bit & 63)
}

func (f *Filter) get(bit uint64) bool {
	return f.bits[bit>>6]&(1<<(bit&63)) != 0
}
