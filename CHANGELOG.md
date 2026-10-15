# Changelog — oekit/pkg

由 ekit v0.34.0（git.enjoye.top/enjoydream/ekit，冻结维护）拆分而来。所有 notable 变更记录如下。


## [v0.3.0] — 2026-10-16


## [v0.2.0] — 2026-10-16

- feat: 吸收 cloudwego/gopkg——`bufiox` 零拷贝缓冲 IO（Reader/Writer 接口 + Bytes/Default 双实现，
  Next/Peek 零拷贝视图 + Malloc/WriteBinary 攒批 + net.Buffers writev）与 `strmap` GC 友好只读
  字符串 map（键打包 + int32 哈希表 + maphash，StrMap[V]/Str2Str）；均为 stdlib-only 独立重写
- 矩阵 DESC 同步：pkg 16 合 1 → 18 合 1

## [v0.1.3] — 2026-10-15

- go 版本降级至 1.24（矩阵分层终态）

## [v0.1.2] — 2026-10-15

- org 迁移收尾 patch（yi-nology → openekit → oekit，module path 统一 github.com/oekit/pkg）

## [v0.1.1] — 2026-10-15

- 16 微库归一为单 module（pkg 16 合 1）

## [v0.1.0] — 2026-10-15

- 首次发布：errors/validator/stringx/slicex/enumx/convert/encoding/jsonrepair/httpjson/mathx/pathx/pagination/ptrx/timex+cron/uidgen/cache
