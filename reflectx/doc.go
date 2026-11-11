// Package reflectx 提供反射层的共享原语（全矩阵单源）。
//
// 目前承载的是**深拷贝原语**：同一份「reflect.Value 递归深拷贝 + 指针环防护」
// 实现曾在 oekit/core/config（merge.go deepCopyValue，core#102 修过指针环
// 栈溢出）与 pkg/pagination（snapshot.go，core#130 pkg 侧）平行维护——两边
// 语义各自演化（core 侧 struct 拷贝保留未导出字段、pkg 侧含未导出字段的
// struct 整块 memcpy 连导出引用字段一起共享）。本包把两者收敛为唯一实现：
//
//   - core/config（core#130）与 pkg/pagination 均改引 DeepCopyValue；
//   - struct 走「整块值拷贝保未导出字段 + 可 Set 的引用型字段逐一递归替换」
//     的**强**语义（见 DeepCopyValue），两边取齐后引用隔离只紧不松。
//
// 指针环安全（core#102 的知识，随实现单源）：visited 按**指针身份**记忆已
// 拷贝节点、先注册再下探，环上第二次访问直接返回已拷贝节点；环内结构共享
// 关系原样保留（src 里两个指针指向同一块，拷贝后仍指向同一块）。
package reflectx
