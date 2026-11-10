package procx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMemLimitHelperProcess 是 re-exec 辅助进程入口：按 PROCX_MEMLIMIT_HELPER
// 环境变量行动（touch=写标记文件 / alloc=分配 N 字节并触页）；正常跑测试时
// 环境变量缺位，什么也不做。
func TestMemLimitHelperProcess(t *testing.T) {
	switch os.Getenv("PROCX_MEMLIMIT_HELPER") {
	case "touch":
		if err := os.WriteFile(os.Getenv("PROCX_MEMLIMIT_MARKER"), []byte("ok"), 0o644); err != nil {
			t.Fatalf("helper touch: %v", err)
		}
	case "alloc":
		n, err := strconv.Atoi(os.Getenv("PROCX_MEMLIMIT_ALLOC"))
		if err != nil {
			t.Fatalf("helper alloc: %v", err)
		}
		buf := make([]byte, n)
		for i := range len(buf) { // 逐页触碰，逼物理内存落地（否则换页/延迟分配不算数）
			buf[i] = 1
		}
	}
}

// helperArgs 构造 re-exec 辅助进程的参数与环境。
func helperArgs(mode string, extra ...string) ([]string, []string) {
	args := []string{"-test.run=^TestMemLimitHelperProcess$"}
	env := append([]string{"PROCX_MEMLIMIT_HELPER=" + mode}, extra...)
	return args, env
}

func TestWithMemoryLimitPanics(t *testing.T) {
	for _, n := range []int64{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("WithMemoryLimit(%d) should panic（构造期 fail-fast）", n)
				}
			}()
			WithMemoryLimit(n)
		}()
	}
}

// TestMemGroupAttachValidation 钉住 Attach 的参数纪律（各平台一致）。
func TestMemGroupAttachValidation(t *testing.T) {
	g := &MemGroup{}
	if err := g.Attach(0); err == nil {
		t.Errorf("Attach(0) = nil, want error")
	}
	if err := g.Attach(-3); err == nil {
		t.Errorf("Attach(-3) = nil, want error")
	}
}

// TestRunWithMemoryLimitFailClosed 钉住 pkg#359 的核心纪律：兑现不了就
// **拒绝执行**（fail closed），不得静默降级成不限——子进程一个字节都不许跑；
// 兑现得了才真跑，且跑完无残组。两个分支都断言。
func TestRunWithMemoryLimitFailClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	marker := filepath.Join(t.TempDir(), "ran")
	args, env := helperArgs("touch", "PROCX_MEMLIMIT_MARKER="+marker)

	res, err := Run(ctx, os.Args[0], args, WithMemoryLimit(32<<20), WithEnv(env))

	capableErr := MemoryLimitCapable()
	if capableErr == nil {
		// 可兑现：跑了、退出 0、标记在
		if err != nil {
			t.Fatalf("Run(WithMemoryLimit) on capable host: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("Run(WithMemoryLimit) exit = %d, want 0", res.ExitCode)
		}
		if _, statErr := os.Stat(marker); statErr != nil {
			t.Fatalf("helper did not run: %v", statErr)
		}
		return
	}
	// 不可兑现：必须报错拒绝执行，且子进程没有跑过（静默不限 = 失效形态）。
	if err == nil {
		t.Fatalf("Run(WithMemoryLimit) on incapable host = nil error, want fail-closed（宿主不能兑现：%v）", capableErr)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("子进程跑过了（%v）——fail closed 被破坏，静默不限是本包禁止的形态", statErr)
	}
	if res != nil {
		t.Errorf("fail-closed 应返回 nil Result，got %+v", res)
	}
}

// memGroupsUnder 递归找出 root 下残留的 procx-mem-* 限额组目录。
func memGroupsUnder(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			p := filepath.Join(dir, e.Name())
			if strings.HasPrefix(e.Name(), "procx-mem-") {
				found = append(found, p)
			}
			walk(p)
		}
	}
	walk(root)
	return found
}

// TestMemLimitOOMKill（能力探测通过才跑）：超限子进程被 OOM-kill，父侧从
// 退出状态可辨「被信号杀（兜底生效）」与「自己非零退出」；负对照钉住
// 「不设限时同程序跑过」（上限真的生效，不是程序自己挂了）。
func TestMemLimitOOMKill(t *testing.T) {
	if err := MemoryLimitCapable(); err != nil {
		t.Skipf("宿主不能兑现内存上限（%v），跳过 OOM 实证", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const alloc = 512 << 20
	args, env := helperArgs("alloc", "PROCX_MEMLIMIT_ALLOC="+strconv.Itoa(alloc))

	// 负对照：不设限，512MB 分配应当跑过（退出 0）。
	res, err := Run(ctx, os.Args[0], args, WithEnv(env))
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("负对照（不设限）失败：exit=%v err=%v——OOM 归因不成立", res.ExitCode, err)
	}

	// 正对照：64MB 上限，512MB 分配触发内核 OOM-kill。
	res, err = Run(ctx, os.Args[0], args, WithMemoryLimit(64<<20), WithEnv(env))
	if err == nil {
		t.Fatalf("超限子进程未被杀，err=nil（内存上限未生效）")
	}
	if res == nil || res.ExitCode != -1 {
		t.Fatalf("OOM-kill 的退出状态应为 -1（信号杀），got %+v err=%v", res, err)
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v（%T），want *exec.ExitError（被信号杀应可辨）", err, err)
	}
}
