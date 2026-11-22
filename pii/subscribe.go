// 处置策略变更订阅：把「换过没有」从**可查询**变成**可响应**。
//
// # 为什么单指针还不够
//
// 上面那份 Policy 解决的是**读的一致性**：一次 Load 拿到的就是同一份策略，不会读到
// 「面已关却回报阻断」这种从未被任何写侧设置过的组合。但它只让读侧自洽，写侧的
// 下游仍然是空的——Gen() 能回答「换过没有」，回答不了「换成了什么」，宿主只能自己
// 起轮询比对，而两次比对之间若换了两次，代次只说得出「变了」。
//
// 订阅把这件事一次做完：变更发生的**那一刻**由写侧把新快照推给订阅者，收到即所见。
//
// # 订阅表的机制不在本文件
//
// 「快照式替换的订阅表 + 铸造句柄 + 锁外通知 + 回调 panic 隔离」这套机制在
// internal/subhub，与 netguard、guardset 共用**同一份实现**。它此前在本仓有三份
// 近乎逐字相同的副本，而这是一段并发正确性代码：只要有一份先被修，另外两份不会
// 跟着改，同一个不变量于是在三个包里有三种成色，失效形态是**偶发漏通知 /
// 偶发误留订阅**——不红不报，只在长跑进程里慢慢显形。
//
// # 为什么回调参数是 Policy 值而不是 *Policy
//
// 读侧 CurrentPolicy() 已经是值返回，订阅沿用同型：订阅者拿到的是一份独立副本，
// 写侧后续换策略不影响它手里的那份，订阅者也无需操心指针的存活期。代价是订阅者
// 想长期留档就得自己留副本——这是显式的，优于「指针还能用吗」这种隐式问题。
//
// # 为什么订阅者 panic 不得影响策略面
//
// 一次成功的 CAS **已经落地**，此时让某个订阅者抛异常就把整次变更说成「没发生」，
// 等于把已经发生的事实改写成没发生——而这恰恰是本文件要消灭的那类失效。故逐个
// recover（实现见 subhub.Invoke），让其余订阅者继续收到通知。
//
// 另一条同源的纪律：订阅者回调里再调 SetStrategy 是**允许**的（无锁，不会自锁），
// 但那会再触发一轮通知，递归深度由订阅者自己收敛，本包不做环检测。
//
// # 为什么句柄是本包自己的类型而不是 subhub.Handle 的别名
//
// 别名会让 pii.Subscription 与 netguard.Subscription 变成**同一个类型**：把
// netguard 的句柄传给本包 Unsubscribe 将编译通过，然后静默什么也不注销。句柄各自
// 定义成独立命名类型（底层仍是 uint64），跨包误用仍是编译错误——这正是句柄存在
// 的意义（「这次注销是不是我登记的那一次」），别名把它抹掉了。
//
// # 通知顺序：并发写侧下代次必须单调不回退（issue #296）
//
// CAS 决定了「哪一代成为当前」，但不决定**通知到达订阅者的顺序**：写侧 A 在
// CAS 成功后、notify 之前被抢占，写侧 B 完成下一代 CAS 并先通知，随后 A 才把
// 旧一代推出来——订阅者会先看到 gen N+1 再看到 gen N，「收到即所见」当场失效。
// 每个订阅者因此有一个**单调投递门**（policySubscriber.deliver）：
//
//   - 比已投递代次更旧的通知直接丢弃（它已经被更新的一代取代）；
//   - 投递进行中到达的通知记为 pending，只保留代次最大的那一份，由当前投递者
//     在回调返回后补投——并发变更因此可能**合并**为最新一代，但绝不会回退；
//   - 回调在锁外执行（投递门只保护自己的状态），回调里再 SetStrategy 仍安全：
//     重入的通知进 pending，不会自锁。

package pii

import (
	"sync"

	"github.com/oekit/pkg/errors"

	"github.com/oekit/pkg/subhub"
)

// Subscription 订阅句柄，传给 Unsubscribe 注销。0 表示无效。
type Subscription uint64

// ErrNilSubscriber 订阅回调为空。
//
// 登记一个必然 panic 的订阅者毫无意义，故直接报错并返回零句柄：调用方能**凭返回值**
// 知道订阅没生效，而不是等到第一次变更时才发现「原来一直没跟上」。
//
// 判在**本包**而不是 subhub：三个使用方各有自己的哨兵与语境文案，共用一个会让
// `errors.Is(err, pii.ErrNilSubscriber)` 这类既有调用点判定链断掉。
var ErrNilSubscriber = errors.New("处置策略订阅回调为空")

// subs 处置策略变更订阅表。零值可用。
var subs subhub.Registry[Policy]

// policySubscriber 单个订阅者的**单调投递门**（机制说明见文件头）。
//
// 零值不可用之外的部分都由 Subscribe 构造；fn 非 nil 由 ErrNilSubscriber 保证。
type policySubscriber struct {
	fn    func(Policy)
	floor uint64 // 登记时看到的代次；更旧的通知是历史，丢弃

	mu         sync.Mutex
	busy       bool   // 本订阅者的回调正在执行
	seen       bool   // 是否已投递过至少一份
	last       uint64 // 已投递的最大代次
	pending    Policy // 投递期间到达的最新快照
	hasPending bool
}

// deliver 单调投递一份快照：旧代丢弃，并发到达者合并为最新一份。
//
// 回调在 mu 之外执行——回调里再 SetStrategy 会重入本函数，此时 busy=true，
// 新快照进 pending 并立即返回，由外层投递循环在回调返回后补投；不会自锁。
func (s *policySubscriber) deliver(p Policy) {
	s.mu.Lock()
	if s.busy {
		s.queueLocked(p)
		s.mu.Unlock()
		return
	}
	s.busy = true
	for {
		if !s.acceptLocked(p) {
			if !s.hasPending {
				s.busy = false
				s.mu.Unlock()
				return
			}
			p = s.pending
			s.hasPending = false
			continue
		}
		s.last, s.seen = p.Gen(), true
		s.mu.Unlock()
		subhub.Invoke(s.fn, p)
		s.mu.Lock()
		if !s.hasPending {
			s.busy = false
			s.mu.Unlock()
			return
		}
		p = s.pending
		s.hasPending = false
	}
}

// acceptLocked 判这份快照是否值得投递（调用方持 mu）。
func (s *policySubscriber) acceptLocked(p Policy) bool {
	if p.Gen() < s.floor {
		return false
	}
	return !s.seen || p.Gen() > s.last
}

// queueLocked 记下投递期间到达的最新快照（只保留代次最大者）。
func (s *policySubscriber) queueLocked(p Policy) {
	if s.hasPending && p.Gen() <= s.pending.Gen() {
		return
	}
	s.pending, s.hasPending = p, true
}

// Subscribe 登记一个处置策略变更订阅者，返回句柄与是否成功。
//
// # 为什么注册完立刻回调一次当前快照
//
// 不这么做，订阅者对「现在生效的是什么」一无所知，要等到下一次变更才知道。策略注入
// 的典型形态恰恰是「注册完之后很久没有下一次变更」，于是订阅者带着一个从没填过的
// 视图上线——比没有订阅更危险，因为它以为自己已经跟上了。
//
// # 立即回调与登记的次序：可合并，但绝不漏掉一代
//
// 立即回调先于登记执行（此时本订阅者还不在通知名单里，绝不会与并发通知交错），
// 登记后再读一次当前快照：若这段时间里发生过变更，再补投一次最新的那一代。
// 登记之后发生的变更由 notifyPolicy 正常送达。于是「取快照 → 登记」窗口内的
// 变更不会漏，而并发变更可能**合并**为最新一代（单调投递门的选择，见文件头）。
//
// 立即回调同样在锁外调用、panic 同样被 recover，与变更通知完全一致。
func Subscribe(fn func(Policy)) (Subscription, error) {
	if fn == nil {
		return 0, ErrNilSubscriber
	}
	// floor 是登记时看到的代次：比它更旧的通知是登记前的历史，投递门直接丢弃。
	sub := &policySubscriber{fn: fn, floor: Gen()}
	p := CurrentPolicy()
	sub.deliver(p) // 立即回调（尚未登记，不会与并发通知交错）
	h := Subscription(subs.Add(sub.deliver))
	if cur := CurrentPolicy(); cur.Gen() > p.Gen() {
		sub.deliver(cur) // 补上「取快照 → 登记」窗口内的变更
	}
	return h, nil
}

// Unsubscribe 注销订阅。重复注销同一句柄是安全的空操作。
//
// 有了它，订阅者才能在长生命周期场景下解绑：否则回调闭包连同它捕获的一切会被订阅表
// 永久留住，而这条泄漏不会自己显形。
func Unsubscribe(h Subscription) {
	subs.Remove(subhub.Handle(h))
}

// notifyPolicy 把新快照推给全部订阅者。**由写侧在 CAS 成功之后、锁外调用。**
//
// 遍历的是一次快照：并发增删订阅者不会让本次通知看到半张名单，且同一次变更对每个
// 订阅者送出的都是**同一份**策略——与「不可撕裂读」同源。
func notifyPolicy(p Policy) {
	subs.Notify(p)
}
