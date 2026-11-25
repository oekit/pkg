// Package stringx 提供常用的字符串操作工具函数。
//
// 包含字符串查找、过滤、截取、反转、大小写转换、多模式替换
// （Replacer trie 最长匹配 / NewMask 敏感词遮罩）等功能。
// 所有函数均安全处理 UTF-8 编码的字符串。
// 生命周期: Stable——同一 minor 线内只允许 additive 变更（新增导出符号 / 新增方法且给零值默认实现 / 新增错误值）；跨 minor 的破坏性变更须先降级并公告（判定规则与流程见 docs/api-compat.md，逐包理由见 api-lifecycle.yaml）。
package stringx
