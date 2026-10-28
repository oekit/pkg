package convert

import (
	"encoding/json"
	"math"
	"strconv"
)

// ===== nil 安全的可选转换（#143）=====
//
// Any* 一族把「转换不了」一律变成零值，而 AnyToStr(nil) 更是返回字符串
// "<nil>"——它不是空串、不是零值、不是 error，而是一个**长得像值**的哨兵：
//
//   - 过得了任何 schema 校验（就是个 string）；
//   - 会被当成「这个字段有内容，只是内容是 <nil>」送到模型面前；
//   - 模型于是拿到一个**不存在的名字**当作真实值。
//
// 「没有这个字段」与「这个字段的值是 <nil> 字符串」在下游必须分得开，但用
// AnyToStr 之后分不开。本文件补的是**第三种形态：明确地说没有**。

// AnyToStrOr 返回 v 的字符串形态；v 为 nil 时返回 def。
//
// 默认 def 为 "" ⇒ nil 读成空串，而不是 "<nil>"。
//
// 与 AnyToStr 的关系：AnyToStr 保持既有语义（不动既有调用点），AnyToStrOr
// 是 nil 安全的那一档。
func AnyToStrOr(v any, def string) string {
	if v == nil {
		return def
	}
	return AnyToStr(v)
}

// OptStr 返回 v 的字符串形态指针；v 为 nil 时返回 nil。
//
// nil 的含义是「**没有这个字段**」——不是「字段的值是空串」。
func OptStr(v any) *string {
	if v == nil {
		return nil
	}
	s := AnyToStr(v)
	return &s
}

// OptBool 返回 v 的 bool 形态指针。
//
// v 为 nil、或无法表示为 bool（如 "abc"）时返回 nil——**不**返回 &false：
// 「有一个值为 false 的字段」与「没有这个字段」不是一回事，把前者伪装成
// 后者就是把不可用值当成了可用值。
func OptBool(v any) *bool {
	b, ok := anyToBoolOK(v)
	if !ok {
		return nil
	}
	return &b
}

// OptInt64 返回 v 的 int64 形态指针。
//
// v 为 nil、或无法表示为 int64 时返回 nil。
func OptInt64(v any) *int64 {
	i, ok := anyToInt64OK(v)
	if !ok {
		return nil
	}
	return &i
}

// OptInt32 返回 v 的 int32 形态指针。
//
// v 为 nil、或无法表示为 int32（超范围、不可解析）时返回 nil——超范围的值
// 装不进 int32 契约，就**没有** int32 值；返回 &0 则是把一个垃圾值伪装成
// 一个合法值。
func OptInt32(v any) *int32 {
	i, ok := anyToInt64OK(v)
	if !ok || i < math.MinInt32 || i > math.MaxInt32 {
		return nil
	}
	n := int32(i)
	return &n
}

// AnyToInt32 任意类型转 int32，无法转换或超出 int32 表示范围返回 0。
//
// 补这一档的原因：有 AnyToInt / AnyToInt64 / AnyToUint64 / AnyToFloat32 /
// AnyToFloat64，唯独没有 int32——而 JSON 解码出来的数字一律是 float64，
// 进 int32 契约前必须转。
//
// 口径与 AnyToInt64 同族：装不下按「无法转换」处理（返回 0），**不发生
// 回绕**（回绕会把一个越界值变成符号相反的合法值）。
func AnyToInt32(v any) int32 {
	i, ok := anyToInt64OK(v)
	if !ok || i < math.MinInt32 || i > math.MaxInt32 {
		return 0
	}
	return int32(i)
}

// anyToInt64OK 与 AnyToInt64 同口径，但**额外报告能否转换**——
// Opt* 一族需要区分「转换不了」与「转换成了 0」。
func anyToInt64OK(v any) (int64, bool) {
	switch val := v.(type) {
	case nil:
		return 0, false
	case bool:
		if val {
			return 1, true
		}
		return 0, true
	case int:
		return int64(val), true
	case int8:
		return int64(val), true
	case int16:
		return int64(val), true
	case int32:
		return int64(val), true
	case int64:
		return val, true
	case uint:
		if uint64(val) > math.MaxInt64 {
			return 0, false
		}
		return int64(val), true
	case uint8:
		return int64(val), true
	case uint16:
		return int64(val), true
	case uint32:
		return int64(val), true
	case uint64:
		if val > math.MaxInt64 {
			return 0, false
		}
		return int64(val), true
	case float32:
		return float64ToInt64OK(float64(val))
	case float64:
		return float64ToInt64OK(val)
	case string:
		return strToInt64OK(val)
	case []byte:
		return strToInt64OK(string(val))
	case json.Number:
		if iv, err := val.Int64(); err == nil {
			return iv, true
		}
		fv, err := val.Float64()
		if err != nil {
			return 0, false
		}
		return float64ToInt64OK(fv)
	}
	return 0, false
}

// anyToBoolOK 与 AnyToBool 同口径，但**额外报告能否转换**。
func anyToBoolOK(v any) (bool, bool) {
	switch val := v.(type) {
	case nil:
		return false, false
	case bool:
		return val, true
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return AnyToInt64(val) != 0, true
	case string:
		return strToBoolOK(val)
	case []byte:
		return strToBoolOK(string(val))
	case json.Number:
		return AnyToInt64(val) != 0, true
	}
	return false, false
}

// float64ToInt64OK 浮点转 int64：NaN/±Inf 与超出 int64 表示范围的值报
// 「无法转换」（直接 int64(f) 属于 implementation-defined）。
func float64ToInt64OK(f float64) (int64, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f < math.MinInt64 || f >= 9223372036854775808 {
		return 0, false
	}
	return int64(f), true
}

// strToInt64OK 字符串按十进制解析，解析失败报「无法转换」。
func strToInt64OK(s string) (int64, bool) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// strToBoolOK 字符串按 strconv.ParseBool 解析，解析失败报「无法转换」。
func strToBoolOK(s string) (bool, bool) {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return false, false
	}
	return v, true
}
