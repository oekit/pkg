package strmap

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/maphash"
	"math"
	"math/bits"
	"sort"
	"strings"
	"unsafe"
)

// ErrKvLenMismatch LoadFromSlice/LoadFromSlice 的键值长度不一致。
var ErrKvLenMismatch = errors.New("strmap: kv 长度不一致")

// ErrDuplicateKey LoadFromSlice 检测到重复键（本实现拒绝重复键以保证 Get 结果确定）。
var ErrDuplicateKey = errors.New("strmap: 存在重复键")

// ErrKeyTooLarge 单键超过 uint32 上限（理论边界，正常数据不会触达）。
var ErrKeyTooLarge = errors.New("strmap: 键过长")

// StrMap 表示 GC 友好的只读字符串 map。
// 值类型 V 建议不含指针（不含指针时整张表对 GC 是一块标量内存，零扫描开销）。
type StrMap[V any] struct {
	// data 打包存放全部键字节，键视图是其上的零拷贝切片
	data []byte
	// items 按哈希槽分组存放键元信息与值
	items []mapItem[V]
	// hashtable 槽位 -> items 下标（int32 省内存；-1 表示空槽）
	hashtable []int32
	seed      maphash.Seed
}

type mapItem[V any] struct {
	off  int
	sz   uint32
	slot uint32
	v    V
}

// New 创建空的 StrMap。
func New[V any]() *StrMap[V] {
	return &StrMap[V]{seed: maphash.MakeSeed()}
}

// NewFromMap 从 map 构建并返回；构建失败（重复键等）panic。
// 适合包级静态表场景——构建期错误在启动时暴露。
func NewFromMap[V any](m map[string]V) *StrMap[V] {
	ret := New[V]()
	if err := ret.LoadFromMap(m); err != nil {
		panic(err)
	}
	return ret
}

// NewFromSlice 从等长键值切片构建并返回；构建失败 panic。
func NewFromSlice[V any](kk []string, vv []V) *StrMap[V] {
	ret := New[V]()
	if err := ret.LoadFromSlice(kk, vv); err != nil {
		panic(err)
	}
	return ret
}

// LoadFromMap 重置并从 map 加载。
func (m *StrMap[V]) LoadFromMap(mp map[string]V) error {
	kk := make([]string, 0, len(mp))
	vv := make([]V, 0, len(mp))
	for k, v := range mp {
		kk = append(kk, k)
		vv = append(vv, v)
	}
	return m.LoadFromSlice(kk, vv)
}

// LoadFromSlice 重置并从等长键值切片加载；检测到重复键返回 ErrDuplicateKey。
func (m *StrMap[V]) LoadFromSlice(kk []string, vv []V) error {
	if len(kk) != len(vv) {
		return ErrKvLenMismatch
	}
	seen := make(map[string]struct{}, len(kk))
	sz := 0
	for _, k := range kk {
		if _, dup := seen[k]; dup {
			return ErrDuplicateKey
		}
		seen[k] = struct{}{}
		if uint64(len(k)) > math.MaxUint32 {
			return ErrKeyTooLarge
		}
		sz += len(k)
	}

	// data 每次重载都**重新分配**，不复用旧缓冲。
	//
	// 此前是 `m.data = m.data[:0]` 后就地重写：key() 返回的是 data 上的
	// unsafe.String 零拷贝视图，重载把旧缓冲的字节**就地改写** ⇒ 之前返回的
	// 键字符串内容随之改变（破坏 Go 字符串不可变保证）。
	// 实测：Get("k") 拿到 "v1" 后 LoadFromSlice 重写同一位置，旧字符串变成
	// "w2"——调用方以为拿到的是快照，实际是个活引用。
	//
	// 重新分配后，旧视图仍指向旧缓冲（旧缓冲由视图保活）⇒ 视图在整个 StrMap
	// 存活期间保持有效（doc.go 的承诺）。
	m.data = make([]byte, 0, sz)
	m.items = m.items[:0]
	if cap(m.items) < len(vv) {
		m.items = make([]mapItem[V], 0, len(vv))
	}

	for i, k := range kk {
		m.items = append(m.items, mapItem[V]{
			off:  len(m.data),
			sz:   uint32(len(k)),
			slot: uint32(maphash.String(m.seed, k)),
			v:    vv[i],
		})
		m.data = append(m.data, k...)
	}
	m.makeHashtable()
	return nil
}

// Len 返回条目数。
func (m *StrMap[V]) Len() int { return len(m.items) }

// Item 返回第 i 个条目（按内部存储序，非字典序）。
// i 越界时 panic（与切片下标语义一致，调用方以 Len 控制范围）。
func (m *StrMap[V]) Item(i int) (string, V) {
	e := &m.items[i]
	return m.key(e), e.v
}

// Get 查找键；表未构建（空表）时返回零值 + false。
// 哈希冲突时顺位扫描同槽条目，最坏 O(n)，实际负载因子 0.75 下近 O(1)。
func (m *StrMap[V]) Get(s string) (V, bool) {
	var zero V
	if len(m.hashtable) == 0 {
		return zero, false
	}
	slot := uint32(maphash.String(m.seed, s)) % uint32(len(m.hashtable))
	i := m.hashtable[slot]
	if i < 0 {
		return zero, false
	}
	e := &m.items[i]
	if m.key(e) == s {
		return e.v, true
	}
	// 冲突：同槽条目在 items 中连续存放，从首个条目向后扫描
	for j := i + 1; j < int32(len(m.items)); j++ {
		e = &m.items[j]
		if e.slot != slot {
			break
		}
		if m.key(e) == s {
			return e.v, true
		}
	}
	return zero, false
}

// String 返回调试友好的内容表示（非确定性顺序）。
func (m *StrMap[V]) String() string {
	var b strings.Builder
	b.WriteString("{\n")
	for i := range m.items {
		k, v := m.Item(i)
		fmt.Fprintf(&b, "%q: %v,\n", k, v)
	}
	b.WriteString("}")
	return b.String()
}

// key 返回条目键的零拷贝视图。StrMap 构建后 data 只读，视图安全。
func (m *StrMap[V]) key(e *mapItem[V]) string {
	return unsafe.String(unsafe.SliceData(m.data[e.off:e.off+int(e.sz)]), int(e.sz))
}

// makeHashtable 按负载因子 0.75 定容素数表，槽位分组排序后填表。
func (m *StrMap[V]) makeHashtable() {
	slots := calcHashtableSlots(len(m.items))
	if cap(m.hashtable) < int(slots) {
		m.hashtable = make([]int32, slots)
	} else {
		m.hashtable = m.hashtable[:slots]
	}

	// 槽位折叠到实际表大小
	for i := range m.items {
		m.items[i].slot %= uint32(slots)
	}

	// 同槽条目连续存放（利于 CPU cache），冲突时 Get 沿 items 顺位扫描
	sort.Slice(m.items, func(i, j int) bool { return m.items[i].slot < m.items[j].slot })

	for i := range m.hashtable {
		m.hashtable[i] = -1
	}
	for i := range m.items {
		e := &m.items[i]
		if m.hashtable[e.slot] < 0 {
			m.hashtable[e.slot] = int32(i)
		}
	}
}

// bits2primes 按 2 的幂档位给出一档素数（大于对应 2^n），素数取模分布更均匀。
var bits2primes = []int32{
	0:  1,
	1:  7,
	2:  7,
	3:  17,
	4:  17,
	5:  31,
	6:  61,
	7:  127,
	8:  251,
	9:  509,
	10: 1021,
	11: 2039,
	12: 4093,
	13: 8191,
	14: 16381,
	15: 32749,
	16: 65521,
	17: 131071,
	18: 262139,
	19: 524287,
	20: 1048573,
	21: 2097143,
	22: 4194301,
	23: 8388593,
	24: 16777213,
	25: 33554393,
	26: 67108859,
	27: 134217689,
	28: 268435399,
	29: 536870909,
	30: 1073741789,
	31: 2147483647,
}

const hashtableLoadFactor = 0.75

func calcHashtableSlots(n int) int32 {
	b := bits.Len64(uint64(float64(n) / hashtableLoadFactor))
	if b >= len(bits2primes) {
		// 31 位档 ≈ 21 亿条目，到这个量级应拆表而不是撑单表
		panic("strmap: 条目数超出单表上限")
	}
	return bits2primes[b]
}

// strStore 打包存放一批字符串（[u32 长度][字节] 连续布局），
// Get 返回零拷贝视图。存储后不可删除——与 StrMap 的只读语义配套。
type strStore struct {
	buf []byte
}

func (s *strStore) Load(ss []string) ([]int, error) {
	const lenSize = 4 // uint32
	total := lenSize * len(ss)
	for _, str := range ss {
		if uint64(len(str)) > math.MaxUint32 {
			return nil, ErrKeyTooLarge
		}
		total += len(str)
	}
	idxes := make([]int, len(ss))
	// buf 每次重载都**重新分配**，不复用旧缓冲——否则重载会把旧缓冲的字节
	// 就地改写，Get 之前返回的字符串视图内容随之改变（破坏 Go 字符串不可变
	// 保证）。重新分配后旧视图仍指向旧缓冲（旧缓冲由视图保活）。
	s.buf = make([]byte, total)
	off := 0
	for i, str := range ss {
		idxes[i] = off
		binary.NativeEndian.PutUint32(s.buf[off:], uint32(len(str)))
		copy(s.buf[off+lenSize:], str)
		off += lenSize + len(str)
	}
	return idxes, nil
}

func (s *strStore) Get(idx int) string {
	const lenSize = 4
	if idx < 0 || idx+lenSize > len(s.buf) {
		return ""
	}
	length := int(binary.NativeEndian.Uint32(s.buf[idx:]))
	if idx+lenSize+length > len(s.buf) {
		return ""
	}
	b := s.buf[idx+lenSize : idx+lenSize+length]
	return unsafe.String(unsafe.SliceData(b), len(b))
}
