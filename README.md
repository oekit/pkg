# pkg

> oekit 微工具集——16 个零依赖（或近零依赖）的基础工具包，合为一个 module 按需 import。

[errors](https://github.com/oekit/pkg/tree/main/errors) · [validator](https://github.com/oekit/pkg/tree/main/validator) · [stringx](https://github.com/oekit/pkg/tree/main/stringx) · [slicex](https://github.com/oekit/pkg/tree/main/slicex) · [enumx](https://github.com/oekit/pkg/tree/main/enumx) · [convert](https://github.com/oekit/pkg/tree/main/convert) · [encoding](https://github.com/oekit/pkg/tree/main/encoding) · [jsonrepair](https://github.com/oekit/pkg/tree/main/jsonrepair) · [httpjson](https://github.com/oekit/pkg/tree/main/httpjson) · [mathx](https://github.com/oekit/pkg/tree/main/mathx) · [pathx](https://github.com/oekit/pkg/tree/main/pathx) · [pagination](https://github.com/oekit/pkg/tree/main/pagination) · [ptrx](https://github.com/oekit/pkg/tree/main/ptrx) · [timex](https://github.com/oekit/pkg/tree/main/timex) · [uidgen](https://github.com/oekit/pkg/tree/main/uidgen) · [cache](https://github.com/oekit/pkg/tree/main/cache)

## 安装

```bash
go get github.com/oekit/pkg@latest
```

```go
import (
    "github.com/oekit/pkg/errors"
    "github.com/oekit/pkg/stringx"
)
```

> Go 按包编译：import 一个子包不会引入其他子包的代码；module require 只有一条。

## 包清单

| 包 | 说明 | 三方依赖 |
|---|---|---|
| `errors` | Wrap/Cause 链、BizError（业务码/HTTP 码双轨）、sentinel | — |
| `validator` | go-playground 封装，ValidationError→BizError(400) | validator/v10 |
| `stringx` | ID 生成、关键词匹配、缩进、复数化、模板替换 | — |
| `slicex` | Partition 二分组、GroupBy | — |
| `enumx` | 确定性顺序 All/Values | — |
| `convert` | 字符串/数字/布尔/切片宽容转换 | — |
| `encoding` | JSON/msgpack 序列化助手 | msgpack |
| `jsonrepair` | 宽容修复链（永不失败，回退原文） | — |
| `httpjson` | DoJSON 带重试语义与 Retry-After | — |
| `mathx` | 安全随机、评测统计 | — |
| `pathx` | glob 匹配（`vendor/**`） | — |
| `pagination` | PageRequest/PageResponse | — |
| `ptrx` | To/From/安全解引用 | — |
| `timex`（+`cron`） | 时间格式化/解析 + cron 表达式 | — |
| `uidgen` | UUID/雪花/随机串 | uuid |
| `cache` | 刷新检查器（TTL/failTTL/inflight 合并） | — |

内部互引仅两条：`validator → errors`、`httpjson → stringx`，其余全部只依赖标准库。

## 历史

原为 16 个独立微库（oekit/errors、oekit/stringx…），2026-10 合并为本 module——
旧仓已归档并重定向；import 路径迁移：`github.com/oekit/<pkg>` → `github.com/oekit/pkg/<pkg>`。

## License

MIT — 见 [LICENSE](LICENSE)
