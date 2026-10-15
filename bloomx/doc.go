// Package bloomx 提供进程内布隆过滤器。
//
// 用于本地去重与存在性预检：Test 返回 false 表示一定不存在，
// 返回 true 表示可能存在（误判率受 fpRate 控制）。适合缓存穿透防护、
// 重复事件过滤等场景；仅内存存活，不做持久化与跨进程共享。
//
// # 用法
//
//	bf := bloomx.New(10000, 0.01) // 预计 1 万元素、1% 误判率
//	bf.Add("event-42")
//	bf.Test("event-42") // true（可能存在）
//	bf.Test("other")    // false（一定不存在）
//
// # 设计说明
//
//   - 非法入参（fpRate 不在 (0,1)、estElements<=0）归一为文档化默认值，
//     不返回 error——参数错了只是精度损失，不值得让调用方写错误分支
//   - 哈希用 Kirsch-Mitzenmacher 双哈希（h1/h2 均基于 encoding.Fnv1a64，
//     消费前过 64 位 finalizer 补 FNV 低位雪崩），每元素只算两次 FNV
//     派生 k 个位置
//   - 并发安全（sync.RWMutex）：Add/Reset 走写锁，Test 走读锁
package bloomx
