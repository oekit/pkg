//go:build linux

package procx

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// listProcesses 枚举当前全部进程的 (pid, ppid, pgid)：走 /proc/<pid>/stat。
// comm 字段可含空格与括号，按最后一个 ')' 切开再取后续字段
// （括号后：state、ppid、pgrp）。读取失败的条目（进程恰好退出等 procfs 竞态）
// 跳过，不做整体失败——快照语义，够用即可。
func listProcesses() ([]procInfo, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	out := make([]procInfo, 0, len(entries))
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // 非 <pid> 目录（/proc/acpi 等）
		}
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue // 进程恰好退出的竞态，跳过
		}
		i := bytes.LastIndexByte(data, ')')
		if i < 0 {
			continue
		}
		fields := strings.Fields(string(data[i+1:]))
		if len(fields) < 3 {
			continue
		}
		ppid, err1 := strconv.Atoi(fields[1])
		pgid, err2 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, procInfo{pid: pid, ppid: ppid, pgid: pgid})
	}
	return out, nil
}
