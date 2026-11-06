package convert

import (
	"fmt"
	"reflect"
	"strings"
)

// ConvertStruct 将 src 结构体的同名字段映射到 dst。
// dst 必须是指向结构体的非 nil 指针。通过反射匹配字段名，相同名称且类型
// 兼容的字段会被复制。
//
//	type Src struct { Name string; Age int }
//	type Dst struct { Name string; Age int }
//	var d Dst
//	ConvertStruct(Src{Name: "test", Age: 18}, &d)
//
// 入参不满足上述契约时返回错误，而不是静默 no-op（[#207]）：调用方若拿到
// nil 就认为已填充，实际 dst 一个字都没变，是最难排查的那类 bug。被检查的
// 情形包括 dst 非指针、dst 为 nil 指针、src 为 nil（含 nil 接口值与 nil 的
// typed pointer），以及 src/dst 指向的不是结构体。
//
// 字段层面则仍是**尽力而为**：src 中不存在于 dst 的字段、类型不一致的字段、
// 不可导出的字段都会被跳过而不报错——映射是「同名字段尽可能复制」，不是严格
// 转换。
func ConvertStruct(src, dst any) error {
	if dst == nil {
		return fmt.Errorf("convert: dst must be a non-nil pointer to struct, got %T", dst)
	}

	dstVal := reflect.ValueOf(dst)
	if dstVal.Kind() != reflect.Pointer {
		return fmt.Errorf("convert: dst must be a non-nil pointer to struct, got %T", dst)
	}
	if dstVal.IsNil() {
		return fmt.Errorf("convert: dst must be a non-nil pointer to struct, got nil %T", dst)
	}

	srcVal := reflect.Indirect(reflect.ValueOf(src))
	if !srcVal.IsValid() {
		return fmt.Errorf("convert: src must be a struct or non-nil pointer to struct, got %T", src)
	}
	if srcVal.Kind() != reflect.Struct {
		return fmt.Errorf("convert: src must be a struct or non-nil pointer to struct, got %T", src)
	}

	dstVal = dstVal.Elem()
	if dstVal.Kind() != reflect.Struct {
		return fmt.Errorf("convert: dst must point to a struct, got pointer to %s", dstVal.Kind())
	}

	srcType := srcVal.Type()
	for i := 0; i < srcVal.NumField(); i++ {
		srcField := srcVal.Field(i)
		srcFieldName := srcType.Field(i).Name

		dstField := dstVal.FieldByName(srcFieldName)
		if !dstField.IsValid() || !dstField.CanSet() {
			continue
		}

		if srcField.Type() == dstField.Type() {
			dstField.Set(srcField)
		}
	}

	return nil
}

// StructToMap 将结构体转为 map[string]any。
// 通过 json tag 获取 key 名，无 json tag 则用字段名。
func StructToMap(v any) map[string]any {
	result := make(map[string]any)
	val := reflect.ValueOf(v)
	val = reflect.Indirect(val)

	if val.Kind() != reflect.Struct {
		return result
	}

	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := val.Field(i)

		if !fieldVal.CanInterface() {
			continue
		}

		key := field.Tag.Get("json")
		// 剥离 tag 选项（如 omitempty/string），只取名字部分，
		// 与 encoding/json 的键名语义一致
		if i := strings.Index(key, ","); i >= 0 {
			key = key[:i]
		}
		if key == "-" {
			// 与 encoding/json 一致：json:"-" 显式排除（敏感字段防泄漏）
			continue
		}
		if key == "" {
			key = field.Name
		}

		result[key] = fieldVal.Interface()
	}

	return result
}
