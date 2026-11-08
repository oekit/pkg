package procx

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// defaultGrace 是 TERM→KILL 的宽限时长。
const defaultGrace = 5 * time.Second

type config struct {
	grace time.Duration
}

// RunOption 定制 Run 形态的树终止行为
// （仓规：函数选项一律 XxxOption，禁止裸 Option——多包 co-import 冲突）。
type RunOption func(*config)

// WithGrace 设置优雅终止宽限：先向整组发 TERM，超过宽限仍未退出才 KILL。
// 默认 5s。windows 上无优雅档，本选项退化为立即整树终止（见包文档）。
//
//   - d < 0：立即 panic（仓规总则①，构造期非法值 fail-fast——在选项构造点
//     报错，而不是等到应用点才炸）；
//   - d == 0：合法，语义为「无宽限，TERM 与 KILL 同拍发出」（要立即收场时用）。
func WithGrace(d time.Duration) RunOption {
	if d < 0 {
		panic(fmt.Sprintf("procx: WithGrace: 负宽限期 %v", d))
	}
	return func(c *config) { c.grace = d }
}

// Result 是 Run 的收场结果。
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Run 起子进程并保证 ctx 到期/取消时整棵进程树收场（收集 stdout/stderr）。
// 退出码与 stdlib 同口径；ctx 取消导致的终止返回 ctx.Err()。
//
// 进程退出后（无论正常还是被终止）顺手做一次残留清扫：unix 清掉组内幸存者、
// windows 关闭 Job 句柄触发 KILL_ON_JOB_CLOSE——守护化残留的孙进程不因主
// 进程退出而泄漏（Command 形态没有这个挂点，见包文档）。
func Run(ctx context.Context, name string, args []string, opts ...RunOption) (*Result, error) {
	var c config
	c.grace = defaultGrace
	for _, o := range opts {
		if o != nil { // 仓规总则②：nil 选项 = 显式回退默认值，不算非法
			o(&c)
		}
	}
	cmd, st := newCmd(ctx, name, args, c.grace)
	var mu sync.Mutex
	var stdout, stderr []byte
	cmd.Stdout = writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		stdout = append(stdout, p...)
		return len(p), nil
	})
	cmd.Stderr = writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		stderr = append(stderr, p...)
		return len(p), nil
	})
	if err := cmd.Start(); err != nil {
		st.release()
		if ctx.Err() != nil {
			return nil, ctx.Err() // exec.Start 对已取消 ctx 返回 ctx.Err()
		}
		return nil, err
	}
	st.arm(cmd)
	werr := cmd.Wait()
	st.release()
	st.reap(cmd)
	res := &Result{Stdout: stdout, Stderr: stderr}
	var ee *exec.ExitError
	if errors.As(werr, &ee) {
		res.ExitCode = ee.ExitCode()
	} else if werr == nil {
		res.ExitCode = 0
	}
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	return res, werr
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// Command 把树终止语义挂到 *exec.Cmd 上（不接管 I/O 配置）——现有
// exec.CommandContext 调用点的逐字替换形态。ctx 到期/取消时：unix 先向
// 整个进程组发 TERM 并清扫跨组逃逸后代，宽限（默认 5s）后 KILL 整组；
// windows 用 taskkill /T /F 按父链整树强杀（无优雅档）。
//
// Command 形态没有「退出后残留清扫」挂点（caller 自己调 Wait，exec.Cmd
// 没有 post-exit 钩子），需要该语义用 Run 形态；windows 上 Run 形态另有
// Job Object 整树语义（Start 即挂入，覆盖 Start 后 spawn 的全部后代），
// Command 形态只有取消时点的树强杀，见包文档。
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd, _ := newCmd(ctx, name, args, defaultGrace)
	return cmd
}
