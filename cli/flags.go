package cli

import (
	"errors"
	"flag"
	"io"
)

// NewFlagSet 创建绑定到 stderr 的标准库 FlagSet。
//
// 两个关键点：
//   - flag.ContinueOnError：解析出错不终止进程，而是返回 error，交由调用方
//     映射成退出码（ExitUsage）——与本包「不调 os.Exit」的约定一致；
//   - fs.SetOutput(stderr)：把标准库默认写到 os.Stderr 的错误与用法重定向
//     到调用方给的 stderr，遵守「输出流由调用方给」的约定。
//
// 典型用法：
//
//	fs := cli.NewFlagSet("build", stderr)
//	release := fs.Bool("release", false, "发布模式")
//	rest, code := cli.ParseFlags(fs, args)
//	if code != cli.ExitOK {
//		return code
//	}
//
// 注意：FlagSet 不支持并发 Parse；每个子命令自建一个即可。
func NewFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	if stderr == nil {
		// nil = 丢弃（与本包 orDiscard 的口径一致）。**不调 SetOutput** 时
		// 标准库默认写 os.Stderr ⇒ 错误信息与用法会逃到进程的真实 stderr，
		// 破坏本包「输出流全由调用方给」的契约（测试里尤其难抓）。
		stderr = io.Discard
	}
	fs.SetOutput(stderr)
	return fs
}

// ParseFlags 用 fs 解析 args，返回剩余位置参数与退出码。
//
//   - 解析成功：返回 (位置参数, ExitOK)
//   - -h/--help：标准库已把用法写到 fs 的输出流（调用方给的 stderr），
//     返回 (位置参数, ExitOK)——帮助请求不算错误
//   - 解析失败（未定义的 flag、取值类型不匹配等）：标准库已把错误信息与
//     用法写到 fs 的输出流，返回 (nil, ExitUsage)
//
// fs 为 nil 时返回 (nil, ExitUsage)。
func ParseFlags(fs *flag.FlagSet, args []string) ([]string, int) {
	if fs == nil {
		return nil, ExitUsage
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return fs.Args(), ExitOK
		}
		return nil, ExitUsage
	}
	return fs.Args(), ExitOK
}
