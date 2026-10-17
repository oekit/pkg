// Package container 提供泛型容器微件：并发安全 Map（sync.Map 背书）、
// SafeMap（RWMutex）、并发安全 Slice、Set、环形缓冲 Ring、阻塞队列 Queue。
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
package container
