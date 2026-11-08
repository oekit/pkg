//go:build !windows

package procx

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// treeState 是 unix 侧的平台句柄：进程组由 SysProcAttr{Setpgid} 在 Start 时
// 就位（arm/release 无额外动作，见 procx.go 的统一调用点）。
type treeState struct{}

func (*treeState) arm(*exec.Cmd) {}
func (*treeState) release()      {}

// newCmd 装配 unix 侧的进程组树终止：Setpgid 让子进程自立进程组，
// ctx 到期时 TERM 整组 + 清扫跨组逃逸后代 → 宽限轮询（树死干净即收手）→
// KILL 整组（kill(-pgid) 同时覆盖直接子进程与未换组的孙进程）。
func newCmd(ctx context.Context, name string, args []string, grace time.Duration) (*exec.Cmd, *treeState) {
	if ctx == nil {
		ctx = context.Background() // 仓规总则②：nil = 显式回退默认值
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		terminateTree(cmd, grace)
		return nil
	}
	// 孙进程会继承 stdio 管道：不给 WaitDelay 的话 Wait 会等到所有管道
	// 持有者退出。宽限 + 1s 后强制收口（管道数据截断由调用方容忍）。
	cmd.WaitDelay = grace + time.Second
	return cmd, &treeState{}
}

// terminateTree 执行两档收场：
//
//	TERM 档：kill(-pgid) 整组 + 跨组逃逸后代（守护化/setsid 的真孙进程，
//	         按父链快照找出后逐个补发，连带各自进程组）
//	宽限轮询：每 10ms 探一次「树死干净没有」，死干净立即收手——
//	         优雅退出的进程不会被 KILL，也不会空等满宽限
//	KILL 档：宽限耗尽仍有幸存者，重枚举逃逸者后整组 + 全部逃逸者 SIGKILL
//
// 快照语义的边界（如实声明）：父链中途断掉的逃逸者（父进程先退出被
// reparent）不可达——这是不需要 subreaper 就能做到的全部；守护化孙进程的
// 典型场景（包装器常驻、工作进程 setsid detach）父链是完整的，覆盖得到。
func terminateTree(cmd *exec.Cmd, grace time.Duration) {
	p := cmd.Process
	if p == nil {
		return
	}
	root := p.Pid
	// decorate 恒 Setpgid ⇒ 子进程自建组（pgid=pid）；防御性读取真实 pgid，
	// 万一调用方改写了 SysProcAttr 也不把信号发进错误的组。组级击杀只在
	// 「本包亲手建组」时启用，绝不 killpg 到调用方自己的组。
	rootPgid := root
	if g, err := syscall.Getpgid(root); err == nil {
		rootPgid = g
	}
	groupKill := cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid

	escapees := collectEscapees(root, rootPgid)

	// TERM 档：先扫逃逸者再打整组，顺序无关——逃逸者名单在快照里已拿到。
	deliverAll(rootPgid, escapees, groupKill, syscall.SIGTERM)

	if waitTreeDead(rootPgid, escapees, grace) {
		return
	}

	// KILL 档：宽限期内被无视 TERM 的父进程可能又 spawn 了新逃逸者，重枚举并集。
	all := mergeTargets(escapees, collectEscapees(root, rootPgid))
	deliverAll(rootPgid, all, groupKill, syscall.SIGKILL)
}

// reap 在进程退出后清一次组内幸存者：同组后代随 fork 留在组里，根进程退出
// 不会带走它们，按泄漏论处、无宽限直接 KILL（Run 形态专属挂点；
// 跨组逃逸者此时父链已断，枚举不可达——如实局限）。
func (*treeState) reap(cmd *exec.Cmd) {
	p := cmd.Process
	if p == nil || cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		return
	}
	if groupExists(p.Pid) { // decorate 恒 Setpgid ⇒ pgid=pid
		_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
	}
}

// deliverAll 给「整组 + 全部逃逸者」发信号。
func deliverAll(rootPgid int, targets []procInfo, groupKill bool, sig syscall.Signal) {
	for _, t := range targets {
		if pidExists(t.pid) {
			_ = syscall.Kill(t.pid, sig)
		}
		deliverGroup(t.pgid, rootPgid, sig, groupKill)
	}
	if groupKill {
		_ = syscall.Kill(-rootPgid, sig) // 组空时 ESRCH，忽略
	}
}

// deliverGroup 给逃逸者所在的进程组发信号。三重护栏：不打根组（已有整组
// 击杀覆盖）、不打本包自己的组（自杀）、仅在确认「组是本包经 Setpgid 建的」
// 时启用组级击杀。
func deliverGroup(pgid, rootPgid int, sig syscall.Signal, groupKill bool) {
	if !groupKill || pgid == rootPgid || pgid == syscall.Getpgrp() {
		return
	}
	_ = syscall.Kill(-pgid, sig)
}

// waitTreeDead 在宽限期内每 10ms 轮询一次，树死干净立即返回 true（省掉
// 无谓的 KILL）；宽限耗尽仍有幸存者返回 false。grace<=0 表示「无宽限」，
// 直接进入 KILL 档。
func waitTreeDead(rootPgid int, targets []procInfo, grace time.Duration) bool {
	if grace <= 0 {
		return false
	}
	deadline := time.Now().Add(grace)
	for {
		if treeDead(rootPgid, targets) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// treeDead 判「树死干净」：根组空 + 全部逃逸者及其组都不存在。
func treeDead(rootPgid int, targets []procInfo) bool {
	if groupExists(rootPgid) {
		return false
	}
	for _, t := range targets {
		if pidExists(t.pid) {
			return false
		}
		if t.pgid != rootPgid && groupExists(t.pgid) {
			return false
		}
	}
	return true
}

// pidExists 用 kill(pid, 0) 判存在性：nil 或 EPERM（进程在、无权限送信号）
// 都算存在，只有 ESRCH 才是不存在。
func pidExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func groupExists(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// mergeTargets 按 pid 去重合并两批逃逸者（KILL 档的旧名单 ∪ 重枚举名单）。
func mergeTargets(a, b []procInfo) []procInfo {
	if len(b) == 0 {
		return a
	}
	seen := make(map[int]bool, len(a)+len(b))
	out := make([]procInfo, 0, len(a)+len(b))
	for _, t := range append(append([]procInfo{}, a...), b...) {
		if !seen[t.pid] {
			seen[t.pid] = true
			out = append(out, t)
		}
	}
	return out
}

// procInfo 是进程枚举快照里的一行。
type procInfo struct {
	pid  int
	ppid int
	pgid int
}

// collectEscapees 从进程快照里找出「根进程的后代中已脱离进程组 rootPgid 的
// 逃逸者」（守护化/setsid 的孙进程）。按 ppid 父链 BFS——父链中途断掉的
// （父进程先退出被 reparent）不可达，快照语义如实局限。
func collectEscapees(root, rootPgid int) []procInfo {
	procs, err := listProcesses()
	if err != nil || len(procs) == 0 {
		return nil
	}
	byPPid := make(map[int][]procInfo, len(procs))
	for _, p := range procs {
		byPPid[p.ppid] = append(byPPid[p.ppid], p)
	}
	var out []procInfo
	queue := []int{root}
	seen := map[int]bool{root: true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range byPPid[cur] {
			if seen[c.pid] {
				continue
			}
			seen[c.pid] = true
			if c.pgid != rootPgid {
				out = append(out, c)
			}
			queue = append(queue, c.pid)
		}
	}
	return out
}
