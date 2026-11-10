//go:build linux

package procx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// memCgroupLeftovers 列出本进程名下的残留内存组（组目录名含 pid）——
// 组的落位点是「本进程组向上第一个可启用 memory 的祖先」，逐挂载逐级扫。
func memCgroupLeftovers(t *testing.T) []string {
	t.Helper()
	rel, ok := selfCgroupRel()
	if !ok {
		t.Fatal("no cgroup v2 self rel")
	}
	mounts, err := cgroup2Mounts()
	if err != nil {
		t.Fatal(err)
	}
	prefix := memCgroupPrefix + strconv.Itoa(os.Getpid()) + "-"
	seen := map[string]bool{}
	var out []string
	for _, m := range mounts {
		for dir := filepath.Join(m, rel); ; dir = filepath.Dir(dir) {
			if entries, err := os.ReadDir(dir); err == nil {
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), prefix) && !seen[e.Name()] {
						seen[e.Name()] = true
						out = append(out, e.Name())
					}
				}
			}
			if filepath.Clean(dir) == filepath.Clean(m) {
				break
			}
		}
	}
	return out
}

// TestMemoryLimitOOMKill 钉住兜底生效的可辨语义：超限子进程被内核 OOM-kill
// ⇒ 父侧看到「被信号杀」（ExitCode -1），与「自己非零退出」可辨。负对照：
// 不设限同程序跑过（正常退出）。
func TestMemoryLimitOOMKill(t *testing.T) {
	if err := MemoryLimitCapable(); err != nil {
		t.Skipf("环境不能兑现内存上限：%v", err)
	}
	sentinel := filepath.Join(t.TempDir(), "done")
	ctx := context.Background()
	args := []string{"-test.run=TestMemoryLimitHelperProcess"}
	env := []string{
		"PROCX_MEMLIMIT_HELPER=1",
		"PROCX_MEMLIMIT_SENTINEL=" + sentinel,
		"PROCX_MEMLIMIT_ALLOC_MB=256",
	}
	// 负对照：不设限时同一程序跑过
	if _, err := Run(ctx, os.Args[0], args, WithEnv(env)); err != nil {
		t.Fatalf("负对照（无上限）Run error: %v", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("负对照未执行（哨兵缺失）：%v", err)
	}
	if err := os.Remove(sentinel); err != nil {
		t.Fatal(err)
	}

	// 64MB 帽下分配 256MB 物理内存 ⇒ 内核 OOM-kill
	res, err := Run(ctx, os.Args[0], args, WithEnv(env), WithMemoryLimit(64<<20))
	if err == nil {
		t.Fatalf("超限子进程正常返回（err=nil）——上限未生效？res=%+v", res)
	}
	if res == nil || res.ExitCode != -1 {
		t.Errorf("被信号杀 vs 自己非零退出不可辨：res=%+v err=%v（want ExitCode -1）", res, err)
	}
	if left := memCgroupLeftovers(t); len(left) > 0 {
		t.Errorf("结束后有残组：%v", left)
	}
}

// TestMemoryLimitCleanupAfterCancel 钉住取消后组回收、无残组。
func TestMemoryLimitCleanupAfterCancel(t *testing.T) {
	if err := MemoryLimitCapable(); err != nil {
		t.Skipf("环境不能兑现内存上限：%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	env := []string{
		"PROCX_MEMLIMIT_HELPER=1",
		"PROCX_MEMLIMIT_SENTINEL=" + filepath.Join(t.TempDir(), "s"),
		"PROCX_MEMLIMIT_SLEEP_MS=10000",
	}
	_, err := Run(ctx, os.Args[0], []string{"-test.run=TestMemoryLimitHelperProcess"},
		WithEnv(env), WithMemoryLimit(64<<20), WithGrace(100*time.Millisecond))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run err = %v, want context.Canceled", err)
	}
	if left := memCgroupLeftovers(t); len(left) > 0 {
		t.Errorf("取消后有残组：%v", left)
	}
}
