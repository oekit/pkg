//go:build !windows

package procx

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

// newCmd 装配 unix 侧的进程组树终止：Setpgid 让子进程自立进程组，
// ctx 到期时 TERM 整组 → 宽限后 KILL 整组（kill(-pgid) 同时覆盖直接子
// 进程与 detach 的孙进程）。
func newCmd(ctx context.Context, name string, args []string, grace time.Duration) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	killGroup := func(sig syscall.Signal) {
		if cmd.Process == nil {
			return
		}
		_ = syscall.Kill(-cmd.Process.Pid, sig) // 负 pid = 整组；组空时 ESRCH，忽略
	}
	cmd.Cancel = func() error {
		killGroup(syscall.SIGTERM)
		// 宽限后 KILL 兜底：即使 Wait 已返回（timer 晚点触发），
		// 对空组 kill 返回 ESRCH，无副作用。
		time.AfterFunc(grace, func() { killGroup(syscall.SIGKILL) })
		return nil
	}
	// 孙进程会继承 stdio 管道：不给 WaitDelay 的话 Wait 会等到所有管道
	// 持有者退出。宽限 + 1s 后强制收口（管道数据截断由调用方容忍）。
	cmd.WaitDelay = grace + time.Second
	return cmd
}
