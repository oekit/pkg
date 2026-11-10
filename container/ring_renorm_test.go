package container

// Ring 逻辑序归基回归测试（pkg#354）：32 位平台 int 逻辑序回绕后负下标
// panic。白盒把计数器推到字长边缘，钉住「归基后 FIFO 序不乱、Len 不变、
// 不出负下标」三条不变量；GOARCH=386 下由 CI 的 32 位构建守门（本测试
// 阈值相对 math.MaxInt 计算，两种字长都走同一条归基路径）。
import (
	"math"
	"testing"
)

// TestRingRenormalizeNearOverflow 白盒：逻辑序逼近 int 上限时 Add/Take/Len
// 必须正常——归基触发后 FIFO 序不乱、不出负下标。
func TestRingRenormalizeNearOverflow(t *testing.T) {
	r := NewRing[int](3)
	// 把逻辑序推到阈值之上（相对 math.MaxInt 设置，64 位测试同样命中归基分支）
	r.read = math.MaxInt - 4
	r.write = math.MaxInt - 4
	for i := range 100 {
		r.Add(i)
		if v, ok := r.Take(); !ok || v != i {
			t.Fatalf("归基后 FIFO 序错乱：i=%d got=(%v,%v)", i, v, ok)
		}
		if r.Len() != 0 {
			t.Fatalf("空转时 Len 应为 0，got %d", r.Len())
		}
		if r.read < 0 || r.write < 0 {
			t.Fatalf("归基失败：计数器出现负值 read=%d write=%d", r.read, r.write)
		}
	}
}

// TestRingRenormalizeAcrossFullness 白盒：满载覆盖路径跨越归基阈值——
// 最旧的被覆盖、Take 得到的仍是最新的至多 n 条且保序。
func TestRingRenormalizeAcrossFullness(t *testing.T) {
	r := NewRing[int](3)
	r.read = math.MaxInt - 6
	r.write = math.MaxInt - 3 // 满载：积压 3
	for i := range 10 {
		r.Add(i) // 持续触发归基 + 覆盖最旧
	}
	if got := r.Len(); got != 3 {
		t.Fatalf("满载覆盖后积压应保持容量 3，got %d", got)
	}
	want := []int{7, 8, 9} // 最新 3 条
	for _, w := range want {
		v, ok := r.Take()
		if !ok || v != w {
			t.Fatalf("FIFO 取值错乱：want %d got (%v,%v)", w, v, ok)
		}
	}
	if _, ok := r.Take(); ok {
		t.Fatalf("取空后 Take 应返回 false")
	}
}

// TestRingRenormalizeKeepsSlotMapping 白盒：归基基点必须是 n 的整数倍——
// 槽位映射（x%n）在归基前后一致，元素物理位置无需搬动。
func TestRingRenormalizeKeepsSlotMapping(t *testing.T) {
	r := NewRing[int](5) // n=5 与 2^k 无公约——映射偏差必然显形
	// 预填 7 条（覆盖 2 条，积压 5=满载），逻辑序落在低位
	for i := range 7 {
		r.Add(i)
	}
	// 手工把计数器推到阈值上模拟长驻进程：偏移取 ≥阈值的最小 n 整数倍
	// （偏移 ≡0 (mod n) ⇒ 槽位映射不变，逻辑位与预填后一致）。
	const n = 5
	offset := ((ringRenormThreshold + n - 1) / n) * n
	r.read += offset
	r.write += offset
	// 触发归基后再写入，物理槽位必须与归基前连续——Take 全量核对
	r.Add(100)
	got := make([]int, 0, 6)
	for {
		v, ok := r.Take()
		if !ok {
			break
		}
		got = append(got, v)
	}
	want := []int{3, 4, 5, 6, 100} // 原积压 5 条 + 新写入 1 条（覆盖最旧 2）
	if len(got) != len(want) {
		t.Fatalf("归基后元素错乱：got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("归基后槽位映射漂移：got %v want %v", got, want)
		}
	}
}
