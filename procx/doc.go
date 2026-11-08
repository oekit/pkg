// Package procx 子进程治理原语：进程**树**终止 + 最小环境（env 清洗）+ 输出
// 封顶。stdlib-only（pkg 零第三方依赖红线），原语下沉自 oagentkit/runtime
// procx 同款纪律（pkg#284：下游直接依赖 oagentkit 会新增跨 org 依赖边）。
//
// # 为什么需要它
//
// `exec.CommandContext` 在 ctx 到期时**只杀直接子进程**——包装器/脚本型命令
// （第三方工具常把真实工作进程 detach 到后台）的孙进程残留即泄漏面，ctx
// 取消管不到。`exec.Cmd` 不设 `Env` 时子进程**继承父进程全部**环境变量——
// 一个纯解码器子进程（ffmpeg/ffprobe 是 CVE 大户）拿走服务端全套凭据。
// `cmd.CombinedOutput()` 把 stderr 全量读进内存——损坏/恶意输入刷大量告警时
// 单个请求就能吃满内存。三个能力各自被下游反复手写，纪律只有一份：
//
//   - **Run / Command**：整树终止。unix：Setpgid 建进程组 → ctx 取消/超时
//     发 SIGTERM 给**整组**（kill(-pgid)）→ 宽限期（WithGrace，缺省 5s）后
//     SIGKILL 整组；windows：Job Object 装树 → TerminateJobObject 整树终止
//     （**无优雅终止档**——windows 没有 SIGTERM 等价物，killGraceful 退化为
//     立即整树终止，不伪装成有；宽限期在该平台只兜 WaitDelay 的管道收口）。
//   - **Env / EnvWithOS**：子进程最小环境。**默认拒绝**（allowlist 而非
//     denylist——denylist 永远漏新凭据；PB_/AWS_/AZURE_ 等凭据前缀不在
//     白名单、随其余一切被剔除），代理变量（HTTP_PROXY 等）可显式保留。
//   - **Output**：stdout/stderr 各自限容采集——**边读边截**（超限后继续
//     排空管道，子进程不会阻塞在 write 上；「读满 cap 就停」的实现会把
//     子进程挂死），truncated 返回值让「被截断」与「本来就这么短」可区分。
//
// # 平台语义矩阵（如实声明）
//
//   - Run / Output：unix 与 windows 均为整树终止；windows 无优雅档（见上）。
//   - Command：unix 全语义（Setpgid + 组 TERM/宽限 KILL + WaitDelay）；
//     **windows 降级**——exec.Cmd 没有 Start 之后的钩子，Command 形态无法
//     把子进程挂进 Job，ctx 取消只终止直接子进程（os/exec 缺省行为）。
//     windows 要整树语义用 Run / Output。
//   - 已知边界：子进程若主动 setpgid 换组/脱离（守护化经典手法之一）可逃出
//     进程组——unix 侧不追杀换组进程（需要 subreaper 全套机制，超出本原语
//     定位）；windows Job 的 KILL_ON_JOB_CLOSE 刻意未设（SetInformationJobObject
//     的结构体布局无法在无 windows runner 的 CI 上验证，oagentkit 侧同款
//     取舍），宿主进程被强杀时 Job 不自动清树。
//   - Run 保证的是「取消/超时/失败时整树收场」；命令**自然成功退出**后残留
//     的守护化孙进程不在承诺内（与 oagentkit 侧口径一致——那是命令自己的
//     语义）。
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
//	    procx.WithEnv(procx.Env("FFREPORT")),            //   最小环境 + 显式保留
//	    procx.WithOutputCap(1<<20))                      //   输出封顶
package procx
