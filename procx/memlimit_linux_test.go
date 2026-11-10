//go:build linux

package procx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMemGroupLifecycle（能力探测通过才跑）：创建 → 设置 → 清理三步的
// 实证面——memory.max 写入并回读一致、Attach 真的把进程移进组、Close
// 之后组不存在（无残组）。
func TestMemGroupLifecycle(t *testing.T) {
	if err := MemoryLimitCapable(); err != nil {
		t.Skipf("宿主不能兑现内存上限（%v）", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	g, err := NewMemGroup(64 << 20)
	if err != nil {
		t.Fatalf("NewMemGroup: %v", err)
	}
	// 上限真的写进 memory.max（页对齐后取值）。
	page := int64(os.Getpagesize())
	want := (64<<20 + page - 1) / page * page
	back, err := os.ReadFile(filepath.Join(g.dir, "memory.max"))
	if err != nil {
		t.Fatalf("read memory.max: %v", err)
	}
	if got := strings.TrimSpace(string(back)); got != strconv.FormatInt(want, 10) {
		t.Fatalf("memory.max = %s, want %d", got, want)
	}

	// Attach：短命子进程真的进了组（cgroup.procs 出现 pid）。
	args, env := helperArgs("touch", "PROCX_MEMLIMIT_MARKER="+filepath.Join(t.TempDir(), "m"))
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), env...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	if err := g.Attach(cmd.Process.Pid); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	_ = cmd.Wait()

	// Attach 进已回收的组要报错，不装作成功。
	if err := g.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := g.Attach(1); !errors.Is(err, ErrMemGroupClosed) {
		t.Errorf("Attach after Close = %v, want ErrMemGroupClosed", err)
	}
	// 幂等：二次 Close 同结果。
	if err := g.Close(ctx); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
	// 无残组：目录不存在。
	if _, err := os.Stat(g.dir); !os.IsNotExist(err) {
		t.Errorf("组目录 %s 仍在（%v），want 已回收", g.dir, err)
	}
}

// TestMemLimitRunNoResidue（能力探测通过才跑）：Run 全程走完后 cgroup2
// 树里没有 procx-mem-* 残组（取消/结束都回收的收口判据）。
func TestMemLimitRunNoResidue(t *testing.T) {
	if err := MemoryLimitCapable(); err != nil {
		t.Skipf("宿主不能兑现内存上限（%v）", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args, env := helperArgs("touch", "PROCX_MEMLIMIT_MARKER="+filepath.Join(t.TempDir(), "m"))
	if _, err := Run(ctx, os.Args[0], args, WithMemoryLimit(32<<20), WithEnv(env)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	mount, err := findCgroup2Mount()
	if err != nil {
		t.Fatalf("findCgroup2Mount: %v", err)
	}
	for _, dir := range memGroupsUnder(t, mount) {
		t.Errorf("残组未回收：%s", dir)
	}
}

// TestMemLimitSignalSemantics（能力探测通过才跑）：OOM-kill 的进程以
// 信号方式终止——syscall.WaitStatus 层面可辨 Signaled/SIGKILL，
// 「兜底生效」与「自己非零退出」不混。
func TestMemLimitSignalSemantics(t *testing.T) {
	if err := MemoryLimitCapable(); err != nil {
		t.Skipf("宿主不能兑现内存上限（%v）", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	args, env := helperArgs("alloc", "PROCX_MEMLIMIT_ALLOC="+strconv.Itoa(256<<20))
	res, err := Run(ctx, os.Args[0], args, WithMemoryLimit(64<<20), WithEnv(env))
	if err == nil {
		t.Fatalf("超限子进程未被杀")
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v, want *exec.ExitError", err)
	}
	ws, ok := ee.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("退出状态 = %v，want 信号杀（SIGKILL）——被信号杀与非零退出必须可辨", ws)
	}
	if res.ExitCode != -1 {
		t.Fatalf("Result.ExitCode = %d, want -1（stdlib 信号口径，不归一）", res.ExitCode)
	}
}
