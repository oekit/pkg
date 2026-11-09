//go:build !windows

package procx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oekit/pkg/shellx"
)

// perlShellOK 本组用例的依赖前置：/bin/sh 起脚本，perl 生成确定性字节洪流
// （两平台都自带，见 procx_unix_test.go 的换组先例）。
func perlShellOK(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	if _, err := exec.LookPath("perl"); err != nil {
		t.Skip("no perl")
	}
}

// perlStderrScript 生成「向 stderr 写 n 字节后正常退出」的脚本。
func perlStderrScript(n int) string {
	return `perl -e 'print STDERR "x" x ` + strconv.Itoa(n) + `'`
}

// perlStdoutScript 同上但写 stdout——负对照用：Command 形态里挂停读 writer
// 的流是 Stdout（Stderr 缺省接 /dev/null，写 stderr 会瞬时跑完不卡）。
func perlStdoutScript(n int) string {
	return `perl -e 'print "x" x ` + strconv.Itoa(n) + `'`
}

// pkg#314 验收判据（输出封顶）：子进程往 stderr 写 10MB，cap=64KB ⇒
// 返回 ≤64KB 且 StderrTruncated=true 可被调用方读到；同时子进程**正常退出**
// （err==nil、ExitCode==0，不阻塞在 write 上）。撤修复负对照的分辨力：
//   - 改回无上限采集 ⇒ StderrTruncated=false、len 超限 ⇒ 本用例红；
//   - 改成「读满 cap 就停读」⇒ 子进程卡在 write，Run 只能等 ctx 超时收场
//     ⇒ err 非 nil ⇒ 本用例红（形态实证见 TestNegative_StopReadingBlocksChildWrite）。
func TestRun_OutputLimitTruncatesAndDrains(t *testing.T) {
	perlShellOK(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := Run(ctx, "/bin/sh", []string{"-c", perlStderrScript(10485760)}, WithOutputLimit(64<<10))
	if err != nil {
		t.Fatalf("子进程应正常退出（排空判据——停读实现会在此挂死）: err=%v res=%+v", err, res)
	}
	if res == nil || res.ExitCode != 0 {
		t.Fatalf("ExitCode 应为 0（写完即退，未卡 write）: %+v err=%v", res, err)
	}
	if len(res.Stderr) > 64<<10 {
		t.Fatalf("stderr 应 ≤64KB，实际 %d", len(res.Stderr))
	}
	if len(res.Stderr) != 64<<10 {
		t.Fatalf("64KB 上限应被写满（洪流中途即截断）: %d", len(res.Stderr))
	}
	if !res.StderrTruncated {
		t.Fatal("超限截断必须可判定（StderrTruncated）——截断与本来就这么短必须可区分")
	}
	if res.StdoutTruncated || len(res.Stdout) != 0 {
		t.Fatalf("stdout 未受影响才对: %+v", res)
	}
}

// 边界口径：正好等于上限不算截断（与 httpjson.Truncated 同口径）。
func TestRun_OutputLimitExactBoundary(t *testing.T) {
	perlShellOK(t)
	res, err := Run(context.Background(), "/bin/sh",
		[]string{"-c", perlStderrScript(64 << 10)}, WithOutputLimit(64<<10))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 || len(res.Stderr) != 64<<10 {
		t.Fatalf("恰好写满上限: %+v", res)
	}
	if res.StderrTruncated {
		t.Fatal("正好等于上限不应标记截断")
	}
}

// 缺省（不传 WithOutputLimit）无上限——存量调用语义钉：全量返回、无截断标记。
// 封顶只随选项生效，不悄悄改变既有调用方的采集行为。
func TestRun_NoLimitByDefaultKeepsFullOutput(t *testing.T) {
	perlShellOK(t)
	res, err := Run(context.Background(), "/bin/sh", []string{"-c", perlStderrScript(1048576)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Stderr) != 1048576 || res.StderrTruncated {
		t.Fatalf("缺省应全量采集: len=%d truncated=%v", len(res.Stderr), res.StderrTruncated)
	}
}

// stopAfterWriter 复刻「读满 cap 就停读」的错误采集实现：消费满 cap 后
// **保持管道读端打开但不再消费**（Write 阻塞在 release 上直到测试收场放行
// ——不能改成返回错误：os/exec 的拷贝 goroutine 收到错误会提前关掉读端，
// 子进程以 SIGPIPE 死，那是另一种坏法；「停读」的本形是读端在、消费停，
// 子进程卡在 write 上）。
type stopAfterWriter struct {
	remaining int
	release   chan struct{}
}

func (w *stopAfterWriter) Write(p []byte) (int, error) {
	if w.remaining > 0 {
		if len(p) > w.remaining {
			p = p[:w.remaining]
		}
		w.remaining -= len(p)
		return len(p), nil
	}
	<-w.release // 停读：不再消费，也不关闭
	return 0, errors.New("release 后收场")
}

// mutexBuffer 互斥包装的缓冲：负对照里 stderr 拷贝 goroutine 与主测试
// goroutine 并发访问（观测窗口结束时 Wait 未必已返回，无 happens-before 边），
// 裸 bytes.Buffer 会被 -race 判竞态。
type mutexBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *mutexBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *mutexBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// 负对照（判据分辨力的实证）：停读形态下子进程**交不出正常收场**——要么
// 卡在 write 上挂死，要么被 SIGPIPE 异常终止（不同平台/os/exec 对管道收口
// 的处置差异），二者同样挂死验收判据（TestRun_OutputLimitTruncatesAndDrains
// 的 err==nil/ExitCode==0 只有「边读边截 + 持续排空」才可能给出）。完成标记
// WRITE-DONE 只在 perl **成功写满 10MB** 后才会出现在 stderr——观测窗口内
// 标记缺席即证明停读实现拿不到完整输出；持续排空的采集毫秒级就能写完同样的
// 10MB，窗口有数十倍余量。
func TestNegative_StopReadingBlocksChildWrite(t *testing.T) {
	perlShellOK(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	var errbuf mutexBuffer
	cmd := Command(ctx, "/bin/sh", "-c", perlStdoutScript(10485760)+" && echo WRITE-DONE >&2")
	cmd.Stdout = &stopAfterWriter{remaining: 64 << 10, release: release}
	cmd.Stderr = &errbuf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	werr := make(chan error, 1)
	waitReturned := false
	go func() { werr <- cmd.Wait() }()
	select {
	case e := <-werr:
		waitReturned = true
		t.Logf("子进程在观测窗口内异常终止（SIGPIPE 形态，同为判据失败面）：Wait=%v", e)
	case <-time.After(1500 * time.Millisecond):
		// 观测窗口内未退出：卡在 write 上（挂死形态，判据失败面）。
	}
	if strings.Contains(errbuf.String(), "WRITE-DONE") {
		t.Fatal("停读形态下子进程不应能把 10MB 写完（完成标记不应出现）")
	}
	// 收场：先放行停读 writer（Wait 的拷贝 goroutine 才能收束），再树终止清掉
	// 卡死的子进程——顺序反了 Wait 会被阻塞的 Write 拖住。
	close(release)
	cancel()
	if !waitReturned {
		select {
		case <-werr:
		case <-time.After(15 * time.Second):
			t.Fatal("取消后 Wait 未返回")
		}
	}
}

// pkg#314 验收判据（env 清洗，端到端）：父进程设 PB_ADMIN_PASSWORD=xxx，
// 经 WithEnv 起的子进程 `env` 输出扫不到该值，同时 PATH 必须在。denylist /
// 固定名单变异的负对照由 TestChildEnv_DenyByDefault 承担（同一组敏感名选型）。
func TestRun_WithEnvStripsSecrets(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh")
	}
	const (
		secretPB  = "s3cret-pa55w0rd-value"
		secretAWS = "aws-secret-value"
	)
	t.Setenv("PB_ADMIN_PASSWORD", secretPB)
	t.Setenv("AWS_SECRET_ACCESS_KEY", secretAWS)
	res, err := Run(context.Background(), "/bin/sh", []string{"-c", "env"}, WithEnv(nil))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := string(res.Stdout)
	for _, bad := range []string{secretPB, secretAWS, "PB_ADMIN_PASSWORD", "AWS_SECRET_ACCESS_KEY"} {
		if strings.Contains(out, bad) {
			t.Fatalf("敏感变量泄漏进子进程环境（%q 命中）:\n%s", bad, out)
		}
	}
	if !strings.Contains(out, "PATH=") {
		t.Fatalf("PATH 必须在子进程环境里:\n%s", out)
	}
	if !strings.Contains(out, "GIT_TERMINAL_PROMPT=0") || !strings.Contains(out, "CI=1") {
		t.Fatalf("缺省纪律项缺失:\n%s", out)
	}

	// 字面注入经 Run 同样生效：extra 里的 KEY=VALUE 进子进程环境。
	res2, err := Run(context.Background(), "/bin/sh", []string{"-c", "env"},
		WithEnv([]string{"SWEEP_MARK=passed"}))
	if err != nil {
		t.Fatalf("Run(WithEnv literal): %v", err)
	}
	if !strings.Contains(string(res2.Stdout), "SWEEP_MARK=passed") {
		t.Fatalf("字面注入应进子进程环境:\n%s", res2.Stdout)
	}
}

// 组合场景（三件套同轮）：最小环境 + 输出封顶 + 树终止——后台洪流子进程
// 常驻，ctx 取消整树收场；封顶采集不改变收场语义（取消返回 ctx.Err() 且
// 带部分输出），stderr 钉在上限且 truncated 标记，洪流进程不因采集的存在
// 而存活。
func TestRun_ComboEnvCapTreeKill(t *testing.T) {
	perlShellOK(t)
	pidfile := filepath.Join(t.TempDir(), "flood.pid")
	script := `perl -e 'while(1){print STDERR "x" x 8192}' & echo $! > ` + shellx.Quote(pidfile) + `; wait $!`
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		res *Result
		err error
	}
	ch := make(chan outcome, 1)
	go func() {
		res, err := Run(ctx, "/bin/sh", []string{"-c", script}, WithEnv(nil), WithOutputLimit(64<<10))
		ch <- outcome{res, err}
	}()

	waitForFile(t, pidfile)
	time.Sleep(300 * time.Millisecond) // 洪流起势（远超 64KB 已流经管道）
	cancel()
	select {
	case out := <-ch:
		if out.err == nil || !errors.Is(out.err, context.Canceled) {
			t.Fatalf("ctx 取消应返回 ctx.Err(): %v", out.err)
		}
		if out.res == nil {
			t.Fatal("取消终止也应返回 Result（已缓冲的部分输出）")
		}
		if len(out.res.Stderr) > 64<<10 {
			t.Fatalf("组合场景封顶仍应生效: %d", len(out.res.Stderr))
		}
		if !out.res.StderrTruncated {
			t.Fatal("洪流必然超限——组合场景下截断标记不得丢")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("组合场景 Run 未在取消后返回")
	}
	gc := readPidFile(t, pidfile)
	waitPidGone(t, gc, 5*time.Second) // 树终止仍覆盖洪流进程
}
