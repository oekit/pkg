package contextx

import (
	"context"
	"testing"
	"time"
)

// 编译期钉住：返回值必须满足 context.Context。
var _ context.Context = ValueOnlyFrom(context.Background())

func TestValueOnlyFrom_KeepsValues(t *testing.T) {
	type keyT string
	const key keyT = "user_id"
	parent := context.WithValue(context.Background(), key, 42)

	child := ValueOnlyFrom(parent)
	if got := child.Value(key); got != 42 {
		t.Errorf("Value(key) = %v, want 42", got)
	}
	if got := child.Value("missing"); got != nil {
		t.Errorf("Value(missing) = %v, want nil", got)
	}
}

func TestValueOnlyFrom_StripsLifecycle(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	child := ValueOnlyFrom(parent)
	if done := child.Done(); done != nil {
		t.Error("Done() must be nil——剥离取消传播")
	}
	if err := child.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
	if dl, ok := child.Deadline(); ok || !dl.IsZero() {
		t.Errorf("Deadline() = (%v, %v), want (zero, false)——剥离 deadline", dl, ok)
	}
}

// TestValueOnlyFrom_ImmuneToParentCancel 核心场景：parent 取消后，
// 只传值 ctx 的 Err 仍为 nil、Done 仍为 nil——后台任务不被连带取消。
func TestValueOnlyFrom_ImmuneToParentCancel(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	child := ValueOnlyFrom(parent)
	cancel()

	// 给调度留出时间；parent 已取消，child 不应感知
	deadline := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
		if err := child.Err(); err != nil {
			t.Fatalf("child Err() = %v after parent cancel, want nil", err)
		}
		if done := child.Done(); done != nil {
			select {
			case <-done:
				t.Fatal("child Done() became closed after parent cancel")
			default:
			}
		}
		time.Sleep(time.Millisecond)
	}
}

func TestValueOnlyFrom_Composable(t *testing.T) {
	type keyT string
	parent := context.WithValue(context.Background(), keyT("k"), "v")
	child := ValueOnlyFrom(parent)

	// 返回值可继续参与 context 组合（WithCancel/WithValue 等）
	granted, cancel := context.WithCancel(child)
	defer cancel()
	if got := granted.Value(keyT("k")); got != "v" {
		t.Errorf("composed Value(k) = %v, want v", got)
	}
}
