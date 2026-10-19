package cli

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// newTestApp 构造一个含两个子命令的测试 App。
func newTestApp() *App {
	return &App{
		Name: "mytool",
		Commands: []Command{
			{Name: "build", Summary: "构建产物", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
				return ExitOK
			}},
			{Name: "serve", Summary: "启动服务", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
				return ExitOK
			}},
		},
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := newTestApp()

	code := app.Run(context.Background(), []string{"deploy"}, &stdout, &stderr)

	if code != ExitUsage {
		t.Errorf("未知子命令的退出码 = %d，期望 %d", code, ExitUsage)
	}
	if !strings.Contains(stderr.String(), "未知子命令") {
		t.Errorf("stderr 缺少可读的未知子命令提示，实际 = %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "deploy") {
		t.Errorf("stderr 未回显出错的子命令名，实际 = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("错误路径不应写 stdout，实际 = %q", stdout.String())
	}
}

func TestRun_EmptyArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	app := newTestApp()

	code := app.Run(context.Background(), nil, &stdout, &stderr)

	if code != ExitUsage {
		t.Errorf("空参数的退出码 = %d，期望 %d", code, ExitUsage)
	}
	// 约定：用法文本写 stderr（stdout 只留给命令体的正常输出）。
	if !strings.Contains(stderr.String(), "用法") {
		t.Errorf("stderr 缺少用法文本，实际 = %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "build") || !strings.Contains(stderr.String(), "serve") {
		t.Errorf("用法文本缺少子命令名，实际 = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("空参数不应写 stdout，实际 = %q", stdout.String())
	}
}

func TestRun_CommandExitCodePassthrough(t *testing.T) {
	cases := []struct {
		name string
		want int
	}{
		{"成功", ExitOK},
		{"运行失败", ExitError},
		{"用法错误", ExitUsage},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			app := &App{
				Name: "mytool",
				Commands: []Command{
					{Name: "run", Summary: "执行", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
						return c.want
					}},
				},
			}
			if got := app.Run(context.Background(), []string{"run"}, &stdout, &stderr); got != c.want {
				t.Errorf("整体退出码 = %d，期望 %d（子命令返回值应原样透传）", got, c.want)
			}
		})
	}
}

func TestUsage_ContainsSummaries(t *testing.T) {
	var buf bytes.Buffer
	app := &App{
		Name: "mytool",
		Commands: []Command{
			{Name: "build", Summary: "构建产物", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int { return ExitOK }},
			{Name: "serve", Summary: "启动服务", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int { return ExitOK }},
			{Name: "clean", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int { return ExitOK }},
		},
	}
	app.Usage(&buf)

	out := buf.String()
	for _, want := range []string{"mytool", "build", "构建产物", "serve", "启动服务", "clean"} {
		if !strings.Contains(out, want) {
			t.Errorf("用法文本缺少 %q，实际 =\n%s", want, out)
		}
	}
	// 退出码约定必须写进用法文本。
	for _, want := range []string{"0", "1", "2"} {
		if !strings.Contains(out, want) {
			t.Errorf("用法文本缺少退出码 %q，实际 =\n%s", want, out)
		}
	}
}

func TestRun_PassesRemainingArgsAndStreams(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var gotArgs []string
	app := &App{
		Name: "mytool",
		Commands: []Command{
			{Name: "build", Summary: "构建产物", Run: func(ctx context.Context, args []string, out, errw io.Writer) int {
				gotArgs = args
				_, _ = out.Write([]byte("输出"))
				_, _ = errw.Write([]byte("诊断"))
				return ExitOK
			}},
		},
	}

	if code := app.Run(context.Background(), []string{"build", "-o", "out.bin"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("退出码 = %d，期望 %d", code, ExitOK)
	}
	if want := []string{"-o", "out.bin"}; len(gotArgs) != 2 || gotArgs[0] != want[0] || gotArgs[1] != want[1] {
		t.Errorf("子命令收到的 args = %v，期望 %v", gotArgs, want)
	}
	if stdout.String() != "输出" {
		t.Errorf("stdout = %q，期望 %q", stdout.String(), "输出")
	}
	if stderr.String() != "诊断" {
		t.Errorf("stderr = %q，期望 %q", stderr.String(), "诊断")
	}
}

func TestRun_MalformedApp(t *testing.T) {
	t.Run("子命令未实现", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		app := &App{Name: "mytool", Commands: []Command{{Name: "build", Summary: "构建产物"}}}
		if code := app.Run(context.Background(), []string{"build"}, &stdout, &stderr); code != ExitError {
			t.Errorf("退出码 = %d，期望 %d（宿主装配错误不是用法错误）", code, ExitError)
		}
		if !strings.Contains(stderr.String(), "未实现") {
			t.Errorf("stderr 缺少提示，实际 = %q", stderr.String())
		}
	})

	t.Run("子命令缺少名称", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		app := &App{Name: "mytool", Commands: []Command{{Summary: "构建产物", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int { return ExitOK }}}}
		if code := app.Run(context.Background(), []string{"build"}, &stdout, &stderr); code != ExitError {
			t.Errorf("退出码 = %d，期望 %d", code, ExitError)
		}
		if !strings.Contains(stderr.String(), "缺少名称") {
			t.Errorf("stderr 缺少提示，实际 = %q", stderr.String())
		}
	})
}

// TestNoProcessExitOrArgsInSource 断言包内代码不出现 os.Exit / os.Args：
// 本包必须保持「不终止进程、不读进程参数」，否则命令体无法被直接单测。
//
// 用 go/parser 走 AST（不解析注释）：文档里出现的 os.Exit/os.Args 字样
// 不算违规，违规的是代码里真的引用了这两个符号。别名导入（import o "os"）
// 与点导入（import . "os"）也一并防住。
func TestNoProcessExitOrArgsInSource(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 失败：无法定位本包源文件")
	}
	dir := filepath.Dir(thisFile)
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("Glob %s: %v", dir, err)
	}
	if len(files) == 0 {
		t.Fatalf("%s 下没有 .go 源文件", dir)
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s: %v", f, err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), f, src, 0)
		if err != nil {
			t.Fatalf("解析 %s: %v", f, err)
		}
		// 本地名 -> 导入路径（用于识别别名导入的 os）。
		imports := map[string]string{}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("解析 %s 的 import 路径: %v", f, err)
			}
			local := path[strings.LastIndex(path, "/")+1:]
			if imp.Name != nil {
				local = imp.Name.Name
				if local == "." && path == "os" {
					t.Errorf("%s: 禁止点导入 os", f)
				}
			}
			imports[local] = path
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if imports[ident.Name] != "os" {
				return true
			}
			if sel.Sel.Name == "Exit" || sel.Sel.Name == "Args" {
				t.Errorf("%s: 禁止使用 os.%s —— 本包必须可单测（不终止进程、不读进程参数）", f, sel.Sel.Name)
			}
			return true
		})
	}
}
