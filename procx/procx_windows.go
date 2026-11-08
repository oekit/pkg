//go:build windows

package procx

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// windows 侧全部走 syscall.NewLazyDLL 直调 kernel32——零第三方依赖
// （不引 golang.org/x/sys；kernel32 直调是 stdlib syscall 的公开用法）。
// 关键 API：CreateJobObjectW / SetInformationJobObject /
// AssignProcessToJobObject / TerminateJobObject。
var (
	modkernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = modkernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = modkernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = modkernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject       = modkernel32.NewProc("TerminateJobObject")
	procTerminateProcess         = modkernel32.NewProc("TerminateProcess")
	procOpenProcess              = modkernel32.NewProc("OpenProcess")
	procCloseHandle              = modkernel32.NewProc("CloseHandle")
)

const (
	jobObjectBasicLimitInformationClass = 9      // JobObjectBasicLimitInformation
	jobObjectLimitKillOnJobClose        = 0x2000 // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	windowsTerminateExitCode            = 1
	// AssignProcessToJobObject 的文档要求最低访问权：
	// PROCESS_SET_QUOTA (0x0100) | PROCESS_TERMINATE (0x0001)。
	processAccessSetQuotaTerminate = 0x0100 | 0x0001
)

// treeState 是 windows 侧的平台句柄（Run 形态专属）：Start 后 arm 把子进程
// 挂入 Job，此后子进程 spawn 的一切后代默认同属该 Job——TerminateJobObject
// 一竿子清整树，宿主进程崩溃时句柄随进程消亡同样触发清树（KILL_ON_JOB_CLOSE，
// 崩溃安全）。Cancel 回调（os/exec 的 watchCtx goroutine）与 Run 主流程并发
// 访问，mu 保护。
type treeState struct {
	mu       sync.Mutex
	job      uintptr
	assigned bool
}

// jobObjectBasicLimitInformation 对齐 JOBOBJECT_BASIC_LIMIT_INFORMATION
// （Go 字段序与 C 布局一致，含自然对齐的填充）。
type jobObjectBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

// newJobObject 建一个带 KILL_ON_JOB_CLOSE 的 Job Object。
func newJobObject() (uintptr, error) {
	h, _, callErr := procCreateJobObjectW.Call(0, 0)
	if h == 0 {
		return 0, fmt.Errorf("procx: CreateJobObjectW: %w", callErr)
	}
	limit := jobObjectBasicLimitInformation{LimitFlags: jobObjectLimitKillOnJobClose}
	r, _, callErr := procSetInformationJobObject.Call(
		h,
		jobObjectBasicLimitInformationClass,
		uintptr(unsafe.Pointer(&limit)), //nolint:gosec // Windows API 要结构体指针，stdlib syscall 惯用法
		unsafe.Sizeof(limit),
	)
	if r == 0 {
		procCloseHandle.Call(h)
		return 0, fmt.Errorf("procx: SetInformationJobObject: %w", callErr)
	}
	return h, nil
}

// openProcessHandle 按 pid 打开目标进程句柄（os.Process 不暴露句柄）。
// 返回的句柄由调用方负责 CloseHandle。
func openProcessHandle(pid int) (uintptr, error) {
	h, _, callErr := procOpenProcess.Call(processAccessSetQuotaTerminate, 0, uintptr(pid))
	if h == 0 {
		return 0, fmt.Errorf("procx: OpenProcess(pid=%d): %w", pid, callErr)
	}
	return h, nil
}

// newCmd 装配 windows 侧的整树终止。
//
//   - Run 形态（arm 被调用）：Start 后立即把子进程挂入 Job（Start 与挂入之间
//     有微秒级窗口，窗口内已 spawn 且 detach 的孙进程不被 Job 覆盖——如实
//     声明），取消时 TerminateJobObject 整树终止；进程退出后关 Job 句柄，
//     KILL_ON_JOB_CLOSE 清树（含正常退出后的守护化残留）。
//   - Command 形态（arm 不会被调用）：exec.Cmd 没有 Start 之后的钩子，
//     无法预先挂 Job ⇒ 取消时经系统工具 taskkill /T /F /PID 按父链快照
//     整树强杀（换组/setsid 的后代只要是子进程就会被 /T 走到）。
//
// 语义降级如实声明：**windows 没有优雅终止档**——grace 参数被忽略，
// 「TERM→宽限→KILL」退化为立即整树终止；调用方不应在 windows 上期待
// 优雅收尾（与包文档一致，不伪装成有）。
func newCmd(ctx context.Context, name string, args []string, grace time.Duration) (*exec.Cmd, *treeState) {
	_ = grace // 优雅档在 windows 上不存在（见上）
	if ctx == nil {
		ctx = context.Background() // 仓规总则②：nil = 显式回退默认值
	}
	st := &treeState{}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Cancel = func() error { return st.cancel(cmd) }
	// 整树强制收口（Job/taskkill 已覆盖树成员，但管道持有者兜底仍需要）
	cmd.WaitDelay = grace + time.Second
	return cmd, st
}

// arm 把子进程挂入 Job（Run 形态专属，Start 后立即调用）。
func (st *treeState) arm(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	job, err := newJobObject()
	if err != nil {
		return // Job 建不出来：取消路径会退化为 taskkill 整树
	}
	ph, err := openProcessHandle(cmd.Process.Pid)
	if err != nil {
		procCloseHandle.Call(job)
		return
	}
	defer procCloseHandle.Call(ph)
	r, _, _ := procAssignProcessToJobObject.Call(job, ph)
	st.mu.Lock()
	st.job = job
	st.assigned = r != 0
	st.mu.Unlock()
}

// release 关闭 Job 句柄：KILL_ON_JOB_CLOSE 清树——守护化残留的后代
// （含正常退出后的）一并收场（Run 形态专属挂点）。
func (st *treeState) release() {
	st.mu.Lock()
	job := st.job
	st.job = 0
	st.mu.Unlock()
	if job != 0 {
		procCloseHandle.Call(job)
	}
}

// reap：windows 的退出后清树由 release 关 Job 句柄触发，无额外动作。
func (st *treeState) reap(cmd *exec.Cmd) {}

// cancel 是 os/exec Cancel 回调：Run 形态（已挂 Job）走 TerminateJobObject；
// Command 形态（arm 未被调用）退化为 taskkill /T /F 按父链快照整树强杀；
// Job 已建但挂入失败时兜底 TerminateProcess 直接子进程。
func (st *treeState) cancel(cmd *exec.Cmd) error {
	st.mu.Lock()
	job := st.job
	assigned := st.assigned
	st.mu.Unlock()

	terminateDirect := func() error {
		if cmd.Process == nil {
			return nil
		}
		ph, err := openProcessHandle(cmd.Process.Pid)
		if err != nil {
			return err
		}
		defer procCloseHandle.Call(ph)
		r, _, callErr := procTerminateProcess.Call(ph, windowsTerminateExitCode)
		if r == 0 {
			return fmt.Errorf("procx: TerminateProcess(pid=%d): %w", cmd.Process.Pid, callErr)
		}
		return nil
	}

	if job == 0 || !assigned {
		// Command 形态（无 Job）：taskkill /T /F 按父链快照整树强杀。
		// /T 沿父链走树，换组/setsid 的后代只要是子进程就会被走到。
		if job == 0 {
			if cmd.Process == nil {
				return nil
			}
			kill := exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid)) //nolint:gosec // 参数全为常量+本包自管 pid
			return kill.Run()
		}
		// Job 建成了但挂入失败：至少杀直接子进程。
		return terminateDirect()
	}
	r, _, callErr := procTerminateJobObject.Call(job, windowsTerminateExitCode)
	if r == 0 {
		return fmt.Errorf("procx: TerminateJobObject: %w", callErr)
	}
	return nil
}
