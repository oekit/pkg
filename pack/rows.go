package pack

import (
	"fmt"
	"reflect"

	"github.com/oekit/pkg/errors"
)

// rowIter 逐行产出 JSONL 行的迭代器。
type rowIter interface {
	// Next 推进到下一行；没有更多行时返回 false。
	Next() bool
	// Value 返回当前行。
	Value() any
	// Err 返回迭代过程中的错误（正常结束时为 nil）。
	Err() error
}

// newRowIter 把 rows 变成行迭代器。
//
// 支持切片与数组（元素类型任意，元素本身只要能被 json 编码即可）；
// 不接受 nil、单个值、map 与字节切片。
func newRowIter(rows any) (rowIter, error) {
	if rows == nil {
		return nil, errors.New("pack: rows 不能为 nil")
	}
	rv := reflect.ValueOf(rows)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
	default:
		return nil, fmt.Errorf("pack: rows 必须是切片或数组，收到 %T", rows)
	}
	if rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() == reflect.Uint8 {
		return nil, fmt.Errorf("pack: rows 不支持字节切片（%T），请逐行传入结构体或映射", rows)
	}
	return &reflectRowIter{rv: rv}, nil
}

// reflectRowIter 基于反射遍历切片/数组。
type reflectRowIter struct {
	rv  reflect.Value
	idx int
}

// Next 推进到下一行。
func (it *reflectRowIter) Next() bool {
	if it.idx >= it.rv.Len() {
		return false
	}
	it.idx++
	return true
}

// Value 返回当前行。
func (it *reflectRowIter) Value() any {
	return it.rv.Index(it.idx - 1).Interface()
}

// Err 返回 nil（反射遍历不会产生错误）。
func (it *reflectRowIter) Err() error {
	return nil
}
