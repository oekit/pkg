// Package procx 提供进程树终止原语：ctx 到期/取消时收场的不只是直接子进程，
// 而是整棵子进程树（#284）。
//
// 背景：exec.CommandContext 在 ctx 到期时只杀直接子进程——包装器/脚本型
// 驱动（第三方工具常把真实工作进程 detach 到后台）留下的孙进程是泄漏面，
// 且 ctx 取消管不到。本包把「树终止」语义挂到 *exec.Cmd 上（unix 用进程组，
// windows 用 taskkill /T /F 整树终止）。
//
// 语义降级如实声明：windows 没有优雅终止档（SIGTERM 不存在）——
// WithGrace 在 windows 上退化为立即整树终止，不伪装成有（见 procx_windows.go）。
//
// 标准库外零依赖（组织红线）：不引入 x/sys，windows 整树终止经系统工具
// taskkill 完成。
package procx

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"
)

// defaultGrace 是 TERM→KILL 的宽限时长。
const defaultGrace = 5 * time.Second

type config struct {
	grace time.Duration
}

// Option 定制树终止行为。
type Option func(*config)

// WithGrace 设置优雅终止宽限：先向整组发 TERM，超过宽限仍未退出才 KILL。
// 默认 5s。windows 上无优雅档，本选项退化为立即整树终止（见包文档）。
func WithGrace(d time.Duration) Option {
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
func Run(ctx context.Context, name string, args []string, opts ...Option) (*Result, error) {
	var c config
	c.grace = defaultGrace
	for _, o := range opts {
		o(&c)
	}
	cmd := newCmd(ctx, name, args, c.grace)
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
		return nil, err
	}
	werr := cmd.Wait()
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
// 整个进程组发 TERM，宽限（默认 5s）后 KILL 整组；windows 立即 taskkill
// 整树（无优雅档）。
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	return newCmd(ctx, name, args, defaultGrace)
}
