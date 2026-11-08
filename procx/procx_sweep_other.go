//go:build !windows && !linux && !darwin

package procx

import "errors"

// errNoSweep 表示该平台没有进程枚举实现：树终止退化为纯进程组击杀
// （killpg 仍覆盖全部同组后代），跨组逃逸清扫不可用——如实降级，不伪装。
var errNoSweep = errors.New("procx: 该平台无进程枚举实现，跨组逃逸清扫不可用")

func listProcesses() ([]procInfo, error) {
	return nil, errNoSweep
}
