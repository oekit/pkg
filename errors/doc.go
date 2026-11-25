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
// # 错误构造仓规（#378，v0.52.0 修订为三形态并存）
//
//   - 格式化构造（无错误链）：Newf("...", args...)（新增，#520）；
//   - 格式化链式包装：Wrapf(err, "...", args...)——err 恒追加为尾参 %w
//     （新增，#520）；stdlib 形态 fmt.Errorf("...: %w", err) 保留合法；
//   - 无格式化静态构造：errors.New（New/As/Is/Unwrap 与 stdlib 同型透传）；
//   - 结构化上下文场景用 Wrap(err, msg, kv...)——kv 经 Kv / KvFromChain
//     升格为结构化日志字段，与格式化包装互补而非替代；
//   - panic 收编统一走 Recovered（见 panic.go）：error 身份、goroutine 栈、
//     panic 值类型信息三样全保留，禁止 `fmt.Errorf("...: %v", recover())`。
//
// （Errorf 的去留史：#378 以「零引用」为由删除，审计只扫了 org 内部；
// #401/#405 曾按兼容层恢复为 Deprecated 函数，v0.48.0 彻底删除——跨 org
// 消费方已同步迁移 fmt.Errorf。v0.52.0 修订：全矩阵 fmt.Errorf 调用点
// 1068 处实测（#520），以「错误处理单 import」收口为由新增 Newf/Wrapf
// 双形态——构造与包装分开命名，非 Errorf 单函数双形态的复活。）
// 生命周期: Stable——同一 minor 线内只允许 additive 变更（新增导出符号 / 新增方法且给零值默认实现 / 新增错误值）；跨 minor 的破坏性变更须先降级并公告（判定规则与流程见 docs/api-compat.md，逐包理由见 api-lifecycle.yaml）。
package errors
