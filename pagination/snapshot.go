package pagination

import (
	"reflect"
	"sync"
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
// 边界（如实声明）：struct 字段全部导出时逐字段递归完整深拷贝；含未导出
// 字段的 struct 走整体 memcpy（reflect 无法逐个 Set 未导出字段——time.Time
// 等含未导出字段的值类型依赖这一路），其未导出字段内的引用成员保持共享；
// 上游经 unexported 指针字段原地覆写的形态超出快照能力。
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
func deepCopyItem[T any](it T) T {
	rv := reflect.ValueOf(&it).Elem()
	return deepCopyValue(rv).Interface().(T)
}

// deepCopyValue 递归拷贝引用结构：指针/slice/array/map/interface 展开重建，
// 数字/字符串/bool 等值类型直接赋值，func/chan/UnsafePointer 共享（不可拷贝、
// 无原地覆写语义）。struct 走法见 pageSnapshot 的边界声明。
func deepCopyValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		c := reflect.New(v.Type().Elem())
		c.Elem().Set(deepCopyValue(v.Elem()))
		return c
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		c := reflect.New(v.Type()).Elem()
		c.Set(deepCopyValue(v.Elem()))
		return c
	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Cap())
		for i := 0; i < v.Len(); i++ {
			c.Index(i).Set(deepCopyValue(v.Index(i)))
		}
		return c
	case reflect.Array:
		c := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			c.Index(i).Set(deepCopyValue(v.Index(i)))
		}
		return c
	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		c := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			c.SetMapIndex(deepCopyValue(iter.Key()), deepCopyValue(iter.Value()))
		}
		return c
	case reflect.Struct:
		allExported, hasRef := structCopyPlan(v.Type())
		if !allExported || !hasRef {
			// 整体 memcpy：两侧均非只读，含未导出字段也允许（值赋值语义）。
			c := reflect.New(v.Type()).Elem()
			c.Set(v)
			return c
		}
		c := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			c.Field(i).Set(deepCopyValue(v.Field(i)))
		}
		return c
	default:
		c := reflect.New(v.Type()).Elem()
		c.Set(v)
		return c
	}
}

// structCopyPlan 判 struct 的拷贝走法：字段是否全导出、是否含引用成员
// （决定整体 memcpy 还是逐字段递归）。
func structCopyPlan(t reflect.Type) (allExported, hasRef bool) {
	allExported, hasRef = true, false
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			return false, false
		}
		if typeContainsRef(f.Type) {
			hasRef = true
		}
	}
	return allExported, hasRef
}
