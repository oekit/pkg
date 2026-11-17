package container

import (
	"fmt"
	"sync/atomic"
)

// VersionedCell 泛型「值+代次」原子发布单元（#535）：值与代次绑在**同一个
// 原子单元**里发布（单 atomic.Pointer），读侧一次 Load 拿到一致的一对——
// 不存在「新值配旧代次」的自相矛盾窗口（拆成两个原子变量的实现，读者会在
// 跨字段的交错窗口同时看到新旧两半，症状是「发布过了但读到旧值」，
// core#79 实测 20 万次 Update 观察到 345 次）。
//
// 它是 core 侧两份同构手写的下沉机制件（core/config.Snapshot 与
// core/governance.Store 各持一份同款 cell+CAS 发布，两者注释自认同构）：
// 本类型只提供**原子机制**——值与代次的成对发布、CAS 推进代次、一致读；
// 各消费方的**值语义策略**由各自包装层实现，不在本层解读：
//
//   - 「读侧永不返 nil」（Snapshot 的契约）：包装层在构造期注入默认值，
//     本层零值 cell 的 Load 返回零值与代次 0，是否视为非法由包装层判定；
//   - 「nil→空配置 / 发布前深拷贝 / nil 发布被忽略」（Store 的口径）：
//     包装层在调用 Publish 之前自行变换入参——本层对指针类型的 nil **原样
//     发布**（推进代次），不做任何解读。
//
// 用法：
//
//	c := container.NewVersionedCell(&defaults)
//	c.Publish(next)          // 发布方：原子替换，代次 +1
//	v, gen := c.Load()       // 读侧：一致的一对
//	if c.Version() > seen {  // 「换过没有」：单调代次，不吃 ABA
//	    seen = gen
//	}
//
// 发布出去的值视为**不可变**：Load 返回的是发布值的原样读出（指针类型是
// 零拷贝引用），发布方 Publish 之后不得再就地修改它——否则读取方看到撕裂
// 状态（数据竞争）。要改先拷一份再发布。
type VersionedCell[T any] struct {
	cur atomic.Pointer[cellState[T]]
}

// cellState 是一次发布的不变快照：值与代次同生同灭，读侧一次 Load 拿到
// 一致的一对，不存在「值换过但代次没动」的窗口。
type cellState[T any] struct {
	val T
	gen uint64
}

// NewVersionedCell 用初始值构造，代次 0；此后每次 Publish +1。想知道
// 「外界发布过没有」，看 Version() > 0。
func NewVersionedCell[T any](initial T) *VersionedCell[T] {
	c := &VersionedCell[T]{}
	c.cur.Store(&cellState[T]{val: initial, gen: 0})
	return c
}

// Publish 原子发布新值，每次成功调用恰好推进一代。
//
// 并发安全：多个发布者同时调用，各自重试（CAS 循环，从**观察到的**上一代
// 推导下一代）——代次唯一、单调、不回卷，不会重复也不会丢失。这是两侧
// 来源（Snapshot.Publish / Store.Update）的共同行为，故本层采用重试语义而
// 非「冲突即失败」。
//
// 值策略归包装层：指针类型的 nil 会**原样发布**并推进代次（「nil 忽略」
// 「nil→空配置」等口径由消费方在调用本方法之前自行判定）。
func (c *VersionedCell[T]) Publish(v T) {
	next := &cellState[T]{val: v}
	for {
		cur := c.cur.Load()
		if cur == nil {
			// 零值 cell 首次发布：nil 单元视作 gen 0（与 core/governance
			// 零值 Store 首次 Update 的口径一致），此后正常 +1。
			next.gen = 1
		} else {
			next.gen = cur.gen + 1
		}
		if c.cur.CompareAndSwap(cur, next) {
			return
		}
	}
}

// Load 返回**一致的一对**：当前值与它的代次。
//
// 需要「拿到值的同时知道它是第几代」（典型：据此决定派生状态要不要重建）
// 时必须用它，而不是 Version() 单独调用——两次调用之间可能插进一次发布，
// 拿到的值与代次就会来自不同代。
//
// 零值 cell（未发布过）返回零值与代次 0；「零值是否合法」由包装层解读
// （见类型文档）。
func (c *VersionedCell[T]) Load() (T, uint64) {
	if s := c.cur.Load(); s != nil {
		return s.val, s.gen
	}
	var zero T
	return zero, 0
}

// Version 返回当前代次：构造值 0，此后每次 Publish +1。零值 cell 返回 0。
//
// 它是发布单元的投影（与值同一次原子替换），并发发布下依然单调、唯一、
// 不回卷；用来回答「换过没有」而不是「值是不是同一个」（指针比对会被 ABA
// 与「相同内容重新发布」骗过）。
func (c *VersionedCell[T]) Version() uint64 {
	if s := c.cur.Load(); s != nil {
		return s.gen
	}
	return 0
}

// String 便于在日志里打印当前代次（不含值——值里常有密钥，而 fmt 的 %v
// 会把它们一并打出来）。
func (c *VersionedCell[T]) String() string {
	var zero T
	_, gen := c.Load()
	return fmt.Sprintf("container.VersionedCell[%T]{gen:%d}", zero, gen)
}
