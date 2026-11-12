package singleflighttest

// 契约套件的**元测试**（#394）：本包此前全仓唯一零 _test.go 子包——套件不是
// 纯声明（runForgetRace 的精细时序、waitFor 有界等待、大量 channel 编排），
// 编排逻辑一旦写错（如负向断言窗口过窄），受害的是两个消费它的 singleflight
// 实现，表现为「契约通过但语义漂移」且无人怀疑测试套件本身。三类护栏：
//
//  1. 正对照：真实实现（pkg/internal/singleflight）必须通过完整套件；
//  2. 负对照：故意有缺陷的 Flight（call 收尾**无条件**删 key——#147 记录的
//     缺陷形态）跑 Run 必须被 ForgetRaceKeepsNewerCall 抓出（子进程跑，
//     断言套件分辨力而非测试自身的通过）；
//  3. waitFor 超时路径：永不成立的 cond 必须在界内**干脆地 Fatal**（假 TB
//     进程内钉住），而不是挂死或静默返回。

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oekit/pkg/internal/singleflight"
)

// TestRun_AcceptsGoodImplementation 正对照：套件对正确实现是绿的——套件
// 自身的编排若有误（时序构造错误、断言写反），这层先炸。
func TestRun_AcceptsGoodImplementation(t *testing.T) {
	Run[int](t, func() Flight[int] { return singleflight.New[int]() })
}

// forgetRaceDeletingFlight 是**故意有缺陷**的 Flight：call 收尾无条件删 key。
// 与正确实现的差别：Forget 之后旧 call 的收尾会把**仍在飞行中的新 call**
// 一起删掉（#147 的缺陷形态）——「旧 call 的收尾不得删新 call」的语义被打破，
// 表现为同 key 并发跑两次 fn。
//
// 最小实现（map + done channel），语义上只保留缺陷面所需的部分：ctx 透传、
// panic 不做特殊处理（跑套件时相关子测试各自有界失败，不影响负对照结论）。
type forgetRaceDeletingFlight struct {
	mu    sync.Mutex
	calls map[string]*brokenCall
}

type brokenCall struct {
	done chan struct{}
	val  int
	err  error
}

func newForgetRaceDeletingFlight() *forgetRaceDeletingFlight {
	return &forgetRaceDeletingFlight{calls: make(map[string]*brokenCall)}
}

func (f *forgetRaceDeletingFlight) Do(ctx context.Context, key string, fn func(context.Context) (int, error)) (int, error) {
	f.mu.Lock()
	if c, ok := f.calls[key]; ok {
		f.mu.Unlock()
		<-c.done
		return c.val, c.err
	}
	c := &brokenCall{done: make(chan struct{})}
	f.calls[key] = c
	f.mu.Unlock()

	c.val, c.err = fn(ctx)
	close(c.done)
	f.mu.Lock()
	delete(f.calls, key) // ← 缺陷：无条件删 key（正确实现只在「仍是本 call」时删）
	f.mu.Unlock()
	return c.val, c.err
}

func (f *forgetRaceDeletingFlight) Forget(key string) {
	f.mu.Lock()
	delete(f.calls, key)
	f.mu.Unlock()
}

// TestRun_RejectsForgetRaceDeletingImplementation 负对照（子进程跑，同
// internal/singleflight/contract_broken_test.go 的既有模式）：套件必须把
// 「收尾无条件删 key」的实现抓出来，且抓出的正是 ForgetRaceKeepsNewerCall
// 这一条——套件分辨力自证。
func TestRun_RejectsForgetRaceDeletingImplementation(t *testing.T) {
	if os.Getenv("OEPKIT_SFTEST_FORGET_RACE_BROKEN") == "1" {
		Run[int](t, func() Flight[int] { return newForgetRaceDeletingFlight() })
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestRun_RejectsForgetRaceDeletingImplementation", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), "OEPKIT_SFTEST_FORGET_RACE_BROKEN=1")
	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("缺陷实现竟通过了契约套件——ForgetRaceKeepsNewerCall 失去分辨力：\n%s", out)
	}
	if !strings.Contains(string(out), "ForgetRaceKeepsNewerCall") {
		t.Fatalf("缺陷应被 ForgetRaceKeepsNewerCall 抓出，实际失败在别处：\n%s", out)
	}
	if elapsed > 30*time.Second {
		t.Fatalf("套件对缺陷实现耗时 %v：负向断言窗口失真（应秒级抓出）\n%s", elapsed, out)
	}
	t.Logf("缺陷实现在 %v 内被 ForgetRaceKeepsNewerCall 抓出（符合预期）", elapsed)
}

// fakeTB 进程内钉 waitFor 的 Fatal 路径：Fatal 记录消息后 runtime.Goexit
// （与 testing.T.Fatal 同语义——只终止当前 goroutine，不污染真 T）。
type fakeTB struct {
	*testing.T // 其余 TB 方法（Helper/Name/...）委托给真 T
	fatalMsg   string
}

func (f *fakeTB) Fatal(args ...any) {
	f.fatalMsg = fmt.Sprint(args...)
	runtime.Goexit()
}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.fatalMsg = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func (f *fakeTB) FailNow() { runtime.Goexit() }

// TestWaitFor_TimesOutAndFailsFast 钉住 waitFor 的超时路径：cond 永不成立时
// 必须在 ~2s 界内 Fatal 且消息逐字透出——超时路径静默返回会把「实现卡死」
// 放大成「套件通过」。
func TestWaitFor_TimesOutAndFailsFast(t *testing.T) {
	fake := &fakeTB{T: t}
	start := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		waitFor(fake, "waitFor 超时路径未被触发", func() bool { return false })
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("waitFor 超时路径未在界内退出（挂死）")
	}
	elapsed := time.Since(start)
	if elapsed < time.Second {
		t.Fatalf("waitFor 过早返回（%v）：超时路径被跳过", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("waitFor 耗时 %v：超过契约界（2s）太多", elapsed)
	}
	if fake.fatalMsg != "waitFor 超时路径未被触发" {
		t.Fatalf("Fatal 消息不符: %q", fake.fatalMsg)
	}
}

// TestWaitFor_ReturnsWhenConditionHolds 契约基线：cond 成立时正常返回、
// 不触发 Fatal。
func TestWaitFor_ReturnsWhenConditionHolds(t *testing.T) {
	fake := &fakeTB{T: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		waitFor(fake, "不应触发", func() bool { return true })
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cond 恒真时 waitFor 未返回")
	}
	if fake.fatalMsg != "" {
		t.Fatalf("cond 成立不应 Fatal: %q", fake.fatalMsg)
	}
}
