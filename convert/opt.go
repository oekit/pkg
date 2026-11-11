package convert

import (
	"encoding/json"
	"math"
	"strconv"
)

// ===== nil 安全的可选转换（#143）=====
//
// Any* 一族把「转换不了」一律变成零值。AnyToStr 的 nil 语义已在 #295 收敛
// 为空串（"<nil>" 是 Go 内部表示的泄漏：过得了 schema 校验、会被当成真实
// 内容送到模型/答复/留痕面前）。
//
// 本文件补的是另外两档：**给 nil 一个显式缺省**（AnyToStrOr）与**明确地说
// 没有**（Opt* 返回 nil 指针——「没有这个字段」与「字段的值是零值」在下游
// 必须分得开）。

// AnyToStrOr 返回 v 的字符串形态；v 为 nil 时返回 def。
//
// 与 AnyToStr 的关系：AnyToStr(nil) 恒等于 AnyToStrOr(nil, "")——调用方
// 要给 nil 一个非空缺省（如 "-"、"未知"）时用本函数；要区分「没有值」与
// 「值为零值」用 OptStr。
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
//
// 数值判定与 AnyToBool 共用同一口径（非零为 true，经 float64 比较，
// [#255]）：OptBool(0.5) 与 AnyToBool(0.5) 都是 true，不会一边 true
// 一边 &false。
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

// anyToInt64OK 是 any→int64 的**唯一实现**（#371 收敛单源）：AnyToInt64
// 是丢弃 ok 位的适配层，OptInt64 / OptInt32 / AnyToInt32 直接消费 ok 位——
// 「转换不了」与「转换成了 0」在此分得开，Opt* 一族靠它不把零值伪装成
// 「没有这个字段」。
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
		// 超出 int64 表示范围属于"无法转换"，按契约报 false（而非回绕为负数）
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
		// encoding.Unmarshal 使用 UseNumber，数值是 json.Number；
		// 不处理会导致所有 JSON 数值静默归零
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

// anyToBoolOK 是 any→bool 的**唯一实现**（#371 收敛单源）：AnyToBool 是
// 丢弃 ok 位的适配层，OptBool 消费 ok 位。
//
// 数值与 json.Number 分支**不得**经整数中转（[#255]，#205 的同源漏网）：
// AnyToInt64 会把 |v|<1 的浮点截断成 0，于是 OptBool(0.5) 判成 &false——
// 与 AnyToBool(0.5)=true 对同一值给出相反结论，而且后果更重：nil/非 nil
// 还叠加了「字段存不存在」的第二层语义。判定与 AnyToBool 共用同一实现
// （AnyToFloat64 != 0），两个入口对任何值都不可能再分叉。
func anyToBoolOK(v any) (bool, bool) {
	switch val := v.(type) {
	case nil:
		return false, false
	case bool:
		return val, true
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return AnyToFloat64(val) != 0, true
	case string:
		return strToBoolOK(val)
	case []byte:
		return strToBoolOK(string(val))
	case json.Number:
		return AnyToFloat64(val) != 0, true
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
