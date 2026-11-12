package bufiox

// TailLines 行粒度尾部窗口测试（#198）：预算内原样、超预算按行丢头、
// 行边界对齐、行数软上限、单行超预算、错误透传。

import (
	"bufio"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestTailLines_WithinBudgetNoTruncation(t *testing.T) {
	in := "line1\nline2\nline3\n"
	tail, truncated, err := TailLines(strings.NewReader(in), 1024)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if truncated {
		t.Fatal("预算内不应截断")
	}
	if want := "line1\nline2\nline3"; tail != want {
		t.Fatalf("tail = %q, want %q", tail, want)
	}
}

func TestTailLines_DropsFromHeadByLines(t *testing.T) {
	// 每行 5 字节内容；budget=20 ⇒ 保留尾部 4 行（20 字节 ≤ 20，
	// 第 5 行会超到 25 > 20）
	var b strings.Builder
	for i := 1; i <= 6; i++ {
		b.WriteString("line" + string(rune('0'+i)) + "\n") // line1..line6
	}
	tail, truncated, err := TailLines(strings.NewReader(b.String()), 20)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !truncated {
		t.Fatal("超预算应报告 truncated")
	}
	if want := "line3\nline4\nline5\nline6"; tail != want {
		t.Fatalf("tail = %q, want %q", tail, want)
	}
}

func TestTailLines_TruncationOnLineBoundary(t *testing.T) {
	// 截断点必须落在行边界：预算 8 时保留 "bbb\nccc"（6 字节 ≤ 8，
	// 加上 "aaa" 到 9 > 8）而不是把 "aaa\nbbb\nccc" 切成 "…b\nccc"
	// 之类的半行残根
	tail, truncated, err := TailLines(strings.NewReader("aaa\nbbb\nccc\n"), 8)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !truncated {
		t.Fatal("头部被丢弃应报告 truncated")
	}
	if want := "bbb\nccc"; tail != want {
		t.Fatalf("截断点应落在行边界, tail = %q, want %q", tail, want)
	}

	// 预算 9：三行共 9 字节 ≤ 9 ⇒ 全量保留、不截断
	tail, truncated, err = TailLines(strings.NewReader("aaa\nbbb\nccc\n"), 9)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if truncated || tail != "aaa\nbbb\nccc" {
		t.Fatalf("预算恰好容纳时应全量保留, got (%q, %v)", tail, truncated)
	}
}

func TestTailLines_EmptyInput(t *testing.T) {
	tail, truncated, err := TailLines(strings.NewReader(""), 16)
	if err != nil || truncated || tail != "" {
		t.Fatalf("空输入应返回 (\"\", false, nil), got (%q, %v, %v)", tail, truncated, err)
	}
}

func TestTailLines_SingleOversizedLineKeptWhole(t *testing.T) {
	// 单行超预算：整行保留（行不可分割），首行即超预算时头部无内容可丢
	big := strings.Repeat("x", 300)
	tail, truncated, err := TailLines(strings.NewReader(big+"\n"), 100)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if truncated {
		t.Fatal("首行即超预算：头部无内容可丢, truncated 应为 false")
	}
	if tail != big {
		t.Fatalf("超预算单行应整行保留, got %d 字节", len(tail))
	}

	// 前有小行、后接超预算大行：小行被丢弃，truncated=true，大行保留
	tail, truncated, err = TailLines(strings.NewReader("head\n"+big+"\n"), 100)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !truncated {
		t.Fatal("丢弃过头部行应报告 truncated")
	}
	if tail != big {
		t.Fatalf("超预算单行应整行保留, got %d 字节", len(tail))
	}
}

func TestTailLines_LineCountSoftCap(t *testing.T) {
	// 极小行（空行）内容字节为 0，字节预算永不超限；行数软上限
	// budget×4 生效：budget=10 ⇒ 至多 40 行
	in := strings.Repeat("\n", 100)
	tail, truncated, err := TailLines(strings.NewReader(in), 10)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !truncated {
		t.Fatal("行数软上限触发应报告 truncated")
	}
	// 40 个空行 join = 39 个 \n
	if want := strings.Repeat("\n", 39); tail != want {
		t.Fatalf("行数软上限应保留尾部 %d 行, got %d 字节的窗口", 40, len(tail))
	}
}

func TestTailLines_LongLineUpTo1MB(t *testing.T) {
	// CI 日志的超长 base64 行（<1MB）必须整行可用
	long := strings.Repeat("a", 512*1024)
	tail, truncated, err := TailLines(strings.NewReader("short\n"+long+"\n"), 64)
	if err != nil {
		t.Fatalf("512KB 单行在 1MB 上限内不应报错: %v", err)
	}
	if !truncated {
		t.Fatal("头部行被丢弃应报告 truncated")
	}
	if tail != long {
		t.Fatalf("长行应整行保留, got %d 字节", len(tail))
	}
}

func TestTailLines_LineOverLimitErrors(t *testing.T) {
	// 超 1MB 的行：bufio.ErrTooLong 原样透出（截半行没有意义）
	over := strings.Repeat("a", tailMaxLineSize+1)
	_, _, err := TailLines(strings.NewReader(over+"\n"), 64)
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("超上限单行应返回 bufio.ErrTooLong, got %v", err)
	}
}

func TestTailLines_ReadErrorPropagates(t *testing.T) {
	boom := errors.New("read boom")
	_, _, err := TailLines(errReader{boom}, 64)
	if !errors.Is(err, boom) {
		t.Fatalf("读错误应原样透传, got %v", err)
	}
}

func TestTailLines_BadBudget(t *testing.T) {
	for _, budget := range []int{0, -1} {
		if _, _, err := TailLines(strings.NewReader("x"), budget); !errors.Is(err, ErrBadBudget) {
			t.Fatalf("budget=%d 应返回 ErrBadBudget, got %v", budget, err)
		}
	}
}

func TestTailLines_CRLFNormalized(t *testing.T) {
	// \r\n 归一为 \n（ScanLines 剥 \r，join 统一 \n）
	tail, _, err := TailLines(strings.NewReader("a\r\nb\r\n"), 100)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if want := "a\nb"; tail != want {
		t.Fatalf("CRLF 应归一为 LF, tail = %q, want %q", tail, want)
	}
}

// TestTailLines_HugeBudgetNoLineCapCollapse 钉住 #408：budget 极大时行数软
// 上限的乘法必须不回绕——此前 int64(budget)*4 在 budget >= 2^61 时回绕成
// 非正数，丢头循环恒真 ⇒ 尾窗静默塌成 1 行且 truncated=true（失败形态被
// 伪装成正常截断）。修法：乘法前判上界，乘积超 int64 时软上限饱和到
// MaxInt64（永不生效——任何行数都满足「行数 <= budget×4」）。
func TestTailLines_HugeBudgetNoLineCapCollapse(t *testing.T) {
	const total = 100
	in := strings.Repeat("\n", total) // Scanner 语义：100 个 \n = 100 行空行（内容字节 0，只触发行数软上限）
	for _, budget := range []int{1024, 1 << 20, 1 << 61, 1 << 62, math.MaxInt64} {
		tail, truncated, err := TailLines(strings.NewReader(in), budget)
		if err != nil {
			t.Fatalf("budget=%d err = %v", budget, err)
		}
		got := strings.Count(tail, "\n") + 1
		if got != total {
			t.Fatalf("budget=%d 保留行数 = %d, want %d（尾窗塌缩）", budget, got, total)
		}
		if truncated {
			t.Fatalf("budget=%d 不应截断（窗口装得下全部行）", budget)
		}
	}
}

// TestTailLines_LineCapStillBinds 契约基线：正常预算下行数软上限仍然生效
// ——极小行（空行）× 超行数预算按行丢头，防回归时把软上限整个修没了。
func TestTailLines_LineCapStillBinds(t *testing.T) {
	// budget=10 ⇒ 行数软上限 40：50 行空行丢头保留 40 行。
	in := strings.Repeat("\n", 50) // Scanner 语义：50 个 \n = 50 行
	tail, truncated, err := TailLines(strings.NewReader(in), 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(tail, "\n") + 1; got != 40 {
		t.Fatalf("保留行数 = %d, want 40", got)
	}
	if !truncated {
		t.Fatal("应报告头部丢弃")
	}
}
