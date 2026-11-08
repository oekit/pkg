package ctxscope

import (
	"context"
	"reflect"
)

// Key 是带命名空间的作用域 key（#201）：字符串命名空间防跨包碰撞，
// 泛型参数 T 钉住该 key 携带的值类型（同命名空间不同 T 是不同的 key——
// 值类型是命名空间之外的第二重隔离）。
//
// Key 是可比较的值类型（仅含一个 string），可作 map key、可拷贝、可作
// 结构体字段/包级变量。**应当包级声明一次、全进程复用**（命名空间推荐
// 逆域名风格 "git-ferry/authz.Role"）；零值 Key（命名空间为 ""）技术上
// 可用但没有防撞意义，不鼓励。
type Key[T any] struct {
	ns string
}

// NewKey 创建带命名空间的作用域 key。namespace 为调用方的治理标识
// （推荐 "<组织/仓>/<类型>" 逆域名风格）；空串可用但无防撞意义——
// 两个包都用空命名空间 + 同一 T 时互相覆盖，与手写私有 key 的碰撞
// 风险等同。
func NewKey[T any](namespace string) Key[T] {
	return Key[T]{ns: namespace}
}

// Namespace 返回 key 的命名空间（调试/诊断用）。
func (k Key[T]) Namespace() string { return k.ns }

// entry 是槽位值的内部载体（#245）：With 存的是 *entry 而不是 T 本身。
//
// 为什么需要包装：`ctx.Value(k).(T)` 对**无类型 nil**（interface(nil)）
// 断言到接口 T 时 ok=false——`With(ctx, k, nil)` 存进去的是 nil interface，
// 此前 From 读不回（ok=false），「显式存 nil」与「从未写入」不可区分。
// 而接口类型正是本包的主用例（SessionScope/角色/追踪 sink 这类抽象），
// 「本请求无会话」的显式 nil 是消费方的自然写法。*entry 载体对 nil 值
// 依然非 nil ⇒ 存了 nil 也能读回 (零值, true)。
//
// 附带的第二重收益：绕过 With 直写 context.WithValue(ctx, k, v) 的调用方，
// 其值不是 *entry，From 一律按「不存在」处理——类型断言把「没走本包纪律」
// 的写入挡在门外（此前直写同型值会被误认成合法槽位值）。
type entry[T any] struct {
	v T
}

// With 把 v 存入 ctx 的 k 槽位，返回派生 ctx。v 可以为 nil（接口类型的
// 显式「无值」表达）——From 仍返回 (零值, true)，与「从未写入」可区分。
// 零值/缺省语义由调用方决定——本包不引入「零值即默认」的隐式行为。
//
// 与 context.WithValue 同底线：不要用本包传递可选的请求参数（那是函数
// 参数的事），它面向「跨多层框架代码下传的请求级作用域」（会话/角色/
// 追踪 sink）。
func With[T any](ctx context.Context, k Key[T], v T) context.Context {
	return context.WithValue(ctx, k, &entry[T]{v: v})
}

// From 取出 k 槽位的值：
//   - 未写入 → (T 零值, false)；
//   - 写入了 nil（With(ctx, k, nil)，接口 T）→ (零值, true)——写读对称
//     （#245），「存了 nil」≠「没存」；
//   - 绕过 With 直写 context.WithValue 的值（载体不是本包的 entry）→
//     (零值, false)，与未写入同口径——存储值类型与声明不符只可能来自
//     这种绕过纪律的调用方。
func From[T any](ctx context.Context, k Key[T]) (T, bool) {
	e, ok := ctx.Value(k).(*entry[T])
	if !ok || e == nil {
		var zero T
		return zero, false
	}
	return e.v, true
}

// IsSet 报告 k 槽位是否被 With 写过（含显式写入的 nil，#245）——只查
// 存在性，不解引用。绕过 With 直写 context.WithValue 的值按未写入处理
// （与 From 同口径）。
func IsSet[T any](ctx context.Context, k Key[T]) bool {
	e, ok := ctx.Value(k).(*entry[T])
	return ok && e != nil
}

// MustFrom 取出 k 槽位的值；未写入时 panic（写了 nil 不 panic——nil 是
// 显式存入的合法值，From 对它返回 (零值, true)，见 #245）。供**启动期
// 断言**用（路由/中间件装配时确认必需的作用域已注入——fail-fast 到进程
// 启动，而不是等请求路径上拿到零值静默错行）。请求处理路径上请用 From
// 的 (T, bool) 双返回值形态。
func MustFrom[T any](ctx context.Context, k Key[T]) T {
	v, ok := From(ctx, k)
	if !ok {
		panic("ctxscope: 必需的作用域值缺失 (namespace=" + k.ns + ", type=" + typeName[T]() + ")")
	}
	return v
}

// typeName 返回 T 的类型名（MustFrom 的 panic 信息用）。
func typeName[T any]() string {
	return reflect.TypeOf((*T)(nil)).Elem().String()
}
