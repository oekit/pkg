// Package collectionx 提供泛型并发/集合容器微件：Set、环形缓冲 Ring、
// 阻塞队列 Queue、并发安全 SafeMap。全部零依赖（仅标准库）。
//
// 设计参考 go-zero collection 的容器件（Set/Ring/fifo.Queue/SafeMap），
// 全面泛型化并按 Go 习惯收敛语义：
//   - Set / Ring 为非并发安全（单 goroutine 或外部加锁使用）；
//   - Queue / SafeMap 内建并发安全；
//   - Ring 溢出覆盖最旧值（有界有损），Queue 无界无损。
//
// 用法:
//
//	s := collectionx.NewSet(1, 2, 3)
//	s.Add(4)
//
//	q := collectionx.NewQueue[int]()
//	q.Put(1)
//	v, ok := q.Take() // 阻塞直至有元素或关闭
package collectionx
