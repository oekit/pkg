// Package cli 提供框架无关、可单测的 CLI 子命令分发原语。
//
// 设计目标是「命令逻辑能被单元测试直接调用」：分发入口是
// App.Run(ctx, args, stdout, stderr) int —— 参数与输出流全部由调用方传入。
// 本包既不读取进程参数，也不终止进程（不读 os.Args、不调 os.Exit），
// 因此测试无需起子进程、无需捕获 stdout，命令体可以像普通函数一样断言。
//
// 组成：
//   - Command：单个子命令（名称 + 一行摘要 + 执行体）
//   - App：子命令集合的分发器，负责查找子命令、生成用法文本、映射退出码
//   - NewFlagSet / ParseFlags：标准库 flag 的薄封装，把解析错误与用法
//     重定向到调用方给的 stderr（标准库默认写 os.Stderr，会破坏「输出流
//     由调用方给」的约定）
//
// 退出码约定写死在包里，调用方不要另立一套：
//
//	ExitOK    = 0  成功
//	ExitError = 1  运行失败
//	ExitUsage = 2  用法错误（缺少子命令、未知子命令、参数不合法）
//
// 未知子命令与参数不合法同归 ExitUsage（2）；子命令返回什么，整体就返回
// 什么（0→0、1→1、2→2）。
//
// 用法（main 里只做三件事：拼 args、给流、拿退出码）：
//
//	func main() {
//		app := &cli.App{
//			Name: "mytool",
//			Commands: []cli.Command{
//				{Name: "build", Summary: "构建产物", Run: runBuild},
//				{Name: "serve", Summary: "启动服务", Run: runServe},
//			},
//		}
//		os.Exit(app.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
//	}
//
// 命令体（可直接被单测调用，无需起子进程）：
//
//	func runBuild(ctx context.Context, args []string, stdout, stderr io.Writer) int {
//		fs := cli.NewFlagSet("build", stderr)
//		release := fs.Bool("release", false, "发布模式")
//		rest, code := cli.ParseFlags(fs, args)
//		if code != cli.ExitOK {
//			return code
//		}
//		_ = release
//		_ = rest
//		return cli.ExitOK
//	}
//
// 用法文本由 Command.Summary 自动生成，调用方不需要手写：
//   - App.Usage(w) 单独打印（宿主想接自己的 --help 分支时写到 stdout）
//   - App.Run 在「缺少子命令」或「未知子命令」时把用法写到 stderr 并返回
//     ExitUsage（错误路径的诊断信息一律走 stderr，stdout 只留给命令体的
//     正常输出，便于管道）
package cli
