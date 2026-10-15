// Package strmap GC 友好的只读字符串 map：键打包进单一 []byte、int32 开放寻址
// 哈希表，配合 maphash 种子哈希，避免为海量静态映射（如 header/路由/字典表）
// 长期持有海量小字符串对象带来的 GC 扫描开销。
//
// 语义约束（按设计取舍）：
//   - 构建后只读（Load 重建整体替换，无单键增删）；
//   - 键与值的内存布局为性能而设，值类型 V 建议不含指针（含指针不会错，只是失去 GC 收益）；
//   - Item/Get 返回的键字符串是底层数组的零拷贝视图，StrMap 存活期间保持有效，
//     不得对其写入（底层只读保证）。
//
// 设计参考 cloudwego/gopkg/container/strmap（Apache-2.0），本实现为独立重写
// （MIT，oekit/pkg）：哈希表定容、打包键、零拷贝视图思路一致；去掉了对
// unsafex 的依赖（Go 1.20+ 直接用 unsafe.String/Slice），长度前缀改用
// binary.NativeEndian，并在空表 Get、重复键等边界上给出确定性错误。
package strmap
