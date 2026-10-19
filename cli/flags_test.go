package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParseFlags_Success(t *testing.T) {
	var stderr bytes.Buffer
	fs := NewFlagSet("build", &stderr)
	release := fs.Bool("release", false, "发布模式")
	output := fs.String("output", "out.bin", "输出文件")

	rest, code := ParseFlags(fs, []string{"-release", "-output", "app.bin", "extra"})

	if code != ExitOK {
		t.Fatalf("退出码 = %d，期望 %d", code, ExitOK)
	}
	if !*release {
		t.Error("-release 未被解析为 true")
	}
	if *output != "app.bin" {
		t.Errorf("-output = %q，期望 %q", *output, "app.bin")
	}
	if len(rest) != 1 || rest[0] != "extra" {
		t.Errorf("剩余位置参数 = %v，期望 [extra]", rest)
	}
}

func TestParseFlags_ErrorGoesToCallerStderr(t *testing.T) {
	// 把 os.Stderr 临时换成空文件，确认解析错误只落在调用方给的流上。
	orig := os.Stderr
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	os.Stderr = f
	defer func() {
		os.Stderr = orig
		_ = f.Close()
	}()

	var buf bytes.Buffer
	fs := NewFlagSet("build", &buf)
	_, code := ParseFlags(fs, []string{"-nope"})

	os.Stderr = orig // 提前恢复，避免后续输出落到临时文件

	if code != ExitUsage {
		t.Errorf("flag 解析失败的退出码 = %d，期望 %d", code, ExitUsage)
	}
	if !strings.Contains(buf.String(), "flag provided but not defined: -nope") {
		t.Errorf("调用方给的 stderr 未收到 flag 错误信息，实际 = %q", buf.String())
	}
	captured, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("读取捕获的 os.Stderr: %v", err)
	}
	if len(captured) != 0 {
		t.Errorf("os.Stderr 收到了输出 %q，期望为空（错误应只写调用方给的 stderr）", captured)
	}
}

func TestNewFlagSet_OutputRedirected(t *testing.T) {
	var buf bytes.Buffer
	fs := NewFlagSet("build", &buf)
	if fs.ErrorHandling() != 0 { // flag.ContinueOnError == 0
		t.Errorf("ErrorHandling = %d，期望 flag.ContinueOnError", fs.ErrorHandling())
	}
	if fs.Output() != &buf {
		t.Error("FlagSet 的输出流未重定向到调用方给的 stderr")
	}
}

func TestParseFlags_Help(t *testing.T) {
	var buf bytes.Buffer
	fs := NewFlagSet("build", &buf)
	fs.Bool("release", false, "发布模式")

	_, code := ParseFlags(fs, []string{"-h"})

	if code != ExitOK {
		t.Errorf("-h 的退出码 = %d，期望 %d（帮助请求不算错误）", code, ExitOK)
	}
	if !strings.Contains(buf.String(), "Usage of build") {
		t.Errorf("-h 的用法未写到调用方给的 stderr，实际 = %q", buf.String())
	}
}
