// Package encoding 提供常用的编码和哈希工具函数。
//
// 当前能力：JSON 编解码包装（MarshalJSON/UnmarshalJSON 等，UseNumber 语义
// 有显式入口 UnmarshalUseNumber/UnmarshalFromStringUseNumber——数字保留为
// json.Number，防 >2^53 float 精度丢失）、非加密哈希
// （Fnv1a64）、加密级摘要（Sha256/Sha256Hex）、Base64（标准/URL 安全，
// 各含**带填充**与**无填充**两套变体——JWT / OIDC / PKCE 协议段用无填充那套）。
//
// 历史说明：曾提供 Md5/Md5Hex/Md5Bytes/Sha1 弱哈希包装，2026-09-15 治理轮
// 移除（共享 SDK 提供弱哈希入口容易被误用到凭据/签名场景）；确有 MD5/SHA-1
// 合规需求的业务自行使用 crypto 标准库。
// 生命周期: Stable——同一 minor 线内只允许 additive 变更（新增导出符号 / 新增方法且给零值默认实现 / 新增错误值）；跨 minor 的破坏性变更须先降级并公告（判定规则与流程见 docs/api-compat.md，逐包理由见 api-lifecycle.yaml）。
package encoding
