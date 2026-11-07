package ctxscope

// ctxscope 原语测试（#201）：With/From 往返、缺省零值、命名空间与值类型
// 双重隔离、MustFrom 的 panic 契约。

import (
	"context"
	"strings"
	"testing"
)

type role string

type sessionScope struct {
	PermissionMode string
	AppendMemory   bool
}

// 模拟两个「跨包」消费方：各自声明 key，命名空间不同、值类型不同。
var (
	roleKey    = NewKey[role]("git-ferry/authz.Role")
	scopeKey   = NewKey[sessionScope]("git-ferry/agent.SessionScope")
	sinkKey    = NewKey[func(string)]("git-ferry/agent.Sink")
	traceKey   = NewKey[string]("otel/trace_id")
	traceIDInt = NewKey[int]("otel/trace_id") // 同命名空间、不同 T
)

func TestWithFrom_RoundTrip(t *testing.T) {
	ctx := With(context.Background(), roleKey, role("admin"))
	got, ok := From(ctx, roleKey)
	if !ok || got != role("admin") {
		t.Fatalf("From 应取回 With 存入的值, got (%v, %v)", got, ok)
	}
}

func TestFrom_MissingReturnsZeroFalse(t *testing.T) {
	got, ok := From(context.Background(), roleKey)
	if ok {
		t.Fatal("未写入应返回 false")
	}
	if got != role("") {
		t.Fatalf("未写入应返回零值, got %q", got)
	}

	// 父 ctx 有、子 ctx 查——链式可见（context.Value 语义透传）
	ctx := With(context.Background(), roleKey, role("operator"))
	got, ok = From(ctx, roleKey)
	if !ok || got != role("operator") {
		t.Fatalf("链上值应可见, got (%v, %v)", got, ok)
	}
}

func TestKey_NamespaceIsolation(t *testing.T) {
	// 不同命名空间（同值类型）：互不可见、互不覆盖
	ctx := With(context.Background(), roleKey, role("admin"))
	ctx = With(ctx, NewKey[role]("other/Role"), role("intruder"))

	got, _ := From(ctx, roleKey)
	if got != role("admin") {
		t.Fatalf("不同命名空间不应互相覆盖, got %q", got)
	}
	got, _ = From(ctx, NewKey[role]("other/Role"))
	if got != role("intruder") {
		t.Fatalf("另一命名空间应独立可取, got %q", got)
	}
}

func TestKey_TypeIsolation(t *testing.T) {
	// 同命名空间、不同 T：是不同的 key
	ctx := With(context.Background(), traceKey, "abc-123")
	gotStr, ok := From(ctx, traceKey)
	if !ok || gotStr != "abc-123" {
		t.Fatalf("string 槽位应可取, got (%q, %v)", gotStr, ok)
	}
	if _, ok := From(ctx, traceIDInt); ok {
		t.Fatal("同命名空间不同 T 不应命中")
	}
}

func TestKey_StructAndFuncValues(t *testing.T) {
	// 非基础类型值（git-ferry 的三个手写实证形态：struct / func）
	ctx := With(context.Background(), scopeKey, sessionScope{PermissionMode: "plan", AppendMemory: true})
	sc, ok := From(ctx, scopeKey)
	if !ok || sc.PermissionMode != "plan" || !sc.AppendMemory {
		t.Fatalf("struct 值应完整往返, got %+v", sc)
	}

	calls := 0
	ctx = With(ctx, sinkKey, func(string) { calls++ })
	sink, ok := From(ctx, sinkKey)
	if !ok {
		t.Fatal("func 值应可取")
	}
	sink("event")
	if calls != 1 {
		t.Fatalf("func 值应保持引用语义, calls=%d", calls)
	}
}

func TestKey_EqualByNamespace(t *testing.T) {
	// Key 是值类型：两次 NewKey 同命名空间 ⇒ 同一个 key（包级声明不是
	// 必须的，但声明一次是治理建议）
	a, b := NewKey[int]("x/y"), NewKey[int]("x/y")
	ctx := With(context.Background(), a, 7)
	if v, ok := From(ctx, b); !ok || v != 7 {
		t.Fatalf("同命名空间同 T 的两个 Key 实例应等价, got (%v, %v)", v, ok)
	}
	if a.Namespace() != "x/y" {
		t.Fatalf("Namespace() = %q", a.Namespace())
	}
}

func TestMustFrom_Present(t *testing.T) {
	ctx := With(context.Background(), roleKey, role("admin"))
	if got := MustFrom(ctx, roleKey); got != role("admin") {
		t.Fatalf("MustFrom 应返回存入值, got %q", got)
	}
}

func TestMustFrom_MissingPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("缺值时 MustFrom 应 panic")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "git-ferry/authz.Role") {
			t.Fatalf("panic 信息应含命名空间, got %v", r)
		}
	}()
	MustFrom(context.Background(), roleKey)
}

// TestWith_RealworldPattern git-ferry 三处手写样板的等价改写：中间件
// 注入、处理器取出。
func TestWith_RealworldPattern(t *testing.T) {
	handler := func(ctx context.Context) string {
		r, ok := From(ctx, roleKey)
		if !ok {
			return "anonymous"
		}
		return string(r)
	}
	mw := func(ctx context.Context, r role) context.Context {
		return With(ctx, roleKey, r)
	}

	if got := handler(mw(context.Background(), role("operator"))); got != "operator" {
		t.Fatalf("中间件注入-处理器取出应往返, got %q", got)
	}
	if got := handler(context.Background()); got != "anonymous" {
		t.Fatalf("未注入时调用方自决兜底, got %q", got)
	}
}
