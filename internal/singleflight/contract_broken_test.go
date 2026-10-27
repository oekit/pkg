package singleflight

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/oekit/pkg/singleflighttest"
)

// ctxIgnoringFlight 是一个**故意有缺陷**的 Flight：waiter 路径完全忽略调用方的
// ctx（一直阻塞到 flight 结束）。
//
// 它用来证明契约套件本身是可靠的：契约套件里所有「等实现进入某个状态」的
// 等待都必须**有界**——实现有缺陷时，正确表现是干脆地失败，而不是把 CI 挂到
// go test 的 10 分钟超时。
type ctxIgnoringFlight struct {
	inner *Flight[int]
}

func (f *ctxIgnoringFlight) Do(ctx context.Context, key string, fn func(context.Context) (int, error)) (int, error) {
	return f.inner.Do(context.Background(), key, fn)
}

func (f *ctxIgnoringFlight) Forget(key string) { f.inner.Forget(key) }

// TestContract_FailsFastOnBrokenImplementation 用子进程跑契约套件，对一个
// 「waiter 忽略 ctx」的缺陷实现跑：
//
//   - 期望：契约用例**很快**失败（数秒内），而不是挂死；
//   - 修前：waiter 的 Do 一直阻塞、block 只在 Do 返回后才关 ⇒ 死锁 ⇒ 子进程
//     挂到 go test 的 10 分钟超时。
func TestContract_FailsFastOnBrokenImplementation(t *testing.T) {
	if os.Getenv("OEPKIT_CONTRACT_BROKEN") == "1" {
		singleflighttest.Run[int](t, func() singleflighttest.Flight[int] {
			return &ctxIgnoringFlight{inner: New[int]()}
		})
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestContract_FailsFastOnBrokenImplementation", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), "OEPKIT_CONTRACT_BROKEN=1")
	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("缺陷实现竟通过了契约套件：\n%s", out)
	}
	if elapsed > 30*time.Second {
		t.Fatalf("契约套件对缺陷实现耗时 %v：等待无界（应干脆失败）\n%s", elapsed, out)
	}
	t.Logf("缺陷实现在 %v 内失败（符合预期：干脆失败而非挂死）", elapsed)
}
