package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestNewFlagSet_NilOutput_NotToStderr 回归测试：stderr 传 nil 时，FlagSet 的
// 错误信息与用法**不得**写到进程的真实 stderr。
//
// 缺陷成因：NewFlagSet 只在 stderr != nil 时才调 SetOutput ⇒ nil 时标准库
// 沿用默认输出（os.Stderr）⇒ 错误信息逃到进程的标准错误流，破坏本包
// 「输出流全由调用方给」的契约（测试里尤其难抓——输出跑到了终端而不是
// 断言点）。
func TestNewFlagSet_NilOutput_NotToStderr(t *testing.T) {
	fs := NewFlagSet("build", nil)
	if fs == nil {
		t.Fatal("NewFlagSet 返回了 nil")
	}
	// 触发一次解析错误：未定义的 flag ⇒ 标准库会写错误信息与用法。
	_, code := ParseFlags(fs, []string{"--no-such-flag"})
	if code != ExitUsage {
		t.Fatalf("解析失败应返回 ExitUsage，实得 %d", code)
	}
	// 关键：nil 输出流必须被换成 io.Discard——**不是**沿用标准库默认的
	// os.Stderr（那会把错误信息逃到进程的真实 stderr）。
	if got := fs.Output(); got != io.Discard {
		t.Fatalf("nil 输出流应换成 io.Discard，实得 %v（默认是 os.Stderr ⇒ 错误信息会逃到进程 stderr）", got)
	}
}

// TestNewFlagSet_CustomOutput 回归护栏：调用方给了输出流 ⇒ 错误信息写进它。
func TestNewFlagSet_CustomOutput(t *testing.T) {
	var buf bytes.Buffer
	fs := NewFlagSet("build", &buf)
	_, code := ParseFlags(fs, []string{"--no-such-flag"})
	if code != ExitUsage {
		t.Fatalf("解析失败应返回 ExitUsage，实得 %d", code)
	}
	if !strings.Contains(buf.String(), "no-such-flag") {
		t.Fatalf("错误信息未写入调用方给的输出流：%q", buf.String())
	}
}
