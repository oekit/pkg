//go:build linux

package procx

// cgroup 虚拟文件写入统一 0o600：目标文件（memory.max / cgroup.procs 等）由
// 内核创建，perm 参数仅在 O_CREAT 创建时生效——此处取 0600 满足 gosec G306
// 的最小权限判据，不改变任何运行期行为。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// memGroupSeq 保证组名不重复（pid + 递增序号，跨进程重启由 pid 兜）。
var memGroupSeq atomic.Uint64

// createMemGroup 建限额组并把 memory.max 写实（pkg#359 的创建/设置面）。
// cgroup v2 的 no-internal-process 纪律（内核文档：域控制器只能在**没有
// 进程**的 cgroup 的 subtree_control 里启用，真根组豁免）决定了「本进程
// 自己的 cgroup」通常不能直接委托，所以建组按三种策略依次尝试：
//
//  1. **父组已委托**（memory 已在父组 subtree_control——systemd 用户会话/
//     服务单元的常规形态）→ 在父组建同级组（不动任何进程）；
//  2. **本组已委托或可启用**（真根组豁免场景）→ 在本组建子组；
//  3. **委托舞蹈**（只有前两策都失败才做）：把本组的全部进程挪进
//     `procx-rest-*` 叶子（memory.max 默认 max、不设限），本组清空后启用
//     memory 再建子组。副作用如实声明：调用进程与同组进程的 cgroup 变为
//     `<base>/procx-rest-*`（资源限额不变，归属目录变化）；挪不动（无权限/
//     有不可挪的进程）就回滚并如实报错，绝不静默不限。
//
// 任何一步失败都清掉半成品再报错——建组/写限/回读核对是「兑现与否」的
// 实证，缺一步都算没兑现。
func createMemGroup(limitBytes int64) (string, int64, error) {
	base, err := cgroupBase()
	if err != nil {
		return "", 0, err
	}
	// 上限按内核页大小向上对齐（memory.max 以页计），回读按对齐值核对。
	page := int64(os.Getpagesize())
	aligned := limitBytes
	if r := aligned % page; r != 0 {
		aligned += page - r
	}

	var attempts []error

	// 策略 1：父组已委托 → 同级组（不挪进程，最轻的形态）。
	if parent := filepath.Dir(base); parent != base && subtreeHasMemory(parent) {
		dir, err := makeMemGroup(parent, aligned)
		if err == nil {
			return dir, aligned, nil
		}
		attempts = append(attempts, fmt.Errorf("procx: 父组 %s 建组: %w", parent, err))
	}

	// 策略 2：本组已委托 / 可启用（真根组豁免）→ 本组子组。
	if subtreeHasMemory(base) {
		dir, err := makeMemGroup(base, aligned)
		if err == nil {
			return dir, aligned, nil
		}
		attempts = append(attempts, fmt.Errorf("procx: 本组 %s 建组: %w", base, err))
	} else if err := enableMemory(base); err == nil {
		dir, mkErr := makeMemGroup(base, aligned)
		if mkErr == nil {
			return dir, aligned, nil
		}
		attempts = append(attempts, fmt.Errorf("procx: 本组 %s 建组: %w", base, mkErr))
	} else {
		attempts = append(attempts, fmt.Errorf("procx: 本组 %s 启用 memory: %w", base, err))
		// 策略 3：委托舞蹈——本组清空后才能启用 memory。
		if rest, derr := delegateDance(base); derr == nil {
			dir, err := makeMemGroup(base, aligned)
			if err == nil {
				return dir, aligned, nil
			}
			attempts = append(attempts, fmt.Errorf("procx: 舞蹈后 %s 建组（进程已挪至 %s）: %w", base, rest, err))
		} else {
			attempts = append(attempts, fmt.Errorf("procx: 委托舞蹈（%s）: %w", base, derr))
		}
	}
	return "", 0, errors.Join(attempts...)
}

// makeMemGroup 在 parent 下建一个子组并写 memory.max，写后回读核对
// （「写了」不等于「生效」——静默不限的形态就是从跳过回读开始的）。
// 失败时清掉半成品。
func makeMemGroup(parent string, aligned int64) (string, error) {
	dir := filepath.Join(parent, fmt.Sprintf("procx-mem-%d-%d", os.Getpid(), memGroupSeq.Add(1)))
	if err := os.Mkdir(dir, 0o755); err != nil {
		return "", err
	}
	cleanup := func(cause error) (string, error) {
		_ = removeMemGroup(context.Background(), dir)
		return "", cause
	}

	maxFile := filepath.Join(dir, "memory.max")
	if err := os.WriteFile(maxFile, []byte(strconv.FormatInt(aligned, 10)), 0o600); err != nil {
		return cleanup(err)
	}
	back, err := os.ReadFile(maxFile)
	if err != nil {
		return cleanup(err)
	}
	got, err := strconv.ParseInt(strings.TrimSpace(string(back)), 10, 64)
	if err != nil || got != aligned {
		return cleanup(fmt.Errorf("%s 回读不符：写 %d 读 %q", maxFile, aligned, strings.TrimSpace(string(back))))
	}
	// 同时封死换出面（memory.swap.max=0）：只限物理内存的形态会被换页规避
	// ——内存洪峰换出去、memory.current 不越线、OOM-kill 永不触发，沙箱的
	// 兜底变成「限了但不响」。swap.max=0 ⇒ 总内存（含换出）≤ 上限 ⇒ 真超限
	// 必 OOM-kill、信号可辨。内核未编译 swap accounting（文件不存在）时
	// 无换出面可限，如实跳过——memory.max 仍是物理内存上限。
	swapFile := filepath.Join(dir, "memory.swap.max")
	if _, err := os.Stat(swapFile); err == nil {
		if err := os.WriteFile(swapFile, []byte("0"), 0o600); err != nil {
			return cleanup(fmt.Errorf("写 %s 失败: %w", swapFile, err))
		}
		if back, err := os.ReadFile(swapFile); err != nil || strings.TrimSpace(string(back)) != "0" {
			return cleanup(fmt.Errorf("%s 回读不符：写 0 读 %q", swapFile, strings.TrimSpace(string(back))))
		}
	}
	return dir, nil
}

// delegateDance 做委托舞蹈：把 base 的全部进程挪进 base/procx-rest-* 叶子，
// base 清空后启用 memory（no-internal-process 满足）。启用失败则回滚
// （进程挪回、删 rest），不让调用方停在半迁移状态。
func delegateDance(base string) (string, error) {
	rest := filepath.Join(base, fmt.Sprintf("procx-rest-%d-%d", os.Getpid(), memGroupSeq.Add(1)))
	if err := os.Mkdir(rest, 0o755); err != nil {
		return "", err
	}
	pids, err := listGroupProcs(base)
	if err != nil {
		_ = os.Remove(rest)
		return "", err
	}
	moved := make([]int, 0, len(pids))
	for _, pid := range pids {
		if err := os.WriteFile(filepath.Join(rest, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0o600); err != nil {
			// 回滚：已挪的挪回去。
			for _, done := range moved {
				_ = os.WriteFile(filepath.Join(base, "cgroup.procs"), []byte(strconv.Itoa(done)), 0o600)
			}
			_ = os.Remove(rest)
			return "", fmt.Errorf("挪 pid=%d 至 %s: %w", pid, rest, err)
		}
		moved = append(moved, pid)
	}
	if err := enableMemory(base); err != nil {
		for _, done := range moved {
			_ = os.WriteFile(filepath.Join(base, "cgroup.procs"), []byte(strconv.Itoa(done)), 0o600)
		}
		_ = os.Remove(rest)
		return "", err
	}
	return rest, nil
}

// subtreeHasMemory 判 dir 的 subtree_control 里是否已启用 memory
// （启用了，其子组才有 memory.max）。
func subtreeHasMemory(dir string) bool {
	enabled, err := os.ReadFile(filepath.Join(dir, "cgroup.subtree_control"))
	if err != nil {
		return false
	}
	return fieldsContain(string(enabled), "memory")
}

// enableMemory 向 dir 的 subtree_control 写 "+memory"。
func enableMemory(dir string) error {
	return os.WriteFile(filepath.Join(dir, "cgroup.subtree_control"), []byte("+memory"), 0o600) //nolint:gosec // G703:落点是本库自建的 cgroup 目录（NewMemGroup 产出），非外部可控路径
}

// attachMemGroup 把 pid 写进组的 cgroup.procs（设置面）。
func attachMemGroup(dir string, pid int) error {
	procs := filepath.Join(dir, "cgroup.procs")
	if err := os.WriteFile(procs, []byte(strconv.Itoa(pid)), 0o600); err != nil { //nolint:gosec // G703:落点是本库自建 cgroup 组内的 cgroup.procs，非外部可控路径
		return fmt.Errorf("procx: 移入 cgroup 组 %s 失败（pid=%d）: %w", dir, pid, err)
	}
	return nil
}

// removeMemGroup 清组（清理面）：杀组内残余 → 递归删子组 → 删组本身，
// 有限轮询（约 2s）等进程退出腾出空组。删不掉如实报错——残组是实打实的
// 泄漏面，不吞。
func removeMemGroup(ctx context.Context, dir string) error {
	var killErrs []error
	var lastErr error
	for range 201 { // 10ms × 200 ≈ 2s 收敛窗口
		if err := ctx.Err(); err != nil {
			return errors.Join(append(killErrs, lastErr, err)...)
		}
		pids, err := listGroupProcs(dir)
		switch {
		case errors.Is(err, os.ErrNotExist):
			return errors.Join(killErrs...) // 组已回收
		case err != nil:
			lastErr = err
		default:
			for _, pid := range pids {
				if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
					killErrs = append(killErrs, fmt.Errorf("procx: 杀 cgroup 组内残余 pid=%d: %w", pid, err))
				}
			}
		}
		lastErr = removeCgroupTree(dir)
		if lastErr == nil || errors.Is(lastErr, os.ErrNotExist) {
			return errors.Join(killErrs...)
		}
		select {
		case <-ctx.Done():
			return errors.Join(append(killErrs, lastErr, ctx.Err())...)
		case <-time.After(10 * time.Millisecond):
		}
	}
	return errors.Join(append(killErrs, fmt.Errorf("procx: cgroup 组回收失败（%s）: %w", dir, lastErr))...)
}

// removeCgroupTree 深度优先删掉组及其子组（子进程可能在组内又建了子组）。
// 组里还有进程时 rmdir 被内核拒绝，错误原样上抛（调用方轮询重试）。
func removeCgroupTree(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			if err := removeCgroupTree(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return os.Remove(dir)
}

// listGroupProcs 读组的 cgroup.procs（每行一个 pid）。
func listGroupProcs(dir string) ([]int, error) {
	data, err := os.ReadFile(filepath.Join(dir, "cgroup.procs"))
	if err != nil {
		return nil, err
	}
	var pids []int
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("procx: 解析 cgroup.procs 行 %q: %w", line, err)
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

// cgroupBase 定位本进程所在 cgroup v2 组目录：cgroup2 挂载点 + /proc/self/cgroup
// 的统一层级路径（`0::<path>`）。cgroup v1（无统一层级）/ 未挂 cgroup2 都
// 如实报错——降级成不限是本包禁止的形态。
func cgroupBase() (string, error) {
	mount, err := findCgroup2Mount()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", fmt.Errorf("procx: 读 /proc/self/cgroup 失败: %w", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "0::")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		return filepath.Join(mount, rest), nil
	}
	return "", fmt.Errorf("%w: 本进程不在 cgroup v2 统一层级（cgroup v1 或未启用）", ErrMemLimitUnsupported)
}

// findCgroup2Mount 从 /proc/self/mountinfo 找 cgroup2 的挂载点。
func findCgroup2Mount() (string, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", fmt.Errorf("procx: 读 /proc/self/mountinfo 失败: %w", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		// mountinfo：… <mount point> … - <fstype> …；挂载点按八进制转义。
		dash := strings.Index(line, " - ")
		if dash < 0 {
			continue
		}
		fields := strings.Fields(line[:dash])
		if len(fields) < 5 {
			continue
		}
		if fstype := strings.Fields(line[dash+3:]); len(fstype) > 0 && fstype[0] == "cgroup2" {
			return unescapeMountPoint(fields[4]), nil
		}
	}
	return "", fmt.Errorf("%w: 未找到 cgroup2 挂载点（内核未启用 cgroup v2）", ErrMemLimitUnsupported)
}

// unescapeMountPoint 还原 mountinfo 挂载点的八进制转义（\040 空格等）。
func unescapeMountPoint(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			v := 0
			ok := true
			for _, c := range []byte(s[i+1 : i+4]) {
				if c < '0' || c > '7' {
					ok = false
					break
				}
				v = v*8 + int(c-'0')
			}
			if ok {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// fieldsContain 判空白分隔列表里有没有 name（whole-word 匹配）。
func fieldsContain(list, name string) bool {
	for f := range strings.FieldsSeq(list) {
		if f == name {
			return true
		}
	}
	return false
}
