package procx

import (
	"errors"
	"fmt"
)

// ErrMemoryLimitUnsupported 表示当前平台/环境**不能兑现**子进程内存上限
// （非 linux、cgroup v1、未挂载/未委托 cgroup v2 memory 控制器）。
// 兑现纪律是 fail closed（pkg#359）：不能兑现就报错拒绝执行，绝不静默降级
// 成不限——「上限没生效但调用看起来成功」与「兜底已兑现」的声明之间没有
// 信号，比不实现更坏。
var ErrMemoryLimitUnsupported = errors.New("procx: memory limit cannot be honored on this platform/environment")

// memGroup 是子进程内存上限组的平台句柄：linux 为 cgroup v2 组，其余平台
// 建不出来（setupMemoryLimit 直接报错）。
type memGroup interface {
	// attach 把子进程移入组（此后其物理内存计入帽）。
	attach(pid int) error
	// destroy 回收组（Run 结束/取消都调用；空组才删得掉，不留残组）。
	destroy() error
}

// WithMemoryLimit 给 Run 的子进程设**物理内存**上限（字节）。linux 走
// cgroup v2 memory.max：帽的是 RSS + page cache，Go runtime 的虚拟 arena
// 预留不落地、不占配额（RLIMIT_AS/RLIMIT_DATA 限的是虚拟地址空间——帽小了
// runtime 自己起不来、帽大了兜不住，是错的工具）。swap 同步钉 0
// （memory.swap.max）：否则有 swap 的机器上超限是「狂换页继续跑」而不是
// 「超限被杀」，OOM 语义落空。超限由内核 OOM-killer SIGKILL ⇒
// [Result].ExitCode = -1（被信号杀），与「自己非零退出」可辨。
//
// 兑现纪律 fail closed（pkg#359）：建不出组/写不进 memory.max（无权限、
// cgroup v1、非 linux）→ Run 报错**拒绝执行**，不静默继续；兑现与否先问
// [MemoryLimitCapable]。组回收：Run 结束/取消都清组，不留残组。
//
//   - bytes <= 0：立即 panic（仓规总则①，构造期 fail-fast）。「不设限」的
//     表达方式是不传本选项（缺省无上限，存量语义不动）；
//   - 仅 Run 形态支持（Command 形态无选项面，需要帽用 Run）。
func WithMemoryLimit(bytes int64) RunOption {
	if bytes <= 0 {
		panic(fmt.Sprintf("procx: WithMemoryLimit: 非正上限 %d（不设限请不传本选项，缺省无上限）", bytes))
	}
	return func(c *config) {
		c.memLimit = bytes
		c.memLimitSet = true
	}
}

// MemoryLimitCapable 是兑现问询面（对齐 sandbox.Capable「建不出即拒绝」的
// 口径）：nil = 本平台/环境真能兑现（真实建组+写 memory.max+回收探测过，
// 不看脸色猜权限）；否则返回不能兑现的原因（非 linux 包
// [ErrMemoryLimitUnsupported]）。
func MemoryLimitCapable() error {
	return memoryLimitCapable()
}
