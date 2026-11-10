package procx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// —— pkg#359 反向验证矩阵 ——

// TestWithMemoryLimitFailFast 钉住仓规总则①：非法上限构造期 fail-fast
// （0/负数几乎必然是笔误，静默吞掉等于「声明有上限、实际没有」）。
func TestWithMemoryLimitFailFast(t *testing.T) {
	for _, n := range []int64{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("WithMemoryLimit(%d) 未 panic", n)
				}
			}()
			_ = WithMemoryLimit(n)
		}()
	}
}

// TestMemoryLimitCapableDeclaresUnsupported 钉住非 linux 的如实降级声明：
// 回「不能兑现」（ErrMemoryLimitUnsupported），不假装支持。
func TestMemoryLimitCapableDeclaresUnsupported(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("linux 侧行为由 memlimit_linux_test.go 覆盖")
	}
	if err := MemoryLimitCapable(); !errors.Is(err, ErrMemoryLimitUnsupported) {
		t.Errorf("MemoryLimitCapable() = %v, want 包含 ErrMemoryLimitUnsupported", err)
	}
	_, err := Run(context.Background(), os.Args[0],
		[]string{"-test.run=TestMemoryLimitHelperProcess"}, WithMemoryLimit(1<<20))
	if !errors.Is(err, ErrMemoryLimitUnsupported) {
		t.Errorf("Run(WithMemoryLimit) = %v, want 包含 ErrMemoryLimitUnsupported", err)
	}
}

// TestMemoryLimitFailClosedNoSilentUnlimited 钉住「兑现不了必须拒绝执行」：
// 建不出组/非 v2/非 linux 时 Run 报错且子进程**没有被执行**（哨兵不出现），
// 与「静默不限」（上限没生效但调用看起来成功）可辨。负对照：同一子进程
// 不设限时照常执行。
func TestMemoryLimitFailClosedNoSilentUnlimited(t *testing.T) {
	capable := MemoryLimitCapable()
	sentinel := filepath.Join(t.TempDir(), "sentinel")
	ctx := context.Background()
	args := []string{"-test.run=TestMemoryLimitHelperProcess"}
	env := []string{"PROCX_MEMLIMIT_HELPER=1", "PROCX_MEMLIMIT_SENTINEL=" + sentinel}

	// 负对照：不设限同一子进程跑过
	if _, err := Run(ctx, os.Args[0], args, WithEnv(env)); err != nil {
		t.Fatalf("负对照（无上限）Run error: %v", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("负对照未执行（哨兵缺失）：%v", err)
	}
	if err := os.Remove(sentinel); err != nil {
		t.Fatal(err)
	}

	res, err := Run(ctx, os.Args[0], args, WithEnv(env), WithMemoryLimit(1<<20))
	if capable != nil {
		if err == nil {
			t.Fatalf("MemoryLimitCapable 报不能兑现（%v）但 Run 未报错——静默不限", capable)
		}
		if res != nil {
			t.Errorf("拒绝执行时不该有 Result：%+v", res)
		}
		if _, statErr := os.Stat(sentinel); statErr == nil {
			t.Error("上限未兑现却执行了子进程（哨兵出现）——必须 fail closed 拒绝执行")
		}
		return
	}
	if err != nil {
		t.Fatalf("Run with limit error: %v", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("上限下子进程未执行：%v", err)
	}
}

// TestMemoryLimitHelperProcess 是子进程替身：按环境变量选择行为（写哨兵/
// 分配内存/睡眠），正常测试运行时立即返回。
func TestMemoryLimitHelperProcess(t *testing.T) {
	if os.Getenv("PROCX_MEMLIMIT_HELPER") != "1" {
		return
	}
	if ms := os.Getenv("PROCX_MEMLIMIT_SLEEP_MS"); ms != "" {
		d, _ := strconv.Atoi(ms)
		time.Sleep(time.Duration(d) * time.Millisecond)
	}
	if mb := os.Getenv("PROCX_MEMLIMIT_ALLOC_MB"); mb != "" {
		n, _ := strconv.Atoi(mb)
		var hold [][]byte
		for i := 0; i < n; i++ {
			b := make([]byte, 1<<20)
			for j := 0; j < len(b); j += 4096 {
				b[j] = 1 // 触页：物理页落地才算 RSS（memory.max 帽的是物理内存）
			}
			hold = append(hold, b)
		}
		_ = hold
	}
	_ = os.WriteFile(os.Getenv("PROCX_MEMLIMIT_SENTINEL"), []byte("ran"), 0o600)
	os.Exit(0)
}
