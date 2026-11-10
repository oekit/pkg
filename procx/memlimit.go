package procx

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// 子进程内存上限的哨兵错误（fail closed 纪律：兑现不了就拒绝执行，绝不
// 静默降级成不限——「上限没生效但调用看起来成功」比不实现更坏，pkg#359）。
var (
	// ErrMemLimitUnsupported 表示当前平台/环境不能兑现子进程内存上限。
	// 非 Linux 一律在此列；Linux 上 cgroup v2 不可用/无权限由调用点带
	// 具体原因包装（errors.Is 可判本哨兵的只有平台不支持一类）。
	ErrMemLimitUnsupported = errors.New("procx: 子进程内存上限仅 Linux/cgroup v2 可兑现")
	// ErrMemGroupClosed 表示对已回收的 [MemGroup] 调用 [MemGroup.Attach]。
	ErrMemGroupClosed = errors.New("procx: 内存限额组已回收")
)

// MemGroup 是「cgroup v2 memory.max 限额」原语：创建（[NewMemGroup]）、
// 设置（[MemGroup.Attach]）、清理（[MemGroup.Close]）三步。
//
// 为什么不用 RLIMIT_AS / RLIMIT_DATA（下游 ptc 实测记录，pkg#359）：两者
// 限的是**虚拟地址空间**，Go 运行时虚拟预留远超任何有意义的帽——RLIMIT_AS
// =256MB/1GB 时 runtime 自己起不来，4GB 才起得来而 2.2GB 已被预留占掉。
// cgroup v2 memory.max 限的是**物理内存**（RSS + page cache）：runtime
// 虚拟 arena 预留不落地、不占配额，程序真拿超限物理内存才由内核
// OOM-killer SIGKILL——父侧从退出状态可辨「被信号杀（兜底生效）」与
// 「自己非零退出（启动失败）」。换出面同时封死（memory.swap.max=0）：
// 只限物理内存会被换页规避（洪峰换出去、OOM-kill 永不触发），封死后
// 总内存（含换出）≤ 上限 ⇒ 真超限必被 OOM-kill。
//
// 兑现纪律（与 [MemoryLimitCapable] 同口径）：建不出组、写不进 memory.max
// 一律报错拒绝，不静默不限。组回收由 [MemGroup.Close] 收口（空组才删得掉，
// 残余进程先杀、子组递归删、有限轮询，不留残组）。
//
// 建组位置与委托（cgroup v2 的 no-internal-process 纪律决定，详见
// memlimit_linux.go 的 createMemGroup）：优先在父组建同级组（systemd
// 委托形态，不动进程）；其次本组直接建（真根组豁免）；都不行才做委托
// 舞蹈——此时调用进程与同组进程的 cgroup 会挪进 `<原组>/procx-rest-*`
// 叶子（限额不变、归属目录变化，如实声明的副作用）。
type MemGroup struct {
	dir   string
	limit int64

	mu       sync.Mutex
	closed   bool
	once     sync.Once
	closeErr error
}

// NewMemGroup 创建一个内存限额组并把上限写进 memory.max（cgroup v2）。
//
//   - limitBytes 是**物理内存**上限（RSS + page cache），换出面同时封死
//     （memory.swap.max=0）——总内存（含换出）≤ 上限，真超限必 OOM-kill；
//     按内核页大小向上对齐（memory.max 以页计，回读核对按对齐后取值——
//     写不进/回读不符即报错拒绝，不做「看起来设置了」的半截兑现）；
//   - limitBytes <= 0 或超防呆上限（1<<62）：返回 error（构造期 fail-fast，
//     本函数带错误返回值，不走选项 panic 的口径）；
//   - 非 Linux / cgroup v1 / 无权限：返回包装 [ErrMemLimitUnsupported] 或
//     带具体原因的 error，**不建组、不假装支持**。
func NewMemGroup(limitBytes int64) (*MemGroup, error) {
	if limitBytes <= 0 {
		return nil, fmt.Errorf("procx: NewMemGroup: 非正上限 %d", limitBytes)
	}
	if limitBytes > 1<<62 {
		return nil, fmt.Errorf("procx: NewMemGroup: 上限 %d 超防呆上限 %d", limitBytes, int64(1<<62))
	}
	dir, limit, err := createMemGroup(limitBytes)
	if err != nil {
		return nil, err
	}
	return &MemGroup{dir: dir, limit: limit}, nil
}

// Attach 把 pid 移进限额组（cgroup.procs）：此后该进程 fork 出的一切后代
// 同属本组、共享同一 memory.max。生效窗口：进程必须先存在（Start 之后调
// 用），Start 与 Attach 之间分配的内存不占配额（如实声明的微小窗口）。
//
// pid <= 0 或组已回收（[MemGroup.Close] 之后）：返回 error。
func (g *MemGroup) Attach(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("procx: Attach: 非法 pid %d", pid)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return ErrMemGroupClosed
	}
	return attachMemGroup(g.dir, pid)
}

// Close 回收限额组：先杀组内残余进程（树终止后仍留在组里的逃逸后代）、
// 递归删子组，再删组本身（空组才删得掉）——有限轮询（约 2s）内收敛，
// 删不掉如实报错。sync.Once 幂等：多次 Close 回同一结果，可放心 defer。
func (g *MemGroup) Close(ctx context.Context) error {
	g.once.Do(func() {
		g.mu.Lock()
		g.closed = true
		g.mu.Unlock()
		g.closeErr = removeMemGroup(ctx, g.dir)
	})
	return g.closeErr
}

// MemoryLimitCapable 是内存上限的**问询面**（sandbox.Capable 同款口径）：
// nil = 当前宿主能兑现（cgroup v2 可用、memory 控制器可启用、memory.max
// 可写）；非 nil = 不能兑现及原因。建不出组 / 写不进 memory.max 的环境
// （无权限、cgroup v1、非 Linux）在执行前就问得出来，调用方据此拒绝执行
// 而不是跑一个「看起来限了、实际不限」的子进程。
//
// 探测是实打实的：建一个探针组、写一次 memory.max、回读核对、清掉。
// 向父组 cgroup.subtree_control 写 "+memory"（启用 memory 控制器）是探测
// 的副作用，也是兑现的必要条件。
func MemoryLimitCapable() error {
	g, err := NewMemGroup(1 << 30)
	if err != nil {
		return err
	}
	return g.Close(context.Background())
}

// WithMemoryLimit 给 Run 的子进程设**物理内存**上限：linux 走 cgroup v2
// memory.max（[MemGroup] 原语，换出面一并封死），超限由内核 OOM-killer
// SIGKILL——Result.ExitCode 保持 stdlib 口径（被信号杀 = -1、err 是
// *exec.ExitError），「兜底生效（信号杀）」与「自己非零退出」可辨，不归一。
//
// 兑现纪律是 fail closed：cgroup 建不出来 / memory.max 写不进 / 非 Linux
// ⇒ Run **拒绝执行**并报错，绝不静默降级成不限（pkg#359 的核心诉求——
// 手搓写错的失效形态正是「静默不限」）。执行前可先问 [MemoryLimitCapable]。
// 组回收：Run 结束/取消都清 cgroup，不留残组；回收失败并入 Run 的返回
// error（errors.Join，不吞）。
//
//   - bytes <= 0：立即 panic（仓规总则①，与 WithOutputLimit 同口径）。
//     「不设限」的表达方式是不传本选项。
//   - Command 形态不经本选项：exec.Cmd 没有 post-exit 钩子收组，
//     自理场景直接用 [MemGroup]（Start 后 Attach、Wait 后 Close）。
func WithMemoryLimit(bytes int64) RunOption {
	if bytes <= 0 {
		panic(fmt.Sprintf("procx: WithMemoryLimit: 非正上限 %d（不设限请不传本选项，缺省无上限）", bytes))
	}
	return func(c *config) {
		c.memLimit = bytes
		c.memLimitSet = true
	}
}
