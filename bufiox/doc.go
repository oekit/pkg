// Package bufiox 面向协议编解码的缓冲 IO：读侧 Next/Peek/Skip 返回内部缓冲的
// 零拷贝视图（省去逐字段拷贝），写侧 Malloc/WriteBinary 攒批后一次 Flush
// （零拷贝引用调用方切片）。典型用法是在自定义二进制协议解析中，先 Peek 定长
// 头、按长度字段 Next 出负载视图，解析全程零拷贝，最后 Release 归还。
//
// 错误约定：
//   - Next/Peek/Skip 数据不足返回 ErrNoRemainingData（底层读错误原样透出）；
//   - ReadBinary 按经典 io 语义：读满 (len(bs), nil)、部分读 (n, io.ErrUnexpectedEOF)、
//     无数据 (0, io.EOF)；
//   - Release 后 Next/Peek 返回的视图全部失效（缓冲被重置）。
//
// 与 bufio 的区别：bufio.Read 必须拷贝进调用方切片；本包的读路径可以把缓冲区
// 视图直接交给调用方，Write 路径可以引用调用方内存攒批。与标准库 bytes 包的
// Reader/Writer 的区别：本包面向流（io.Reader/io.Writer 适配）并带 Release
// 生命周期。
//
// 设计参考 cloudwego/gopkg/bufiox（Apache-2.0），本实现为独立重写（MIT，
// oekit/pkg）：接口语义一致；去掉了对 bytedance/gopkg mcache/dirtmake 的依赖，
// 缓冲改用普通分配（GC 回收，无池化归还需要跟踪）。
package bufiox
