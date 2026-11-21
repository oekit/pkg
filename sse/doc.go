// Package sse 提供 SSE（Server-Sent Events）的游标回放内核与 net/http
// 应答器：游标窗口回放 + 「订阅先于回放」按事件 ID 去重 + 心跳注释帧。
// 传输无关的 [ParseCursor]/[Cursor]/[ReplaySince]（ctx 感知形态
// [ReplaySinceCtx]）是内核导出面，任何传输的响应端都可组合复用；
// [Responder] 是 net/http 的薄适配（响应头四件套 + 写帧 + 心跳），宿主只
// 注入取数与广播两处业务差异（[ReplayFunc]/[ReplayFuncCtx] 取游标窗口、
// [Source] 供实时帧）。stdlib-only（pkg 零第三方依赖红线）。
//
// # 定位与分工
//
// 本包是**应答器级**语义（响应头四件套、游标回放窗口、订阅/回放重叠
// 去重、心跳保活、慢消费者断流自愈），不是 Writer 级原语。oekit 矩阵
// 中 hertz/sse 已确立「SSE 是 oekit 的地盘」，但它绑定 Hertz
// RequestContext 且只有 Send/SendRaw——任何「DB 持久化事件 + 实时订阅」
// 的 net/http 事件流 API（挂件事件流、OpenAI 兼容流等）都还要各自手抄
// 这套应答器样板（下沉前两个消费点各约 60 行）。本包与 hertz/sse 并列：
// net/http 形态拿应答器，Hertz 形态拿 Writer 原语，互不替代。
//
// # 订阅先于回放：无缝隙与去重边界
//
// 建流次序是本包的核心不变量：**先建立实时订阅，后回放游标窗口**。
//
//   - 若先回放后订阅，「回放结束」与「订阅生效」之间存在缝隙——缝隙内
//     发生的实时帧永久丢失（DB 里已落、订阅里没有，谁也不会再发一遍）。
//     先订阅后回放则缝隙归零：订阅建立之后的每一帧实时面都会送达。
//
//   - 代价是重叠：订阅建立后、回放进行中，宿主广播的实时帧与回放窗口
//     尾部可能是同一批事件。去重边界以**事件 ID** 为准：回放结束后，
//     凡 `ID <= 已回放最大游标` 的实时帧一律跳过（幂等跳帧）。因此：
//
//     1. 事件 ID 必须由宿主从 **1** 开始**单调递增**分配（与 SSE `id:` 帧
//     语义一致，同时是断线重连的游标）。0 保留给「无游标」——Last-Event-ID
//     缺省值，不是合法事件 ID（#564）；
//     2. [ReplayFunc] 必须按 ID 升序返回、且只返回 `ID > after` 的事件；
//     3. 去重只保证「同一 ID 不写两次」，不保证「宿主重复广播不同 ID
//     的同一逻辑事件」——那是宿主事件模型的职责。
//
//   - 客户端游标取 SSE 标准的 `Last-Event-ID` 请求头（EventSource 重连
//     自动携带）；缺省视为 0（全量回放）。头在场但不是合法无符号整数
//     时按 400 拒绝（fail-closed：非法游标多半是客户端 bug，静默按 0
//     全量回放会放大成一次昂贵的误回放）。
//
// # 传输无关内核（任何响应端必须遵循的装配纪律）
//
// [ParseCursor]、[Cursor]、[ReplaySince]/[ReplaySinceCtx] 把游标判定与
// 回放/去重语义从 net/http 中解耦出来。**任何传输的响应端**（net/http、
// Hertz、WebSocket 网关等）都应遵循同一纪律，且不得另写一套：
//
//  1. **先订阅、再按游标回放**：游标头经 [ParseCursor] 解析（空 = 0 从
//     头、非法 = 400），然后先 [Source.Subscribe] 建立实时面，再调用
//     [ReplaySinceCtx]（无 ctx 场景可用 [ReplaySince]）取窗口——顺序反了
//     会留下丢帧缝隙；
//  2. **按 Event.ID 去重**：回放事件先写；实时帧一律经 [Cursor.Accept]
//     判定，`ID <= 已回放最大 ID` 的帧（含 ID=0 哨兵）跳过（订阅/回放
//     重叠与同 ID 重复广播都只写一次）；
//  3. 事件 ID 必须由宿主从 1 开始单调递增分配，[ReplayFunc]/
//     [ReplayFuncCtx] 按 ID 升序返回。
//
// 骨架（传输层只替换 write 与心跳）：
//
//	after, ok := sse.ParseCursor(lastEventID)
//	if !ok { /* 400 */ }
//	live, cancel := src.Subscribe()
//	defer cancel()
//	backlog, cursor := sse.ReplaySinceCtx(req.Context(), after, replay)
//	for _, e := range backlog { write(e) }
//	for e := range live {
//	    if cursor.Accept(e) { write(e) }
//	}
//
// # 心跳注释帧
//
// 空闲期每 [DefaultHeartbeat]（15s）写一帧 SSE 注释帧（冒号开头、
// EventSource 客户端忽略的行，如 `: keepalive`，本包称之为「ping」形态
// 的保活帧），仅用于填满字节流。语义：HTTP 长连接上的反代/负载均衡
// （Nginx 默认 60s、常见云 LB 30~60s）会对无字节流动的连接掐流，
// 注释帧让空闲流在客户端视角仍然安静、在链路视角一直活着。15s 的取值
// 理由：须低于常见中间层 30~60s 的空闲读超时并留出整数倍余量——按仓规
// 「跨区间取值须在包文档声明理由」在此声明。可用
// [WithHeartbeat] 改间隔、[WithHeartbeatComment] 改注释文案（仍须是
// 冒号开头的单行注释，否则客户端会当事件解析）。
//
// # 慢消费者自愈
//
// 每个订阅者有独立缓冲（[WithSubscriberBuffer]）。广播时缓冲满的订阅者
// 被判定为掉队者：其流被立即断开（客户端看到流结束），客户端按 SSE
// 标准带 `Last-Event-ID` 重连，漏掉的窗口由 [ReplayFunc] 回放补齐——
// **丢弃帧必须以断流收场**（静默丢帧会破坏游标契约：客户端以为收全了），
// 丢帧-重连-补窗正是游标回放的自愈闭环。
//
// # 用法
//
//	hub := sse.NewHub() // 实时面：进程内广播枢纽
//	replay := func(ctx context.Context, after uint64) []sse.Event { // 取数面：宿主 DB
//	    return db.EventsAfter(ctx, after) // 客户端断开时 ctx 取消，查询随之中止
//	}
//	mux.Handle("/events", sse.NewResponderCtx(hub, replay))
//
//	// 事件落库成功后广播实时面：
//	hub.Emit(sse.Event{ID: next, Type: "run.step", Data: string(json)})
//
//	// 自带跨进程总线的宿主实现 Source 即可接入同一应答器：
//	// type MyBus struct{...}; func (b *MyBus) Subscribe() (<-chan sse.Event, func())
package sse
