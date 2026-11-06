// Package cron 提供 Cron 表达式解析和调度功能。
//
// 支持标准 5 字段 Cron 表达式：分 时 日 月 周
//
// ⚠️ 与标准 cron 的方言差异（[#186]）：dow 支持 7≡Sunday；不支持 MON/JAN
// 名字、不支持 L/W/# 扩展；不可满足表达式（2 月 31 日）Next 返回零值
// time.Time 而非报错——从系统 crontab 迁移时注意。
package cron
