// Package ctxscope 提供类型安全的请求级作用域传递原语（#201）：命名空间
// key + 泛型 With/From——把「会话/角色/追踪 sink」沿 context.Context 下传
// 到工具/处理器，替代各消费方手写的 context.WithValue + 私有 key 类型 +
// FromContext 类型断言三件套。
//
// 为什么是库而不是各自手写：
//
//   - key 碰撞防护：私有 `type ctxKey struct{}` 在同一进程内跨包仍可能撞
//     （两个包各定义一个空结构体 key 并往同一个 ctx 写，后者静默覆盖
//     前者）。Key 携带**字符串命名空间** + 泛型值类型——命名空间是防撞
//     的治理面（推荐逆域名风格 "git-ferry/authz.Role"），值类型是第二重
//     隔离（同命名空间不同 T 是不同的 key）；
//   - 泛型消样板：With/From 一行各一个，类型断言收进库；
//   - 测试注入统一：测试里 ctxscope.With(ctx, roleKey, RoleAdmin) 构造
//     任意作用域 ctx，不必模拟整条中间件链。
//
// 与 contextx.ValueOnlyFrom 的关系：正交并存——ValueOnlyFrom 是「从已有
// ctx 取值」的适配（剥离取消传播），本包是「类型安全地写入 + 命名空间
// key」的原语。
package ctxscope
