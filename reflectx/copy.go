package reflectx

import "reflect"

// DeepCopyValue 递归深拷贝 v 的引用结构（map/slice/array/pointer/interface/
// struct），返回与 v 同类型的独立副本。
//
// 各类型的走法：
//   - map/slice/array：逐 key/元素递归后重建；
//   - pointer：新建目标并递归下探；**visited 按 src 指针身份记忆已拷贝节点**
//     （core#102 的环防护，随本包单源）——环上第二次访问直接返回已拷贝节点，
//     自引用/互引用结构不再无限递归（无防护时是 fatal error: stack overflow
//     崩进程，不是可恢复错误）；环内/DAG 结构共享关系原样保留；
//   - interface：解引后拷贝动态值（接口内隐藏的 map/slice 不再别名）；
//   - struct：**先整块值拷贝**（保住未导出字段——reflect 无法 Set 它们，
//     time.Time 等含未导出字段的值类型依赖这一路），再把**可 Set 的引用型
//     字段逐一替换成深拷贝**（core#84：整体赋值对引用型字段是浅拷贝，改
//     副本会反向污染源）。含未导出字段的 struct 其未导出字段内部仍可能
//     共享引用（reflect 无法探入）——这是反射深拷贝的固有边界。
//   - 其余（数值/字符串/bool/func/chan 等）：原值返回（func/chan 共享即拷贝，
//     二者没有可拷贝的内部引用）。
//
// 仅用于低频路径（配置合并、页快照等）——每层反射分配的开销不适合热路径。
// 调用方持有 reflect.Value 时用本函数；只有具体类型时 DeepCopy 更顺手。
func DeepCopyValue(v reflect.Value) reflect.Value {
	return deepCopyVisited(v, map[uintptr]reflect.Value{})
}

// DeepCopy 是 DeepCopyValue 的泛型糖：按 T 的静态反射视图深拷贝 v
// （nil 接口也拿得到类型），返回独立副本。
func DeepCopy[T any](v T) T {
	rv := reflect.ValueOf(&v).Elem()
	out := deepCopyVisited(rv, map[uintptr]reflect.Value{})
	if !out.IsValid() {
		var zero T
		return zero
	}
	res := out.Interface()
	if res == nil {
		// T 是接口类型且动态值为 nil：直接断言会在 nil 接口上 panic。
		var zero T
		return zero
	}
	return res.(T)
}

// deepCopyVisited 是 DeepCopyValue 的递归体；visited 键为 src 指针地址
// （拷贝期间 src 不被分配/移动，地址即身份），值为对应的新拷贝。
//
// 环防护覆盖 Pointer/Map/Slice 三类引用分支（#426）：此前只有 Pointer 登记
// visited——不经指针也能成环（m["self"]=m 的 map 自引用、s[0]=s 的 slice
// 头自引用），Map/Slice 分支直接递归 ⇒ goroutine stack exceeds fatal 打崩
// 进程，不是可恢复错误。reflect.Value.Pointer() 对 Map（底层存储指针）与
// Slice（首元素地址，len>=1 才可能自引用）同样可用，三类分支统一
// 「先注册再下探」：环上第二次访问直接返回已拷贝节点，结构共享原样保留。
func deepCopyVisited(v reflect.Value, visited map[uintptr]reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	switch v.Kind() {
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		if out, ok := visited[v.Pointer()]; ok {
			return out // 环闭合（#426）：返回已拷贝节点
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		visited[v.Pointer()] = out // 先注册再下探
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(deepCopyVisited(iter.Key(), visited), deepCopyVisited(iter.Value(), visited))
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		if out, ok := visited[v.Pointer()]; ok {
			return out // 环闭合（#426）：返回已拷贝节点
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		visited[v.Pointer()] = out // 先注册再下探
		for i := range v.Len() {
			out.Index(i).Set(deepCopyVisited(v.Index(i), visited))
		}
		return out
	case reflect.Array:
		out := reflect.New(v.Type()).Elem()
		for i := range v.Len() {
			out.Index(i).Set(deepCopyVisited(v.Index(i), visited))
		}
		return out
	case reflect.Struct:
		// 先整块值拷贝（保住未导出字段——reflect 无法 Set 它们），再把
		// 可 Set 的引用型字段逐一替换成深拷贝；标量字段递归返回原值，
		// 等价不动。
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		for i := range out.NumField() {
			f := out.Field(i)
			if !f.CanSet() {
				continue
			}
			f.Set(deepCopyVisited(v.Field(i), visited))
		}
		return out
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		if out, ok := visited[v.Pointer()]; ok {
			return out // 环闭合（core#102）：返回已拷贝节点，结构共享
		}
		out := reflect.New(v.Type().Elem())
		visited[v.Pointer()] = out // 先注册再下探：自引用环在此终止
		out.Elem().Set(deepCopyVisited(v.Elem(), visited))
		return out
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		// 拷贝动态值,避免接口内隐藏的 map/slice 别名(concrete 值对
		// 接口字段可赋值)
		return deepCopyVisited(v.Elem(), visited)
	default:
		return v
	}
}
