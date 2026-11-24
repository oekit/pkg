// Package container 提供泛型容器微件：并发安全 Map（sync.Map 背书）、
// SafeMap（RWMutex）、并发安全 Slice、Set、环形缓冲 Ring、阻塞队列 Queue、
// 「值+代次」原子发布单元 VersionedCell（#535，机制层——nil/深拷贝等值
// 策略归消费方包装层）。
// 全部零依赖（仅标准库）。
//
// 设计参考 go-kratos/kit container（maps/sets/slices）与 go-zero collection
// 的容器件，全面泛型化并按 Go 习惯收敛语义；v0.8.0 起收编原 collectionx
// 全部 API（Set/Ring/Queue/SafeMap 类型名与签名不变，仅换 import 路径）：
//   - Set / Ring 为非并发安全（单 goroutine 或外部加锁使用）；
//   - Queue / SafeMap / Map / Slice 内建并发安全；
//   - Ring 溢出覆盖最旧值（有界有损），Queue 无界无损；
//   - Map（sync.Map 背书）适合 key 集合稳定、读多写少的场景；
//     写冲突频繁或需要 GetOrSet 时用 SafeMap（RWMutex 背书）。
//
// 用法:
//
//	s := container.NewSet(1, 2, 3)
//	s.Add(4)
//
//	q := container.NewQueue[int]()
//	q.Put(1)
//	v, ok := q.Take() // 阻塞直至有元素或关闭
//
//	m := container.NewMap[string, *User]()
//	m.Store("u1", u)
//
//	sl := container.NewSlice[int]()
//	sl.Append(1, 2, 3)
//
//	cell := container.NewVersionedCell(&defaults)
//	cell.Publish(next)        // 发布方：原子替换，代次 +1
//	v, gen := cell.Load()     // 读侧：值与代次一致的一对
//
// 生命周期: Stable——只允许 additive 变更（新增导出符号 / 新增方法且给零值默认实现 / 新增错误值）；改签名、改语义、改默认值、删导出符号都算 breaking，判定规则与变更流程见 docs/api-compat.md，逐包理由见 api-lifecycle.yaml。
package container
