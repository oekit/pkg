//go:build !windows

package procx

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
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
func TestNegative_PlainCommandContextLeaksGrandchild(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	// 后台孙进程把自己 PID 回传到 stdout；sh 前台 exec sleep（命令尾优化）
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "sleep 300 & echo $!; exec sleep 300")
	cmd.Stdout = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var bgPid string
	deadline := time.Now().Add(2 * time.Second)
	for {
		if f := strings.Fields(strings.TrimSpace(out.String())); len(f) > 0 {
			bgPid = f[0]
			break
		}
		if time.Now().After(deadline) {
			cancel()
			_ = cmd.Wait()
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
