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

// With 把 v 存入 ctx 的 k 槽位，返回派生 ctx。零值/缺省（From 查不到）
// 语义由调用方语义决定——本包不引入「零值即默认」的隐式行为。
//
// 与 context.WithValue 同底线：不要用本包传递可选的请求参数（那是函数
// 参数的事），它面向「跨多层框架代码下传的请求级作用域」（会话/角色/
// 追踪 sink）。
func With[T any](ctx context.Context, k Key[T], v T) context.Context {
	return context.WithValue(ctx, k, v)
}

// From 取出 k 槽位的值：不存在（或存储值类型与声明不符——只会发生在
// 绕过 With 直写 context.WithValue 的调用方身上）返回 T 的零值与 false。
func From[T any](ctx context.Context, k Key[T]) (T, bool) {
	v, ok := ctx.Value(k).(T)
	return v, ok
}

// MustFrom 取出 k 槽位的值；不存在时 panic。供**启动期断言**用（路由/
// 中间件装配时确认必需的作用域已注入——fail-fast 到进程启动，而不是
// 等请求路径上拿到零值静默错行）。请求处理路径上请用 From 的 (T, bool)
// 双返回值形态。
func MustFrom[T any](ctx context.Context, k Key[T]) T {
	v, ok := ctx.Value(k).(T)
	if !ok {
		panic("ctxscope: 必需的作用域值缺失 (namespace=" + k.ns + ", type=" + typeName[T]() + ")")
	}
	return v
}

// typeName 返回 T 的类型名（MustFrom 的 panic 信息用）。
func typeName[T any]() string {
	return reflect.TypeOf((*T)(nil)).Elem().String()
}
