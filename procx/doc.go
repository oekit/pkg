// Package procx 子进程治理原语：进程**树**终止。stdlib-only（pkg 零第三方
// 依赖红线），原语下沉自 oagentkit/runtime procx 同款纪律（pkg#284：下游
// pigeonbox/core 直接依赖 oagentkit 会新增跨 org 依赖边，决策记录
// pigeonbox/core#71）。
//
// # 为什么需要它
//
// `exec.CommandContext` 在 ctx 到期时**只杀直接子进程**——包装器/脚本型命令
// （第三方工具常把真实工作进程 detach 到后台）的孙进程残留即泄漏面，ctx
// 取消管不到。下游预览子系统的 ffmpeg/ffprobe 包装驱动正撞上这个缺口。
//
//   - **Run**：全治理形态——收集 stdout/stderr 返回 [Result]（退出码与
//     stdlib 同口径，被信号终止 = -1），ctx 取消导致的终止返回 ctx.Err()；
//   - **Command**：drop-in 形态——存量 `exec.CommandContext` 调用点逐字
//     替换，I/O 配置完全由调用方自理。
//
// # 收场语义（unix）
//
// Setpgid 让子进程自立进程组 → ctx 取消时先向**整组**发 SIGTERM
// （kill(-pgid)，同组后代随 fork 继承一并送达），同时按父链快照清扫
// **跨组逃逸的后代**（守护化/setsid 的真孙进程——组击杀够不到的那层，
// 逐个补发 TERM 并连带各自进程组；linux 走 /proc、darwin 走 ps(1)，
// 仅取消路径调用、带 5s 上限，其余 unix 降级为纯组击杀）→ 宽限轮询
// （WithGrace，缺省 5s；每 10ms 探一次，树死干净立即收手——优雅退出的
// 进程不会被 KILL，也不空等满宽限；0=无宽限立即 KILL，负值构造期 panic）
// → 宽限耗尽仍有幸存者则重枚举逃逸者后 SIGKILL 整组 + 全部逃逸者。
//
// Run 在进程退出后（无论正常还是被终止）顺手清一次组内幸存者（无宽限
// 直接 KILL）——守护化残留不因主进程正常退出而泄漏；Command 形态没有
// 这个挂点（caller 自己调 Wait，exec.Cmd 无 post-exit 钩子）。
//
// # 收场语义（windows）
//
// 无进程组概念，两条机制按形态分工：
//
//   - **Run 形态：Job Object**。Start 后立即把子进程挂入 Job
//     （CreateJobObjectW + SetInformationJobObject + AssignProcessToJobObject，
//     全走 syscall.NewLazyDLL，不引 x/sys），此后子进程 spawn 的一切后代
//     默认同属该 Job；取消时 TerminateJobObject 整树终止；进程退出后关闭
//     Job 句柄，**KILL_ON_JOB_CLOSE** 清树——宿主进程崩溃时句柄随进程消亡
//     同样触发清树（崩溃安全）。Start 与挂入之间有微秒级窗口，窗口内已
//     spawn 且 detach 的孙进程不被 Job 覆盖（如实声明）。
//   - **Command 形态：taskkill /T /F**。exec.Cmd 没有 Start 之后的钩子，
//     无法预先挂 Job ⇒ 取消时按父链快照整树强杀（换组/setsid 的后代只要
//     还是子进程就会被 /T 走到）；没有退出后清扫挂点。
//
// **语义降级如实声明：windows 没有优雅终止档**（无 SIGTERM 等价物）——
// WithGrace 在 windows 上退化为立即整树终止，不伪装成有；宽限期在该平台
// 只兜 WaitDelay 的管道收口。
//
// # 边界（如实声明，不伪装全知）
//
//   - 父链中途断掉的逃逸者（父进程先退出被 reparent）不可达——unix 的
//     父链清扫是快照，不需要 subreaper 就能做到的是「父链完整」的部分，
//     守护化孙进程的典型场景（包装器常驻、工作进程 setsid detach）父链
//     完整、覆盖得到；真·double-fork 断链场景需要 subreaper 全套机制，
//     超出本原语定位。
//   - TERM 之后才守护化、且守护化完成前父链在 KILL 重枚举前断掉的孙进程，
//     同样不可达。
//   - 跨组逃逸者按「pid + 各自进程组」击杀：若对方把组与无关进程共用
//     （极罕见），会波及同组进程——进程组语义的固有粒度。
//   - 若子进程已属于别的 Job（windows），AssignProcessToJobObject 失败：
//     退化为只杀直接子进程。
//
// # 与仓内其他包的分工
//
// shellx 是「字符串怎么写进命令行」（转义/分词，纯函数）；本包是「命令怎么
// 跑、怎么收场」（进程托管）。两者互补：外部输入拼 argv 前先过 shellx，
// 执行用本包。
//
// 用法（替换 exec.CommandContext 逐字迁移）：
//
//	cmd := procx.Command(ctx, "ffmpeg", "-i", in, out) // unix 整树语义
//	res, err := procx.Run(ctx, "ffmpeg", args,          // 全治理形态：
//	    procx.WithGrace(2*time.Second))                 //   宽限可调
package procx
