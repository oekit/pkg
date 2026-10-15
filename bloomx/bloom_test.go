package bloomx

import (
	"fmt"
	"sync"
	"testing"
)

// TestNew_PinnedParams 钉住 m/k 公式：New(1024, 0.01) 按标准公式
// m = ceil(-n·ln p/(ln2)²) = 9816 → 向上取 2 的幂 = 16384；
// k = ceil(m/n·ln2) = 12。公式或归一规则变更时此测试失败提醒同步文档。
func TestNew_PinnedParams(t *testing.T) {
	bf := New(1024, 0.01)
	if got := bf.Bits(); got != 16384 {
		t.Errorf("Bits() = %d, want 16384", got)
	}
	if got := bf.HashFuncs(); got != 12 {
		t.Errorf("HashFuncs() = %d, want 12", got)
	}
}

// TestNew_ClampsInvalidInputs 非法入参归一为文档化默认值而非报错：
// est<=0 → 1024，fpRate 不在 (0,1) → 0.01。
func TestNew_ClampsInvalidInputs(t *testing.T) {
	want := New(DefaultEstElements, DefaultFPRate)
	for _, tt := range []struct {
		name       string
		est        int
		fpRate     float64
		wantBits   int
		wantHashes int
	}{
		{"zero est", 0, 0.01, 16384, 12},
		{"negative est", -7, 0.01, 16384, 12},
		{"fpRate zero", 1024, 0, 16384, 12},
		{"fpRate negative", 1024, -0.5, 16384, 12},
		{"fpRate one", 1024, 1, 16384, 12},
		{"fpRate above one", 1024, 1.5, 16384, 12},
		{"both invalid matches defaults", 0, 0, want.Bits(), want.HashFuncs()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bf := New(tt.est, tt.fpRate)
			if bf.Bits() != tt.wantBits {
				t.Errorf("Bits() = %d, want %d", bf.Bits(), tt.wantBits)
			}
			if bf.HashFuncs() != tt.wantHashes {
				t.Errorf("HashFuncs() = %d, want %d", bf.HashFuncs(), tt.wantHashes)
			}
		})
	}
}

func TestFilter_AddTest(t *testing.T) {
	bf := New(1024, 0.01)
	if bf.Test("never-added") {
		t.Error("empty filter must report false for any element")
	}

	bf.Add("foo")
	bf.Add("bar")
	for _, key := range []string{"foo", "bar"} {
		if !bf.Test(key) {
			t.Errorf("Test(%q) = false, want true", key)
		}
		if !bf.TestBytes([]byte(key)) {
			t.Errorf("TestBytes(%q) = false, want true", key)
		}
	}
	// 哈希全确定性（无随机源），空余位充足，该断言恒稳定
	if bf.Test("baz") {
		t.Error(`Test("baz") = true on a nearly empty filter, want false`)
	}
}

func TestFilter_AddMatchesAddBytes(t *testing.T) {
	viaString := New(1024, 0.01)
	viaBytes := New(1024, 0.01)
	viaString.Add("same-key")
	viaBytes.AddBytes([]byte("same-key"))

	if !viaBytes.Test("same-key") || !viaString.TestBytes([]byte("same-key")) {
		t.Error("Add and AddBytes must share the same hashing path")
	}
}

// TestFilter_FalsePositiveRate 经验校验：按 1% 误判率建过滤器、灌入
// 容量个元素后，对 1 万个未加入元素统计误判，应显著低于 5%
// （哈希全确定性，断言结果恒定）。阈值放宽到 5%：本测试目的是
// 兜住公式/实现退化，而非复刻理论曲线。
func TestFilter_FalsePositiveRate(t *testing.T) {
	const n = 5000
	bf := New(n, 0.01)
	for i := 0; i < n; i++ {
		bf.Add(fmt.Sprintf("member-%d", i))
	}

	fp := 0
	const probes = 10000
	for i := 0; i < probes; i++ {
		if bf.Test(fmt.Sprintf("probe-%d", i)) {
			fp++
		}
	}
	if rate := float64(fp) / probes; rate > 0.05 {
		t.Errorf("false positive rate = %.4f, want <= 0.05 (%d/%d)", rate, fp, probes)
	}
}

func TestFilter_Reset(t *testing.T) {
	bf := New(1024, 0.01)
	bf.Add("foo")
	bf.Reset()
	if bf.Test("foo") {
		t.Error("Test after Reset should be false")
	}
	if bf.Bits() != 16384 || bf.HashFuncs() != 12 {
		t.Error("Reset must preserve m/k configuration")
	}
	// Reset 后可继续使用
	bf.Add("bar")
	if !bf.Test("bar") {
		t.Error("filter unusable after Reset")
	}
}

// TestFilter_Concurrent 并发 Add/Test/Reset 不数据竞争（配合 -race）。
func TestFilter_Concurrent(t *testing.T) {
	bf := New(1024, 0.01)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				key := fmt.Sprintf("w%d-k%d", w, i)
				bf.Add(key)
				_ = bf.Test(key)
			}
		}(w)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		bf.Reset()
	}()
	wg.Wait()
}
