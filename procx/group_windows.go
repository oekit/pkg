//go:build windows

// group_windows.go —— Windows 进程树落地：Job Object（RunDetailed 执行族）。
//
// Windows 没有 POSIX 的进程组与信号：没有 pgid，没有 SIGTERM/SIGKILL，
// `syscall.Kill` 也不存在。把「整棵树当一个单元终止」这件事在 Windows 上
// 只有一条路——**Job Object**：把子进程放进一个 Job，终止时
// `TerminateJobObject` 杀掉 Job 内的全部进程，孙进程一并收场。
//
// 按 pid 逐个 Kill 拿不到孙进程，而孙进程残留正是 procx 要防的那类缺陷
// （包装器类命令常把真实工作进程守护化到后台）。故这里不用「杀 leader」
// 糊过去。
//
// # 与 Run 形态（procx_windows.go treeState）的分工
//
// treeState 是 Run 形态的平台句柄（Start 后 arm 挂 Job，KILL_ON_JOB_CLOSE
// 崩溃安全）；本文件的 windowsGroup 是 RunDetailed 执行族的托管句柄（
// startAndWait 在 Start 后 attach）。Job 构造复用同一组 kernel32 原语
// （newJobObject 带 KILL_ON_JOB_CLOSE——宿主进程被强杀时 Job 句柄随进程
// 消亡触发清树，崩溃安全）。
//
// # 已知差异（如实声明，不假装与 unix 等价）
//
//   - **没有优雅终止档**。Windows 无 SIGTERM 等价物，故 killGraceful 直接
//     整树强制终止：「TERM → 宽限 3s → KILL」在 Windows 上退化为「立即终止」。
//     killGrace 仍然生效，但它兜的只剩 WaitDelay 的管道收口。
package procx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// windowsGroup 用 Job Object 表达「一棵树」。
type windowsGroup struct {
	cmd *exec.Cmd
	job uintptr // 0 = 尚未托管
}

// newGroupCmd 装配命令。
//
// 刻意**不设** SysProcAttr：Windows 上「整组」由 Job 表达，而
// CREATE_NEW_PROCESS_GROUP 只影响控制台 Ctrl+C 的送达面，与本包
// 「超时/取消后整树收场」的承诺无关。少设一个标志，就少一处需要在
// 无法运行 Windows 的机器上靠想象验证的行为。
func newGroupCmd(cctx context.Context, dir string, argv, env []string) (*exec.Cmd, processGroup) {
	cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = env

	g := &windowsGroup{cmd: cmd}
	cmd.Cancel = func() error {
		err := g.kill(killGraceful)
		if errors.Is(err, errGroupGone) {
			// 子进程恰在 deadline 触发瞬间自然退出：按 os/exec 约定返回
			// ErrProcessDone——否则成功退出会被误当 Cancel 失败（第六轮审计）。
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = killGrace
	return cmd, g
}

// attach 建 Job（KILL_ON_JOB_CLOSE，崩溃安全）并把已启动的子进程放进去。
//
// 必须在 Start 之后调用：AssignProcessToJobObject 要的是进程句柄，而句柄
// 只有 Start 之后才有。attach 失败一律当致命——不托管就兑现不了「整树收场」，
// 此时调用方按「启动失败」处理（startAndWait 会杀掉并 Wait 回收，不留孤儿）。
func (g *windowsGroup) attach() error {
	if g.cmd.Process == nil {
		return errors.New("procx: 子进程未启动，无法托管进程树")
	}
	job, err := newJobObject()
	if err != nil {
		return errors.Join(errors.New("procx: CreateJobObject 失败"), err)
	}
	ph, err := openProcessHandle(g.cmd.Process.Pid)
	if err != nil {
		procCloseHandle.Call(job)
		return errors.Join(errors.New("procx: OpenProcess 失败"), err)
	}
	defer procCloseHandle.Call(ph)
	r, _, aerr := procAssignProcessToJobObject.Call(job, ph)
	if r == 0 {
		procCloseHandle.Call(job)
		return errors.Wrapf(aerr, "procx: AssignProcessToJobObject 失败")
	}
	g.job = job
	return nil
}

// kill 终止整棵树。两档在 Windows 上走同一条路（无优雅终止等价物，见文件头）。
func (g *windowsGroup) kill(mode killMode) error {
	_ = mode
	if g.job == 0 {
		// 尚未托管（Start 失败 / attach 失败）：没有可终止的树。
		return errGroupGone
	}
	r, _, err := procTerminateJobObject.Call(g.job, windowsTerminateExitCode)
	if r == 0 {
		return errors.Wrapf(err, "procx: TerminateJobObject 失败")
	}
	return nil
}

// close 先终止残留（Wait 已返回、但可能仍有孙进程握资源），再释放句柄
// （KILL_ON_JOB_CLOSE 对残余兜底）。
func (g *windowsGroup) close() {
	if g.job == 0 {
		return
	}
	_, _, _ = procTerminateJobObject.Call(g.job, windowsTerminateExitCode)
	_ = procCloseHandle.Call(g.job)
	g.job = 0
}
