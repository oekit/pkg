//go:build windows

package procx

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// newCmd 装配 windows 侧的整树终止：无进程组原语（纯 stdlib 不可用
// Job Object——需 x/sys，违反本仓零依赖红线）⇒ 经系统工具
// taskkill /T /F /PID 整树强杀。
//
// 语义降级如实声明：**windows 没有优雅终止档**——grace 参数被忽略，
// 「TERM→宽限→KILL」退化为立即整树终止；调用方不应在 windows 上期待
// 优雅收尾（与包文档一致，不伪装成有）。
func newCmd(ctx context.Context, name string, args []string, grace time.Duration) *exec.Cmd {
	_ = grace // 优雅档在 windows 上不存在（见上）
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		kill := exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid))
		return kill.Run()
	}
	// 整树强制收口（taskkill /T 已覆盖树成员，但管道持有者兜底仍需要）
	cmd.WaitDelay = grace + time.Second
	return cmd
}
