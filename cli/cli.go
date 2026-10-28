package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// 退出码约定（写死在包里，宿主与子命令共用同一套语义）。
const (
	// ExitOK 成功。
	ExitOK = 0
	// ExitError 运行失败：子命令执行出错。
	ExitError = 1
	// ExitUsage 用法错误：缺少子命令、未知子命令、参数不合法。
	ExitUsage = 2
)

// Command 单个 CLI 子命令。
type Command struct {
	// Name 子命令名，对应 args[0]；不可为空（空名视为宿主装配错误）。
	Name string
	// Summary 一行摘要，用于自动生成用法文本；可为空（此时只列名字）。
	Summary string
	// Aliases 子命令的别名。
	//
	// 别名与名字同等对待：先在所有子命令里精确匹配名字，匹配不到再在
	// 所有子命令里匹配别名。它解决的是「同一个动作有两种叫法」这件事——
	// 比如历史上以 `--doctor` 调用的子命令，改成子命令形式后仍要认旧写法。
	//
	// 别名可以带前导连字符（"--doctor"），也可以不带（"doc"）。为空的子
	// 命令没有别名。
	Aliases []string
	// Run 执行体。args 是去掉子命令名后的剩余参数，stdout/stderr 由宿主
	// 传入。返回值即退出码，原样透传给 App.Run 的调用方。
	Run func(ctx context.Context, args []string, stdout, stderr io.Writer) int
}

// App CLI 应用：子命令集合的分发器。
//
// App 本身不含可变状态，零值之外的结构体在 Run 之后仍可复用；并发调用
// Run 是安全的（只读 Commands）。
type App struct {
	// Name 程序名，用于用法文本与错误信息；为空时退化为 "app"。
	Name string
	// Commands 子命令列表。用法文本按声明顺序展示（作者自己决定顺序），
	// 同名子命令以先声明的为准。
	Commands []Command
	// Default 无参时执行的子命令名。
	//
	// 为空则无参是用法错误（ExitUsage）——这是本包的默认行为，保持不变。
	// 填上之后，`prog` 与 `prog <Default>` 等价。
	//
	// 它解决的是「这个程序不接参数也能干活」的场景：以 stdio 提供服务的
	// 进程通常是被框架拉起来的，没人给它传子命令名，而「没给子命令」在
	// 别的程序里等于用法错误，在这里等于「按默认方式跑」。
	//
	// Default 指向不存在的子命令是宿主装配错误（ExitError），不是用法
	// 错误。
	Default string
	// Version 版本串。填上之后提供内建的 version 子命令与 --version 标志。
	//
	// 为空则不提供：此时 `version` / `--version` 是未知子命令（ExitUsage）。
	// 版本串由宿主自己给（构建期注入），本包不读环境变量也不猜。
	Version string
}

// Run 分发 args[0] 对应的子命令，返回其退出码。
//
// 行为约定：
//   - args 为空（未给子命令）：有 Default 则执行默认子命令；否则往 stderr
//     打印提示与用法，返回 ExitUsage(2)
//   - 未知子命令：往 stderr 打印错误与用法，返回 ExitUsage(2)
//   - 子命令名匹配：调用该子命令的 Run，args 去掉子命令名后透传，返回值
//     原样返回（0→0、1→1、2→2）
//   - 命令表里有空名子命令（宿主装配错误）：往 stderr 打印错误，返回
//     ExitError(1)——不是用户输入问题，不落到 2
//   - `--help` / `-h` / `help`：把用法写到 stdout，返回 ExitOK(0)。
//     问帮助不是错误，所以不是 2
//   - `--version` / `version`（且 App.Version 非空）：把版本写到 stdout，
//     返回 ExitOK(0)
//
// 匹配顺序：子命令名 → 别名 → 内建帮助 → 内建版本。名字排在别名前面，
// 所以宿主自己声明了名为 "help" 的子命令时，它仍然照常工作。
//
// 本方法不读进程参数、不终止进程：args 与两个输出流全部由调用方给定，
// 因此可被单元测试直接调用。stdout/stderr 传 nil 时按丢弃处理，不会 panic。
func (a *App) Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	stdout = orDiscard(stdout)
	stderr = orDiscard(stderr)
	if a == nil {
		fmt.Fprintln(stderr, "cli: App 为 nil")
		return ExitError
	}
	// 先校验命令表：空名子命令是宿主装配错误，fail-fast。
	for i := range a.Commands {
		if a.Commands[i].Name == "" {
			fmt.Fprintf(stderr, "%s: 第 %d 个子命令缺少名称\n", a.appName(), i+1)
			return ExitError
		}
	}
	if len(args) == 0 {
		if a.Default == "" {
			fmt.Fprintf(stderr, "%s: 缺少子命令\n", a.appName())
			a.Usage(stderr)
			return ExitUsage
		}
		c, ok := a.find(a.Default)
		if !ok {
			// 宿主把默认子命令写错了，这是装配错误，不是用户输入问题。
			fmt.Fprintf(stderr, "%s: 默认子命令 %q 不存在\n", a.appName(), a.Default)
			return ExitError
		}
		return a.run(ctx, c, nil, stdout, stderr)
	}
	name := args[0]
	if c, ok := a.find(name); ok {
		return a.run(ctx, c, args[1:], stdout, stderr)
	}
	// 内建帮助：问帮助不是错误路径，所以输出走 stdout、退出码 0。
	if isHelpArg(name) {
		a.Usage(stdout)
		return ExitOK
	}
	// 内建版本：同上。
	if isVersionArg(name) && a.Version != "" {
		fmt.Fprintf(stdout, "%s %s\n", a.appName(), a.Version)
		return ExitOK
	}
	fmt.Fprintf(stderr, "%s: 未知子命令 %q\n", a.appName(), name)
	a.Usage(stderr)
	return ExitUsage
}

// find 按名字查找子命令：先在所有子命令里精确匹配名字，匹配不到再匹配别名。
//
// 名字优先于别名，因为别名是「同一种叫法」，不盖过正式名字。
func (a *App) find(name string) (*Command, bool) {
	for i := range a.Commands {
		if a.Commands[i].Name == name {
			return &a.Commands[i], true
		}
	}
	for i := range a.Commands {
		for _, alias := range a.Commands[i].Aliases {
			if alias == name {
				return &a.Commands[i], true
			}
		}
	}
	return nil, false
}

// run 执行子命令。Run 为 nil 是宿主装配错误，不是用法错误。
func (a *App) run(ctx context.Context, c *Command, args []string, stdout, stderr io.Writer) int {
	if c.Run == nil {
		fmt.Fprintf(stderr, "%s: 子命令 %q 未实现\n", a.appName(), c.Name)
		return ExitError
	}
	return c.Run(ctx, args, stdout, stderr)
}

// isHelpArg 判断这个参数是不是「要帮助」。
func isHelpArg(s string) bool {
	return s == "--help" || s == "-h" || s == "help"
}

// isVersionArg 判断这个参数是不是「要版本」。
func isVersionArg(s string) bool {
	return s == "--version" || s == "version"
}

// Usage 把用法文本写到 w。
//
// 用法文本包含程序名、各子命令名与其 Summary、以及退出码约定；
// Summary 为空的子命令只列名字。子命令按声明顺序展示。
//
// 默认子命令标注「（默认）」，带别名的标注「（别名：…）」。标注附在摘要
// 之后——名称列只按 ASCII 名字算宽度，标注不参与列宽计算，否则中文摘要
// 会把整列的对齐挤歪。
//
// 宿主想接自己的 --help 分支时，直接 Usage(os.Stdout) 即可；
// App.Run 内部的错误路径写的是 stderr。
func (a *App) Usage(w io.Writer) {
	if w == nil {
		return
	}
	name := a.appName()
	fmt.Fprintf(w, "用法:\n  %s <子命令> [参数...]\n", name)
	if a != nil {
		width := 0
		for _, c := range a.Commands {
			if c.Name == "" {
				continue
			}
			if len(c.Name) > width {
				width = len(c.Name)
			}
		}
		if width > 0 {
			fmt.Fprintf(w, "\n子命令:\n")
			for _, c := range a.Commands {
				if c.Name == "" {
					continue
				}
				summary := c.Summary
				if c.Name == a.Default {
					summary += " （默认）"
				}
				if len(c.Aliases) > 0 {
					summary += " （别名：" + strings.Join(c.Aliases, "、") + "）"
				}
				if summary != "" {
					summary = "  " + summary
				}
				fmt.Fprintf(w, "  %-*s%s\n", width, c.Name, summary)
			}
		}
	}
	if a != nil && a.Version != "" {
		fmt.Fprintf(w, "\n版本:\n  %s %s\n", name, a.Version)
	}
	fmt.Fprintf(w, "\n退出码:\n")
	fmt.Fprintf(w, "  %d  成功\n", ExitOK)
	fmt.Fprintf(w, "  %d  运行失败\n", ExitError)
	fmt.Fprintf(w, "  %d  用法错误（缺少子命令、未知子命令或参数不合法）\n", ExitUsage)
}

// appName 返回用于诊断信息与用法文本的程序名（空名退化为 "app"）。
func (a *App) appName() string {
	if a == nil || a.Name == "" {
		return "app"
	}
	return a.Name
}

// orDiscard 把 nil 输出流换成丢弃流——调用方传 nil 不应触发 panic。
func orDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}
