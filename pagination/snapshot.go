package pagination

import (
	"reflect"
	"sync"

	"github.com/oekit/pkg/reflectx"
)

// 页快照（#285/#318）：伪翻页检测与已收集数据都以「上一页快照」为基准，
// 快照与 fetch 返回的切片必须彻底脱钩——纯值类型逐值拷贝即可，含引用成员的
// 元素类型（[]*T 是文档示例里的常态形态）必须按引用结构深拷贝：浅拷贝只复制
// 指针本身，fetch 原地覆写池化缓冲后快照与当页持有同一批指针，DeepEqual 对
// 相等指针恒真 ⇒ 第 2 页被误判成伪翻页静默丢页；Walk 返回的已收集条目同样
// 会被下一页覆写（页 1 槽位、页 2 内容）。

// refKindCache 缓存「元素类型是否含引用语义成员」的判定：按类型走 reflect
// 只需一次（WalkEach/Walk 每页都做快照，类型固定）。
var refKindCache sync.Map

// typeContainsRef 判类型是否含引用语义成员（指针/map/slice/interface，含
// struct/array 内嵌）——此类元素浅拷贝会与 fetch 缓冲同源。func/chan 不算
// （不可拷贝也无「原地覆写」语义，共享即安全）。类型级递归不可能成环
// （struct 无法按值包含自身，slice/map/指针到引用种类即停）。
func typeContainsRef(t reflect.Type) bool {
	if v, ok := refKindCache.Load(t); ok {
		return v.(bool)
	}
	r := false
	switch t.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		r = true
	case reflect.Array:
		r = typeContainsRef(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if typeContainsRef(t.Field(i).Type) {
				r = true
				break
			}
		}
	}
	refKindCache.Store(t, r)
	return r
}

// staticType 返回 T 的静态反射类型（接口元素取接口类型本身，而非首个元素的
// 动态类型——[]any 的元素可能为 nil）。
func staticType[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}

// pageSnapshot 把 fetch 返回的一页拷成**独立快照**：元素类型含引用成员时按
// 引用结构深拷贝，纯值类型退化为整片拷贝（零反射开销，行为与逐值拷贝一致）。
//
// 边界（如实声明）：struct 先整块值拷贝保住未导出字段（reflect 无法逐个
// Set 未导出字段——time.Time 等含未导出字段的值类型依赖这一路），再把可
// Set 的**导出**引用型字段逐一深拷贝替换；未导出字段内部的引用成员保持
// 共享（reflect 无法探入），上游经 unexported 指针字段原地覆写的形态超出
// 快照能力。走法单源见 reflectx.DeepCopyValue。
func pageSnapshot[T any](items []T) []T {
	if len(items) == 0 {
		return nil
	}
	if !typeContainsRef(staticType[T]()) {
		out := make([]T, len(items))
		copy(out, items)
		return out
	}
	out := make([]T, len(items))
	for i, it := range items {
		out[i] = deepCopyItem(it)
	}
	return out
}

// deepCopyItem 单元素深拷贝（经 T 的静态反射视图——nil 接口也拿得到类型）。
// 实现单源在 reflectx.DeepCopyValue（core#130 治本：与 oekit/core/config 的
// deepCopyValue 收敛为唯一实现，含 core#102 指针环防护）。
func deepCopyItem[T any](it T) T {
	return reflectx.DeepCopy(it)
}

// deepCopyValue 已收敛到 reflectx.DeepCopyValue（core#130 治本）：递归拷贝
// 引用结构 + core#102 指针环防护的唯一实现在 pkg/reflectx，本包经
// deepCopyItem（reflectx.DeepCopy）消费。struct 走法为「整块值拷贝保未导出
// 字段 + 可 Set 的引用型字段逐一递归替换」——比旧实现（含未导出字段的
// struct 整块 memcpy、导出引用字段一并共享）的引用隔离**更紧**，快照独立
// 性只增不减；func/chan/UnsafePointer 共享（不可拷贝、无原地覆写语义），
// 见 pageSnapshot 的边界声明。
