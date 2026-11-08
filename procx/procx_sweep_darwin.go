//go:build darwin

package procx

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// sweepTimeout 是跨组逃逸清扫时进程枚举的超时上限：超时即放弃清扫
// （退化为纯进程组击杀），不让取消路径被异常的外部命令挂死。
const sweepTimeout = 5 * time.Second

// listProcesses 枚举当前全部进程的 (pid, ppid, pgid)。
//
// darwin 没有 /proc，stdlib syscall 也没暴露 kinfo_proc 的解析助手
// （sysctl kern.proc.all 的裸字节解析要钉死内核结构体布局，脆），所以走
// ps(1)：macOS 出厂自带、输出格式稳定（`=` 去表头）。只在取消路径上调用，
// 正常路径零开销；带超时上限，防 ps 异常挂死取消路径。
func listProcesses() ([]procInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sweepTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,pgid=").Output()
	if err != nil {
		return nil, err
	}
	procs := make([]procInfo, 0, 128)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		pgid, err3 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		procs = append(procs, procInfo{pid: pid, ppid: ppid, pgid: pgid})
	}
	return procs, nil
}
