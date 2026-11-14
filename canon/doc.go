// Package canon 提供「策略/配置面规范形指纹」原语：规范形（canonical）
// → schema 版本化哈希（48bit）→ 按 facet diff。任何「规则改过之后能
// 回答会话按哪版策略放行、两台机器授权面是否同版、按 facet 吊销受影响
// 面」的策略/配置治理，都复用同一条管线：配置面 → 排序/去重/缺省折算
// → 版本哈希 → facet diff。stdlib-only（pkg 零第三方依赖红线）。
//
// # 三条归一纪律（同语义必须同版本）
//
// 「同语义必须同版本」由三条纪律钉死，前两条本包直接给原语，第三条
// 是宿主职责、必须在喂进 Facet 前完成：
//
//   - **排序**：集合语义字段（能力词、权限清单）声明顺序不是语义，
//     改一下排列不该换版本——用 [SortDedup] 排序后喂入；
//   - **去重**：同一词写两遍与写一遍是同一个能力集（去重后才是集合
//     语义）——同上，[SortDedup] 一并完成；
//   - **缺省折算**：{}（缺省）与显式写缺省值必须同版。缺省表是宿主
//     的业务语义，本包不内置：宿主用**单源折算函数**（如 admission
//     的 Effective）把「有效值」折算出来再喂入，禁止折算逻辑散落两处。
//     注意结构体显式零值与 map 缺键在 JSON 规范形里是两种形态
//     （"allow":false 与缺键），折算必须在喂入前完成，不能指望哈希层
//     兜底。
//
// # canonical 稳定性（键序无关、字段序钉死）
//
// 规范形 = schema 串 + facet 名 → 值的 JSON 形态，整体一次 JSON 化：
//
//   - **map 键序无关**：encoding/json 对 map 键按字典序输出，键插入
//     顺序不进哈希；
//   - **facet 声明序无关**：facet 按 Name 字典序进规范形——清单排列
//     不是语义；
//   - **结构体字段序钉死**：结构体按字段声明序输出——字段序是规范形
//     的一部分。因此「改 canonical 结构」（增删字段、换字段序、改类型）
//     都会换哈希，**必须同时升格 Schema 串**（如 policyver/v1 →
//     policyver/v2）：版本串里的 schema 是读方识别旧形态的判据，
//     升格纪律是契约条文，本包按 schema 不一致的 diff 直接拒绝兜底。
//
// # 48bit 的取舍
//
// 版本哈希取 sha256 前 48bit（12 个 hex 字符）。**只用于对比，不用于
// 防篡改**：指纹回答「这两个面是否同版」，48bit 碰撞概率在「同语义
// 必须同版本」的输入域下可忽略，但绝不是密码学承诺——任何把版本串当
// 完整性证明的用法都是错的（防篡改请用完整 sha256 或签名）。
//
// # facet diff
//
// [Diff] 按 facet 名逐片比较各自规范形指纹，产出 Added/Changed/Removed
// 清单（同版 facet 不报）。按 facet 吊销受影响面（会话按旧版放行的
// 记录、mandate 重检）直接消费这份清单；[FacetDigest] 可独立持久化
// 单个 facet 的指纹（会话行记 policy_version + 命中 facet 集）。
//
// # 用法
//
//	surface := canon.Surface{
//	    Schema: "policyver/v1", // 宿主自己的 schema 命名空间
//	    Facets: []canon.Facet{
//	        {Name: "role:admin", Value: canon.SortDedup(conf.AdminWords)},
//	        {Name: "admission", Value: admission.Effective(conf.Admission)},
//	    },
//	}
//	ver, err := canon.Fingerprint(surface) // "policyver/v1:3f9a1c2b4d5e"
//	changes, err := canon.Diff(oldSurface, surface)
//	for _, ch := range changes { // 按 facet 吊销受影响面
//	    revokeFacet(ch.Name, ch.Kind)
//	}
package canon
