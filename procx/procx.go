package procx

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// defaultGrace 是 TERM→KILL 的宽限时长。
const defaultGrace = 5 * time.Second

type config struct {
	grace       time.Duration
	minimalEnv  bool
	envExtra    []string
	outputLimit int
	limitSet    bool
}

// RunOption 定制 Run 形态的治理行为
// （仓规：函数选项一律 XxxOption，禁止裸 Option——多包 co-import 冲突）。
type RunOption func(*config)

// WithGrace 设置优雅终止宽限：先向整组发 TERM，超过宽限仍未退出才 KILL。
// 默认 5s。windows 上无优雅档，本选项退化为立即整树终止（见包文档）。
//
//   - d < 0：立即 panic（仓规总则①，构造期非法值 fail-fast——在选项构造点
//     报错，而不是等到应用点才炸）；
//   - d == 0：合法，语义为「无宽限，TERM 与 KILL 同拍发出」（要立即收场时用）。
func WithGrace(d time.Duration) RunOption {
	if d < 0 {
		panic(fmt.Sprintf("procx: WithGrace: 负宽限期 %v", d))
	}
	return func(c *config) { c.grace = d }
}

// WithEnv 让 Run 用「最小环境」起子进程：cmd.Env = ChildEnv(extra)——
// 白名单 ∪ 显式放行、默认拒绝（语义与 extra 形态见 [ChildEnv]）。执行不可信
// 内容（外部输入喂的媒体处理、用户工作目录里的 agent 任务）的调用点显式加
// 本选项；缺省（不传）继承父进程全量环境——存量语义不动，可信 CLI 场景继续
// 走缺省，两种威胁模型在调用点字面可见。多次调用后者覆盖前者（Go 选项惯例）。
// Command 形态不经本选项：caller 自理 cmd.Env = procx.ChildEnv(...)。
func WithEnv(extra []string) RunOption {
	return func(c *config) {
		c.envExtra = extra
		c.minimalEnv = true
	}
}

// WithOutputLimit 给 Run 的 stdout/stderr **各设**采集上限（字节）——
// **边读边截**：超限部分照常从管道读走并丢弃，子进程不会卡在 write 上、
// 正常退出；截断在 [Result.StdoutTruncated] / [Result.StderrTruncated]
// 里如实标记（「被截断」与「本来就这么短」必须可区分；正好等于上限不算
// 截断）。上限按流各自独立，互不挤占。
//
// 动机（pkg#314，pkg#294 承诺的输出封顶半边）：损坏输入会让 ffmpeg/ffprobe
// 这类命令往 stderr 狂刷告警，无上限采集即单请求内存放大——限容必须配
// 「继续排空」：最常见错误实现是「读满 cap 就停读」，子进程阻塞在 write 上
// 永不退出，Run 挂死（测试有负对照钉住该分辨力）。
//
//   - n <= 0：立即 panic（仓规总则①，构造期 fail-fast）。「不设限」的表达
//     方式是不传本选项（缺省无上限，存量调用不受影响）；0 不是合法上限，
//     静默吞掉全部输出几乎必然是笔误，等应用点才炸就晚了。
func WithOutputLimit(n int) RunOption {
	if n <= 0 {
		panic(fmt.Sprintf("procx: WithOutputLimit: 非正上限 %d（不设限请不传本选项，缺省无上限）", n))
	}
	return func(c *config) {
		c.outputLimit = n
		c.limitSet = true
	}
}

// Result 是 Run 的收场结果。
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	// StdoutTruncated / StderrTruncated 报告对应流是否因超过 WithOutputLimit
	// 上限被截断（未设上限恒 false）。截断是刻意的（限容防内存放大），但
	// **必须可观测**——否则调用方会把「半截输出」当完整输出用；截断与本来
	// 就这么短必须可区分。正好等于上限不算截断。
	StdoutTruncated bool
	StderrTruncated bool
}

// Run 起子进程并保证 ctx 到期/取消时整棵进程树收场（收集 stdout/stderr）。
// 退出码与 stdlib 同口径；ctx 取消导致的终止返回 ctx.Err()。
//
// 进程退出后（无论正常还是被终止）顺手做一次残留清扫：unix 清掉组内幸存者、
// windows 关闭 Job 句柄触发 KILL_ON_JOB_CLOSE——守护化残留的孙进程不因主
// 进程退出而泄漏（Command 形态没有这个挂点，见包文档）。
func Run(ctx context.Context, name string, args []string, opts ...RunOption) (*Result, error) {
	var c config
	c.grace = defaultGrace
	for _, o := range opts {
		if o != nil { // 仓规总则②：nil 选项 = 显式回退默认值，不算非法
			o(&c)
		}
	}
	cmd, st := newCmd(ctx, name, args, c.grace)
	if c.minimalEnv {
		cmd.Env = ChildEnv(c.envExtra)
	}
	var stdout, stderr cappedBuffer
	if c.limitSet {
		stdout.limit = c.outputLimit
		stderr.limit = c.outputLimit
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		st.release()
		if ctx.Err() != nil {
			return nil, ctx.Err() // exec.Start 对已取消 ctx 返回 ctx.Err()
		}
		return nil, err
	}
	st.arm(cmd)
	werr := cmd.Wait()
	st.release()
	st.reap(cmd)
	// cmd.Wait 返回时输出拷贝 goroutine 已全部收束（os/exec 契约），读缓冲无竞争。
	res := &Result{
		Stdout:          stdout.buf,
		Stderr:          stderr.buf,
		StdoutTruncated: stdout.truncated,
		StderrTruncated: stderr.truncated,
	}
	var ee *exec.ExitError
	if errors.As(werr, &ee) {
		res.ExitCode = ee.ExitCode()
	} else if werr == nil {
		res.ExitCode = 0
	}
	if ctx.Err() != nil {
		return res, ctx.Err()
	}
	return res, werr
}

// cappedBuffer 限容写入器：边读边截——超过 limit 的字节**照常消费**（Write
// 恒报全量成功，io.Copy 因此继续排空管道，子进程不会卡在 write 上）但不再
// 保留，丢弃即 truncated 置位。limit<=0 = 不限容（Run 缺省，存量语义：
// 无上限全量采集）。缓冲钉在 limit，洪流再多也不放大内存（写入 chunk 大小
// 除外——os/exec 的拷贝缓冲有界）。
//
// Write 只会被 os/exec 的每个流各自的拷贝 goroutine 调用，互斥锁让本类型
// 自洽（多流共享/复位重用也不出竞争）。
type cappedBuffer struct {
	mu        sync.Mutex
	limit     int
	buf       []byte
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit > 0 {
		if room := b.limit - len(b.buf); len(p) > room {
			if room > 0 {
				b.buf = append(b.buf, p[:room]...)
			}
			b.truncated = true
			return len(p), nil // 全量报消费成功：管道必须继续排空，子进程才能退出
		}
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

// Command 把树终止语义挂到 *exec.Cmd 上（不接管 I/O 与 Env 配置）——现有
// exec.CommandContext 调用点的逐字替换形态。ctx 到期/取消时：unix 先向
// 整个进程组发 TERM 并清扫跨组逃逸后代，宽限（默认 5s）后 KILL 整组；
// windows 用 taskkill /T /F 按父链整树强杀（无优雅档）。需要最小环境时
// caller 自理：cmd.Env = procx.ChildEnv(...)。
//
// Command 形态没有「退出后残留清扫」挂点（caller 自己调 Wait，exec.Cmd
// 没有 post-exit 钩子），需要该语义用 Run 形态；windows 上 Run 形态另有
// Job Object 整树语义（Start 即挂入，覆盖 Start 后 spawn 的全部后代），
// Command 形态只有取消时点的树强杀，见包文档。
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd, _ := newCmd(ctx, name, args, defaultGrace)
	return cmd
}
