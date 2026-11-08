//go:build !windows

package procx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/oekit/pkg/shellx"
)

// groupMembers 用 pgrep -g 列出进程组内所有存活成员（树的超集口径）。
func groupMembers(t *testing.T, pgid int) []string {
	t.Helper()
	out, err := exec.Command("pgrep", "-g", strconv.Itoa(pgid)).Output()
	if err != nil {
		return nil // 组空（pgrep 无命中退出非零）
	}
	return strings.Fields(string(out))
}

// waitGroupEmpty 等进程组清空（给 TERM/KILL 传播留窗口）。
func waitGroupEmpty(t *testing.T, pgid int) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if len(groupMembers(t, pgid)) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("进程组 %d 仍有存活成员: %v", pgid, groupMembers(t, pgid))
}

// 验收 1（孙进程正对照）：父进程 fork 后台孙进程后常驻；ctx 取消后整组清空。
func TestRun_KillsTreeOnContextCancel(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := Command(ctx, "/bin/sh", "-c", "sleep 300 & sleep 300") // 孙 + 直接子
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := cmd.Process.Pid
	time.Sleep(300 * time.Millisecond) // 让孙进程落生
	if members := groupMembers(t, pgid); len(members) < 2 {
		t.Fatalf("前置失败：组内成员应 ≥2（父+孙），实际 %v", members)
	}
	cancel()
	cmdWaitDone := make(chan struct{})
	go func() { _ = cmd.Wait(); close(cmdWaitDone) }()
	select {
	case <-cmdWaitDone:
	case <-time.After(10 * time.Second):
		t.Fatal("Wait 未在取消后返回")
	}
	waitGroupEmpty(t, pgid)
}

// 验收 2（孙进程负对照）：故意不用本包（无 Setpgid、无组杀）⇒ 同场景
// 后台孙进程**残留**——证明验收 1 的判据真的在量「树」，不是量「直接子进程」。
// PID 经临时文件回传（孙进程继承 stdout 管道 ⇒ 进程内 buffer 有数据竞争，
// -race 必抓——这也正是孙进程继承管道的实证之一）。
func TestNegative_PlainCommandContextLeaksGrandchild(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	pidFile := t.TempDir() + "/bgpid"
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c",
		"sleep 300 & echo $! > "+pidFile+"; exec sleep 300")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var bgPid string
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			if f := strings.Fields(strings.TrimSpace(string(data))); len(f) > 0 {
				bgPid = f[0]
				break
			}
		}
		if time.Now().After(deadline) {
			cancel()
			t.Skip("未捕捉到孙进程 PID，跳过负对照")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// 直接子进程被杀；无组杀 ⇒ 后台孙进程残留（泄漏复现）。
	// 注意：孙进程继承 stdout 管道 ⇒ 此处 cmd.Wait() 会阻塞到孙退出（300s）
	// ——这正是本包用 WaitDelay 解决的管道持有问题；负对照刻意不 Wait。
	cancel()
	time.Sleep(500 * time.Millisecond)
	alive := exec.Command("kill", "-0", bgPid).Run() == nil
	_ = exec.Command("kill", "-9", bgPid).Run() // 清场：只杀本测试回传的 PID
	if !alive {
		t.Skip("孙进程未残留，负对照无从验证")
	}
}

// 验收 3（优雅档）：进程收到 TERM 自行清理后退出 ⇒ 不得被 KILL。
func TestRun_GracefulExitOnTerm(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	ctx, cancel := context.WithCancel(context.Background())
	// trap TERM 后立刻退 0 并写标记文件
	mark := t.TempDir() + "/graceful"
	cmd := Command(ctx, "/bin/sh", "-c",
		"trap 'echo done > "+mark+"; exit 0' TERM; sleep 300")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // 让 sh 完成 trap 安装，避免 TERM 抢在前面
	cancel()
	_ = cmd.Wait()
	// 标记文件在 = 进程走的是 trap 路径（TERM 收到、自行清理），不是 KILL
	data, err := os.ReadFile(mark)
	if err != nil || !bytes.Equal(bytes.TrimSpace(data), []byte("done")) {
		t.Fatal("进程未被优雅终止（trap 未执行）——WithGrace 档失效")
	}
}

// 验收 4（超时档）：忽略 TERM 的进程 ⇒ 宽限后被 KILL（ExitCode=-1 信号致死）。
func TestRun_KillsAfterGraceWhenTermIgnored(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(1200*time.Millisecond, cancel) // 驱动取消（Run 会阻塞到收场）
	start := time.Now()
	res, err := Run(ctx, "/bin/sh", []string{"-c", "trap '' TERM; sleep 300"},
		WithGrace(700*time.Millisecond))
	if err == nil {
		t.Fatal("ctx 取消的终止应返回 ctx.Err()")
	}
	elapsed := time.Since(start)
	// KILL 致死：sh 被信号杀（ExitCode -1）；宽限必须被遵守（≥700ms），且上限有界
	if res == nil || res.ExitCode != -1 {
		t.Fatalf("TERM 被忽略的进程应被 KILL（ExitCode=-1），实际 %+v err=%v", res, err)
	}
	if elapsed < 700*time.Millisecond {
		t.Fatalf("KILL 发生早于宽限（%v）——WithGrace 未被遵守", elapsed)
	}
	if elapsed > 8*time.Second {
		t.Fatalf("收场耗时 %v，超出宽限+传播窗口太多", elapsed)
	}
}

// ---- 以下为 pkg#284 验收判据的补强用例：跨组逃逸孙进程、Command 形态、
// ---- Option 契约与正常退出残留清扫（原 4 判据用例保持不动）----

// pidAlive 用 kill(pid, 0) 判存在性：nil=存在，EPERM=存在但无权限（也算存在），
// 只有 ESRCH 才是「不存在」。不用外部 pgrep（CI 上多一层子进程噪声）。
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待 %s 出现超时（5s）", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func readPidFile(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 pidfile %s: %v", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		t.Fatalf("pidfile %s 内容非法: %q", path, string(b))
	}
	return pid
}

func waitPidGone(t *testing.T, pid int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for pidAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("判据失败：pid %d 在 %v 后仍存活", pid, d)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// readPgid 读一个进程当前所属进程组：linux 走 /proc/<pid>/stat（第 5 字段
// pgrp），darwin 无 /proc 走 ps(1)。仅供测试断言使用。
func readPgid(t *testing.T, pid int) int {
	t.Helper()
	switch runtime.GOOS {
	case "linux":
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			t.Fatalf("读 /proc/%d/stat: %v", pid, err)
		}
		i := bytes.LastIndexByte(b, ')')
		if i < 0 {
			t.Fatalf("/proc/%d/stat 形状非法", pid)
		}
		fields := strings.Fields(string(b[i+1:]))
		// 括号后：fields[0]=state、fields[1]=ppid、fields[2]=pgrp
		if len(fields) < 3 {
			t.Fatalf("/proc/%d/stat 字段不足", pid)
		}
		v, err := strconv.Atoi(fields[2])
		if err != nil {
			t.Fatalf("解析 pgrp: %v", err)
		}
		return v
	case "darwin":
		out, err := exec.Command("ps", "-o", "pgid=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			t.Fatalf("ps 读 pid=%d 的 pgid: %v", pid, err)
		}
		v, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil {
			t.Fatalf("解析 ps pgid 输出 %q: %v", string(out), err)
		}
		return v
	default:
		t.Skipf("readPgid 未适配 %s", runtime.GOOS)
		return 0
	}
}

// waitOwnGroup 等孙进程真正进入自建进程组。perl 在 fork 与 setpgrp 之间有
// 窗口，过早读 pgid 会读到父 shell 的组，造成误判——轮询到 pgid==pid 为止。
func waitOwnGroup(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if readPgid(t, pid) == pid {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pid %d 未进入自建进程组（跨组前置自证失败）", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// crossGroupGrandchildScript 生成「真孙进程」脚本：后台 perl setpgrp 换组后
// exec sleep 300（孙进程跨组逃逸、父 sh 常驻 wait），孙 pid 落 pidfile。
// macOS 没有 setsid(1)，perl 是两平台（darwin/linux）都自带的最稳换组手段。
func crossGroupGrandchildScript(t *testing.T) (script, pidfile string) {
	t.Helper()
	pidfile = filepath.Join(t.TempDir(), "grandchild.pid")
	script = fmt.Sprintf(`perl -e 'setpgrp(0,0); exec "sleep", "300"' & echo $! > %s; wait`, shellx.Quote(pidfile))
	return script, pidfile
}

// 验收 1（正对照，跨组加强）：孙进程 setpgrp 换出进程组（组击杀够不到），
// ctx 取消后必须仍然死亡——证明父链清扫真的追到了「逃出组」的守护化进程。
// 纯 killpg 口径下本用例失败（孙进程残留），这正是它要钉住的行为。
func TestRun_CancelKillsCrossGroupGrandchild(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	script, pidfile := crossGroupGrandchildScript(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type outcome struct {
		res *Result
		err error
	}
	ch := make(chan outcome, 1)
	go func() {
		res, err := Run(ctx, "/bin/sh", []string{"-c", script})
		ch <- outcome{res, err}
	}()

	waitForFile(t, pidfile)
	gc := readPidFile(t, pidfile)
	waitOwnGroup(t, gc) // 前置自证：孙进程真的换了组（判据量的是「跨组逃逸」）
	if !pidAlive(gc) {
		t.Fatal("前置自证失败：孙进程未存活")
	}
	time.Sleep(100 * time.Millisecond) // 让启动稳定后再取消

	cancel()
	select {
	case out := <-ch:
		if out.err == nil || !errors.Is(out.err, context.Canceled) {
			t.Fatalf("ctx 取消导致的终止应返回 ctx.Err()，got err=%v", out.err)
		}
		if out.res == nil {
			t.Fatal("取消终止也应返回 Result（已缓冲的部分输出）")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run 在 ctx 取消后 15s 未返回")
	}
	waitPidGone(t, gc, 5*time.Second) // 判据：跨组孙进程不存在
}

// Command 形态 drop-in：echo 逐字可替换。
func TestCommand_DropInEcho(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := Command(ctx, "/bin/sh", "-c", "echo ok")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CombinedOutput: %v", err)
	}
	if strings.TrimSpace(string(out)) != "ok" {
		t.Fatalf("期望输出 ok，got %q", string(out))
	}
}

// Command 形态的跨组孙进程清树（unix）。
func TestCommand_CancelKillsCrossGroupGrandchild(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	script, pidfile := crossGroupGrandchildScript(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := Command(ctx, "/bin/sh", "-c", script)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, pidfile)
	gc := readPidFile(t, pidfile)
	waitOwnGroup(t, gc)
	time.Sleep(100 * time.Millisecond)

	cancel()
	werr := make(chan error, 1)
	go func() { werr <- cmd.Wait() }()
	select {
	case <-werr:
	case <-time.After(15 * time.Second):
		t.Fatal("Command 形态 Wait 在 ctx 取消后 15s 未返回")
	}
	waitPidGone(t, gc, 5*time.Second)
}

// Run 正常退出后的组内残留清扫：主进程已退出，组内幸存的孙进程 = 泄漏，
// 必须被收场（此前口径「不在承诺内」，本用例把承诺收进来）。
func TestRun_ReapsSameGroupSurvivorsAfterNormalExit(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	pidfile := filepath.Join(t.TempDir(), "survivor.pid")
	script := fmt.Sprintf(`sleep 300 >/dev/null 2>&1 & echo $! > %s; exit 0`, shellx.Quote(pidfile))
	res, err := Run(context.Background(), "/bin/sh", []string{"-c", script})
	if err != nil {
		t.Fatalf("正常退出应无错误（残留清扫不影响退出语义）: %v", err)
	}
	if res == nil || res.ExitCode != 0 {
		t.Fatalf("ExitCode 应为 0，got %+v err=%v", res, err)
	}
	gc := readPidFile(t, pidfile)
	waitPidGone(t, gc, 5*time.Second)
}

// 仓规总则①：负宽限期构造期 fail-fast（在选项构造点 panic，不等到应用点）。
func TestWithGrace_NegativePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("负宽限期应构造期 panic（仓规总则① fail-fast）")
		}
	}()
	WithGrace(-time.Second)
}

// 仓规总则②：nil 选项 = 显式回退默认值，不算非法。
func TestRun_NilOptionFallsBackToDefaults(t *testing.T) {
	res, err := Run(context.Background(), "/bin/sh", []string{"-c", "echo hi"}, nil)
	if err != nil {
		t.Fatalf("nil 选项不应报错: %v", err)
	}
	if res == nil || res.ExitCode != 0 || strings.TrimSpace(string(res.Stdout)) != "hi" {
		t.Fatalf("结果不符: %+v", res)
	}
}

// 预取消 ctx：Start 不会发生，返回 (nil, ctx.Err())。
func TestRun_PreCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Run(ctx, "/bin/sh", []string{"-c", "echo nope"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("预取消 ctx 的 Run 应返回 context.Canceled，got %v", err)
	}
	if res != nil {
		t.Fatalf("未启动的进程应无 Result，got %+v", res)
	}
}
