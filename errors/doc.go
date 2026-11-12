// Package errors 提供线程安全的多错误聚合。
//
// 支持 errors.Is 链式检查，使用 sync.Pool 优化字符串格式化。
//
// # 快速接入
//
//	var err error
//	err = errors.Append(err, op1())
//	err = errors.Append(err, op2())
//	if err != nil { ... }
//
// # 支持 errors.Is
//
//	if errors.Is(err, os.ErrNotExist) { ... }
//
// # 遍历所有错误
//
//	if merr, ok := err.(*errors.MultiError); ok {
//	    for _, e := range merr.Errors() { ... }
//	}
//
// # 使用场景
//
//   - 批量操作：同时执行多个操作，收集所有错误
//   - 资源清理：关闭多个资源，记录所有失败
//   - 验证：收集所有验证错误
//
// # 性能优化
//
//   - 使用 sync.Pool 复用 bytes.Buffer
//   - Copy-on-write 优化常见的"反复 Append 到左侧"模式
//   - 自动展开嵌套的 MultiError
//
// # 错误构造仓规（#378）
//
//   - 普通错误构造与包装统一 stdlib 形态：fmt.Errorf("...: %w", err) /
//     errors.New（New/As/Is/Unwrap 与 stdlib 同型透传，哨兵判定单 import 即可）；
//   - 结构化上下文场景用 Wrap(err, msg, kv...)——kv 经 Kv / KvFromChain
//     升格为结构化日志字段，普通包装没有 kv，两者互补而非替代；
//   - panic 收编统一走 Recovered（见 panic.go）：error 身份、goroutine 栈、
//     panic 值类型信息三样全保留，禁止 `fmt.Errorf("...: %v", recover())`。
//
// （Errorf 的去留史：#378 以「零引用」为由删除，但该审计只扫了 org 内部
// ——跨 org 消费方 go-git-platform 72 处 / go-gongfeng 4 处在 MVS 抬升后
// 编译断裂，断裂沿依赖链中段组件传递（#401/#405 反证）。已按兼容层恢复
// 为行为逐字一致的 Deprecated 函数并附迁移指引（见 Errorf）：新代码仍按
// 上述仓规走 stdlib，跨 org 存量不强制迁移、编译与行为不变。）
package errors
