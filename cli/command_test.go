package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

// TestRun_AliasMatchesCommand 断言别名能派发到同一个子命令。
func TestRun_AliasMatchesCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var got []string
	app := &App{
		Name: "mytool",
		Commands: []Command{
			{Name: "doctor", Summary: "自检", Aliases: []string{"--doctor", "-doctor"}, Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
				got = args
				return ExitOK
			}},
		},
	}
	for _, alias := range []string{"doctor", "--doctor", "-doctor"} {
		got = nil
		if code := app.Run(context.Background(), []string{alias}, &stdout, &stderr); code != ExitOK {
			t.Fatalf("别名 %q 的退出码 = %d，期望 %d", alias, code, ExitOK)
		}
		if got == nil {
			t.Errorf("别名 %q 没有派发到子命令", alias)
		}
	}
}

// TestRun_AliasPassesRemainingArgs 断言别名派发时剩余参数照样透传。
func TestRun_AliasPassesRemainingArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var got []string
	app := &App{
		Name: "mytool",
		Commands: []Command{
			{Name: "doctor", Aliases: []string{"--doctor"}, Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
				got = args
				return ExitOK
			}},
		},
	}
	if code := app.Run(context.Background(), []string{"--doctor", "-v"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("退出码 = %d，期望 %d", code, ExitOK)
	}
	if len(got) != 1 || got[0] != "-v" {
		t.Errorf("子命令收到的 args = %v，期望 [-v]", got)
	}
}

// TestRun_NameWinsOverAlias 断言名字排在别名前面：宿主自己声明了名为
// "help" 的子命令时，内建帮助不盖过它。
func TestRun_NameWinsOverAlias(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ran := false
	app := &App{
		Name: "mytool",
		Commands: []Command{
			{Name: "help", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
				ran = true
				return ExitOK
			}},
		},
	}
	if code := app.Run(context.Background(), []string{"help"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("退出码 = %d，期望 %d", code, ExitOK)
	}
	if !ran {
		t.Error("宿主声明的 help 子命令应当优先于内建帮助")
	}
}

// TestRun_DefaultCommand 断言无参时执行默认子命令。
func TestRun_DefaultCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ran := false
	app := &App{
		Name:    "mytool",
		Default: "serve",
		Commands: []Command{
			{Name: "serve", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
				ran = true
				return ExitOK
			}},
		},
	}
	if code := app.Run(context.Background(), nil, &stdout, &stderr); code != ExitOK {
		t.Fatalf("退出码 = %d，期望 %d", code, ExitOK)
	}
	if !ran {
		t.Error("无参时应当执行默认子命令")
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("默认路径不应产生输出，stdout = %q stderr = %q", stdout.String(), stderr.String())
	}
}

// TestRun_DefaultCommandMissing 断言默认子命令写错是装配错误（ExitError），
// 不是用法错误。
func TestRun_DefaultCommandMissing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := &App{
		Name:    "mytool",
		Default: "nope",
		Commands: []Command{
			{Name: "serve", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int { return ExitOK }},
		},
	}
	if code := app.Run(context.Background(), nil, &stdout, &stderr); code != ExitError {
		t.Errorf("退出码 = %d，期望 %d（装配错误不是用法错误）", code, ExitError)
	}
	if !strings.Contains(stderr.String(), "默认子命令") {
		t.Errorf("stderr 缺少提示，实际 = %q", stderr.String())
	}
}

// TestRun_BuiltInHelp 断言内建帮助：输出走 stdout、退出码 0。
//
// 退出码是 0 而不是 2：问帮助不是错误路径。这一点与「未知子命令」不同。
func TestRun_BuiltInHelp(t *testing.T) {
	for _, arg := range []string{"--help", "-h", "help"} {
		var stdout, stderr bytes.Buffer
		app := newTestApp()
		if code := app.Run(context.Background(), []string{arg}, &stdout, &stderr); code != ExitOK {
			t.Errorf("%q 的退出码 = %d，期望 %d（帮助不是错误）", arg, code, ExitOK)
		}
		if !strings.Contains(stdout.String(), "用法") {
			t.Errorf("%q 的 stdout 缺少用法文本，实际 = %q", arg, stdout.String())
		}
		if stderr.Len() != 0 {
			t.Errorf("%q 的 stderr 不应有内容，实际 = %q", arg, stderr.String())
		}
	}
}

// TestRun_BuiltInVersion 断言内建版本。
func TestRun_BuiltInVersion(t *testing.T) {
	for _, arg := range []string{"--version", "version"} {
		var stdout, stderr bytes.Buffer
		app := newTestApp()
		app.Version = "1.2.3"
		if code := app.Run(context.Background(), []string{arg}, &stdout, &stderr); code != ExitOK {
			t.Errorf("%q 的退出码 = %d，期望 %d", arg, code, ExitOK)
		}
		if !strings.Contains(stdout.String(), "1.2.3") {
			t.Errorf("%q 的 stdout 缺少版本号，实际 = %q", arg, stdout.String())
		}
	}
}

// TestRun_VersionAbsentIsUnknown 断言没有 Version 时 version 是未知子命令。
func TestRun_VersionAbsentIsUnknown(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := newTestApp()
	if code := app.Run(context.Background(), []string{"version"}, &stdout, &stderr); code != ExitUsage {
		t.Errorf("退出码 = %d，期望 %d", code, ExitUsage)
	}
}

// TestUsage_ShowsDefaultAndAliases 断言用法文本标出默认子命令与别名。
func TestUsage_ShowsDefaultAndAliases(t *testing.T) {
	var buf bytes.Buffer
	app := &App{
		Name:    "mytool",
		Default: "serve",
		Commands: []Command{
			{Name: "serve", Summary: "启动服务", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int { return ExitOK }},
			{Name: "doctor", Summary: "自检", Aliases: []string{"--doctor"}, Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int { return ExitOK }},
		},
	}
	app.Usage(&buf)
	out := buf.String()
	if !strings.Contains(out, "默认") {
		t.Errorf("用法文本缺少默认子命令标注，实际 =\n%s", out)
	}
	if !strings.Contains(out, "--doctor") {
		t.Errorf("用法文本缺少别名标注，实际 =\n%s", out)
	}
}
