package cli

import (
	"context"
	"fmt"
	"io"
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
}

// Run 分发 args[0] 对应的子命令，返回其退出码。
//
// 行为约定：
//   - args 为空（未给子命令）：往 stderr 打印提示与用法，返回 ExitUsage(2)
//   - 未知子命令：往 stderr 打印错误与用法，返回 ExitUsage(2)
//   - 子命令名匹配：调用该子命令的 Run，args 去掉子命令名后透传，返回值
//     原样返回（0→0、1→1、2→2）
//   - 命令表里有空名子命令（宿主装配错误）：往 stderr 打印错误，返回
//     ExitError(1)——不是用户输入问题，不落到 2
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
		fmt.Fprintf(stderr, "%s: 缺少子命令\n", a.appName())
		a.Usage(stderr)
		return ExitUsage
	}
	name := args[0]
	for i := range a.Commands {
		c := &a.Commands[i]
		if c.Name != name {
			continue
		}
		if c.Run == nil {
			fmt.Fprintf(stderr, "%s: 子命令 %q 未实现\n", a.appName(), name)
			return ExitError
		}
		return c.Run(ctx, args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "%s: 未知子命令 %q\n", a.appName(), name)
	a.Usage(stderr)
	return ExitUsage
}

// Usage 把用法文本写到 w。
//
// 用法文本包含程序名、各子命令名与其 Summary、以及退出码约定；
// Summary 为空的子命令只列名字。子命令按声明顺序展示。
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
				if summary != "" {
					summary = "  " + summary
				}
				fmt.Fprintf(w, "  %-*s%s\n", width, c.Name, summary)
			}
		}
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
