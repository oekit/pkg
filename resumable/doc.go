// Package resumable 提供「分片上传/断点续传」的会话生命周期状态机原语：
// 会话创建（含秒传钩子）、分片登记、断点查询、完成、中止、孤儿清扫，
// 状态面纯内存、存储后端接口化（分片字节落哪由 [ChunkStore] 决定），
// stdlib-only（pkg 零第三方依赖红线）。
//
// # 为什么需要它
//
// 下游 zeus 两个仓**各自独立**手搓了整套同构状态机（media 主实现 ~700 行
// + gin-vue-admin 又一份 ~400 行，pkg#361）：会话建立/分片幂等/断点对账/
// 合并抢占/孤儿清扫每个上传服务都要面对，而并发正确性的坑——CAS 进度、
// 合并抢占、missing 对账、孤儿清扫——是必须单点的语义，两份实现的防御
// 注释里各自都踩过这四个坑。业务差异其实只在**去重键**（[DedupResolver]）
// 与**合并落存储**（[MergeFunc]）两处，其余全是共同面。
//
// # 状态机
//
//	StatusUploading --ClaimForMerge--> StatusMerging --Finish--> StatusDone
//	StatusUploading --Abandon--> StatusAborted
//	StatusMerging   --ReleaseClaim--> StatusUploading（校验/合并失败回滚）
//
// 四类并发坑的单点兑现（[SessionStore] 契约 + [Manager] 编排）：
//
//   - **CAS 进度**：[Manager.PutChunk] 幂等分片 + 写后存活确认 +
//     AppendReceived 原子登记（只在收片态、重复登记幂等）；
//   - **合并抢占**：[Manager.Complete] 的 ClaimForMerge 是 0→1 原子迁移，
//     并发 Complete 恰好一个赢家进合并、输家拿 [ErrMergeInProgress]；
//   - **断点对账**：[Manager.ListParts] 双源取交（「已登记 ∧ 字节存活 ∧
//     字节数吻合」才算已收）——登记了但字节丢/残缺的分片必须回缺片清单，
//     否则断点查询会把客户端骗去 Complete 才炸；
//   - **孤儿清扫**：[Manager.Sweep] 单点收口过期会话/无主分片/合并卡死
//     （过期判定走 UpdatedAt——收片与状态迁移都会续期）。
//
// # 用法
//
//	sessions := resumable.NewMemSessionStore()      // 状态面：纯内存
//	chunks := store{...}                            // 存储面：实现 ChunkStore
//	m := resumable.NewManager(sessions, chunks)
//
//	s, uploaded, err := m.Init(ctx, resumable.InitReq{
//	    Owner: uid, Hash: h, Size: size, ChunkSize: 8 << 20,
//	}, func(ctx context.Context, hash string) (bool, error) {
//	    return objectExists(ctx, hash)               // 秒传：内容已存在
//	})
//	// uploaded=true ⇒ 秒传命中，直接复用既有对象
//
//	err = m.PutChunk(ctx, s.ID, idx, r, 8<<20)      // 幂等分片 + 进度 CAS
//	recv, missing, st, _ := m.ListParts(ctx, s.ID)  // 断点查询（双清单对账）
//	err = m.Complete(ctx, s.ID, func(ctx context.Context, s *resumable.Session, r io.Reader) error {
//	    return putObject(ctx, s.Hash, r)             // 合并落存储（流式）
//	})
//	err = m.Abort(ctx, s.ID, uid)                   // 归属校验 + 清分片
//	n, err := m.Sweep(ctx, 24*time.Hour)            // 孤儿清扫
//
// # 边界（如实声明，不伪装全知）
//
//   - **合并的幂等性**：merge 成功而 Finish 失败时会话留在合并态、由
//     Sweep 收口（不重跑 merge）；merge 失败则回滚回收片态、调用方重试
//     Complete 会**再跑一次** merge——落存储的写入要么幂等要么自持事务；
//   - **超长分片的字节**：PutChunk 流式不过缓冲，超长判定发生在读满
//     want+1 之后——存储里可能短暂留下残片（不登记、对账算 missing），
//     重传覆盖或 Abort/Sweep 清掉；
//   - **here 式的业务语义不内置**：秒传的「已存在」判定、合并的目标布局
//     （单对象/分块对象/去重库）、留存策略（完成即删 vs 定期删）都是钩子/
//     调用方的事；Sweep 按「过期即清」的单一口径走；
//   - **存储后端的一致性**：本包假定 ChunkStore 的 Put 是「以写代换」、
//     Stat 是最终一致的字节数；最终一致存储上「写后存活确认」可能把
//     读旧值判成残缺（ErrChunkSize）——分片幂等重传即可修复，不影响
//     状态机正确性。
package resumable
