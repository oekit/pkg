// Package auditchain 提供 append-only 审计哈希链的纯计算内核——防篡改审计
// 留痕（合规类产品经典需求）的可复用单源：canonical 串构造（\x1f 分隔、字段序
// 由宿主注入）、sha256 链哈希、创世锚、逐行重算 + 前后链接核对的 verify 走查
// （首个断点即停）、时间归一（UTC + 微秒截断）。纯 stdlib，零存储假设——
// gorm 咨询锁 / busyRetry / 追加事务等方言件留在宿主，本包不做任何 IO。
//
// # canonical 单源纪律
//
// 写入方（追加落库）与校验方（链走查）必须共享同一个 Chain——即同一个
// CanonicalFunc 字段序定义：任何一方私改字段序，从改动点起的每一行哈希重算
// 都对不上，全链假断。这是包契约而不是文档倡议：Canonicalize 是唯一出口，
// 落库的行哈希与走查时的 Record.Canonical 都必须由同一实例产出。字段内若
// 可能出现 \x1f（业务文本不可信时），由宿主 canonical 先消毒——内核不做
// 猜测性转义，因为转义规则本身也是 canonical 的一部分，双方不一致即分叉。
//
// # 时间归一是读回一致的前提
//
// NormalizeTs = UTC + 微秒截断。PG timestamptz 精度上限微秒：纳秒入哈希，
// 任何方言读回（微秒精度）重算必假断。写入方入链前必须归一——它是双方言
// 读回一致的前提，不是可选优化。
//
// # 追加（宿主侧示意，方言件留宿主）
//
//	chain, err := auditchain.NewChain(func(row auditRow) []string {
//	    return []string{row.ID, row.Ts, row.Actor, row.Action, row.PrevHash}
//	})
//	// 追加事务内（宿主方言件：咨询锁串行化 → 读链尾 → INSERT → 回填哈希）：
//	row.PrevHash = lastHash // 空表取 auditchain.GenesisHash
//	row.Ts = auditchain.NormalizeTs(now)
//	row.Hash = chain.HashRow(row)
//
// # 走查
//
//	res, err := auditchain.Verify(src) // src 为 RecordSource 行迭代器（DB 分批扫描适配）
//	// 锚定语义三档：
//	//   缺省：首行必须衔接创世锚 GenesisHash（未清扫链头的部署 = 完整证明）；
//	//   WithAnchor(h)：首行衔接锚行哈希（从链中段起查）；
//	//   WithUnanchoredHead()：保留清扫截断链头的部署，首行只查自洽——头行
//	//   截断本身不可由本地链证明（防截断靠外发 SIEM 侧对账）。
//	// 首个断点即停：OK=false + FirstBrokenID + Reason（后续行 PrevHash 全部
//	// 对不上，多报无益）。
package auditchain
