// Package pack 提供跨宿主迁移用的资产包容器：zip 容器 + 一份清单（manifest）。
//
// # 容器格式
//
// 一个迁移包是标准 zip 文件，包内固定含一份清单 manifest.json：
//
//	manifest.json   清单（格式版本、创建时间、各文件行数、各文件 SHA-256 摘要）
//	<其它条目>     数据文件，一行一条 JSON（JSONL）
//
// 选 zip 而不是自定义二进制格式，是因为迁移包要能被人在没装本程序的机器上打开看看里面是什么。
// archive/zip 是标准库，但「容器 + 清单 + 每文件摘要 + 格式版本 + 体检」这一整套若每个要导出
// 资产的仓各写一遍，就会出现格式漂移；本包把这套约定固化下来。
//
// # 写
//
//	w := pack.NewWriter(f, pack.CurrentFormatVersion)
//	n, err := w.AddJSONL("users.jsonl", rows)
//	m, err := w.Close() // 清单落盘并返回
//
// # 读
//
//	arc, err := pack.Open(r, size)
//	err = arc.ReadJSONL("users.jsonl", func(row json.RawMessage) error { ... })
//
// # 体检
//
//	m, problems, err := pack.Inspect(r, size)
//
// Inspect 一次说清所有问题（缺哪几个文件、哪个摘要不符、哪个条数对不上），
// 而不是遇到第一个错误就停；err 只在两类「不可导」原因时返回，且可用 errors.Is 区分：
//
//	ErrCorrupt           包损坏：zip 读不开、清单缺失/损坏、清单 JSON 解析失败
//	ErrUnsupportedVersion 版本不认识：清单的 format_version 与调用方期望不符
//
// # rows 支持范围
//
// AddJSONL 接受「任意可 JSON 编码的行集合」，逐行编码后流式写入，不把整包读进内存：
//
//	[]any              元素任意可 JSON 编码（map/string/int/结构体指针……）
//	[]map[string]any   常见结构
//	[]struct{...}      结构体切片
//	[]T                任意元素类型的切片/数组
//
// 约束：rows 必须是切片或数组（元素零值也接受）；不接受 nil、单个值、map、字节切片
// （[]byte 与「行集合」语义冲突，直接报错而不是写出一行 base64）。
//
// # 摘要
//
// 清单里的摘要是数据文件内容的 SHA-256 十六进制值，与 github.com/oekit/pkg/encoding
// 的 Sha256Hex 同算法同结果（流式场景用增量 hasher 累积，等价关系由契约测试锁定）。
//
// # 文件名安全
//
// 写入与读取都做文件名净化：含路径分隔符、含 ".."、绝对路径（含 Windows 盘符）、
// 控制字符的条目名一律拒绝，读取时记入问题清单——防 zip slip 路径穿越。
package pack
