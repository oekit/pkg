package pii

// 处置执行面：策略感知的打码与载荷下探。
//
// 与 policy.go 的分工：那边是**注册表**（登记哪些敏感面、面开不开、处置强度
// 枚举是什么），这边是**执行面**（拿到策略后怎么处置一段文本/一棵载荷）。
// 拆分的直接原因：policy.go 的文件头写的是「处置策略面（面开关 + 策略枚举）」，
// 而执行引擎原本也在那个文件里，文档只描述了它一半的内容。

// （「为什么原包不够」与三条不变量在 policy.go 的文件头，此处不重复。）

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// redactedPlaceholder redact 策略的固定占位。
const redactedPlaceholder = "[redacted]"

// applySpan 按**给定**策略处置单个 span 的原值。
//
// 策略与摘要密钥都由调用方从同一份 Policy 快照传入而不是就地读全局：一次处置
// 是一个不可分割的动作，让处置形态依赖调用时刻而返回的阻断信号依赖返回时刻，
// 两者可能对不上（见 applySpans）。
func applySpan(v string, kind Kind, s StrategyKind, digestKey string) string {
	switch s {
	case StrategyRedact:
		return redactedPlaceholder
	case StrategyHash:
		// 缺密钥 fail-closed：不出摘要。无盐无密钥的摘要只是原值的可离线穷举
		// 等价物（手机号域 9×10⁹，字典枚举即还原），那比 mask 更危险——它
		// 「看起来什么都不留」。缺密钥时整段替换 + applySpansAs 回报阻断，
		// 让接线错误第一次处置就显形（取舍记录在 SetDigestKey）。
		if digestKey == "" {
			return redactedPlaceholder
		}
		return keyedDigest(normalizeDigits(v), kind, digestKey)
	default:
		return maskSpan(v, kind) // mask 与 block 都走保长打码；block 的处置在 Apply 层
	}
}

// keyedDigest 域分离 + HMAC-SHA256 + 截断 16 位 hex。
//
// 域分离（kind 作前段 + NUL 定界）让不同 Kind 的同形值永不撞摘要；NUL 定界
// 消除拼接歧义（kind 来自固定枚举，v 任意——定界后 (kind,v) 与
// (kind',v') 的编码不可能相同）。密钥保密 ⇒ 字典枚举失效：攻击者没有密钥
// 就无法对候选值复算摘要，9×10⁹ 的手机号域不再可穷举。
func keyedDigest(v string, kind Kind, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(kind))
	mac.Write([]byte{0})
	mac.Write([]byte(v))
	return hex.EncodeToString(mac.Sum(nil))[:16]
}

// applySpansAs 以**指定**策略与摘要密钥处置，是「一次调用一个策略」这一不变量的
// 可测形式：测试可固定策略断言「处置形态与 blocked 互相自洽」，不必与全局开关
// 赛跑出竞态窗口。
//
// 策略与密钥由 Policy 快照带进来，而不是就地读全局——见 applySpans 下方注释。
func applySpansAs(text string, s StrategyKind, digestKey string) (string, bool) {
	spans := Detect(text)
	if len(spans) == 0 {
		return text, false
	}
	var b strings.Builder
	b.Grow(len(text))
	prev := 0
	for _, sp := range spans {
		b.WriteString(text[prev:sp.Start])
		b.WriteString(applySpan(text[sp.Start:sp.End], sp.Kind, s, digestKey))
		prev = sp.End
	}
	b.WriteString(text[prev:])
	// hash 缺密钥与 block 同档：要求阻断。摘要没出去，宿主必须看见「这次处置
	// 没有按配置的形态完成」。
	return b.String(), s == StrategyBlock || (s == StrategyHash && digestKey == "")
}

// Apply 对指定面处置一段文本。
//
// 返回 (处置后文本, 是否阻断)：
//   - 面未启用 / 全局关闭 → (原文, false)
//   - 检出且策略为 block → (处置后文本, true)，**调用方必须履行阻断**
//   - 其余 → (处置后文本, false)
func Apply(s Surface, text string) (string, bool) {
	return applyWith(CurrentPolicy(), s, text)
}

// applyWith 以**指定策略快照**处置一段文本。
//
// # 为什么先取快照再逐项判
//
// enabled / 面开关 / 策略是同一份 Policy 的三个字段，分三次调用访问器去读，
// 中间会被热重载插进来——读到「旧面开关 + 新策略」这种从未被设置过的组合，
// 表现为「宿主已关掉某个面，本次调用却仍回报 blocked=true」。取一次快照，
// 三项判断与处置形态必然同源。
func applyWith(p Policy, s Surface, text string) (string, bool) {
	if !p.enabled {
		return text, false
	}
	// 未知面名**不静默放行**：面未登记与「宿主显式关闭」是两件事，塌缩成同一个
	// 状态就等于拼错一个字符即可让该面完全不脱敏——而 tool_results 正是 PII
	// 最需要兜住的面（工具产出直接回喂模型）。无信号才是这里真正危险的地方：
	// 宿主会以为自己在保护，其实什么都没做。
	//
	// 取舍：整段替换 + 要求阻断，而不是 panic。panic 更响，但会顺着调用栈杀掉
	// 正在跑的编排；静默放行最省事，代价是数据裸奔。这里取两者之间可恢复的那个：
	// 结果仍然是「原文没有出去」，同时让宿主在返回值里看见异常。
	if _, err := SurfaceBit(s); err != nil {
		return redactedPlaceholder, true
	}
	if !p.surfaces.Has(s) {
		return text, false // 确属显式关闭：既定语义，原文返回
	}
	return applySpansAs(text, p.strategy, p.digestKey)
}

// ApplyValue 递归下探常见载荷结构处置（结构面覆盖与原面无关门面一致，
// 另加上面/策略维度）。返回 (处置后载荷, 是否阻断)。
func ApplyValue(s Surface, v any) (any, bool) {
	return applyValueWith(CurrentPolicy(), s, v)
}

// applyValueWith 以**指定策略快照**下探载荷。
//
// # 为什么递归闭包要捕获快照
//
// 本函数此前把闭包交给 applyValue，每个字符串叶子都回调 `Apply(s, t)`——
// 于是**每个叶子重走一遍全部三份全局状态**。一次遍历跨越一次热重载，载荷内
// 就会同时出现两种处置形态（前半占位、后半打码），而 blocked 由 OR 累加得出，
// 与文本形态不同源。这正是 applySpansAs 当初要消除的现象，只不过那条保证止步
// 于单文本路径，复合载荷从旁边绕了过去——而 ApplyValue 才是宿主处理工具返回
// 的主入口。policy_atomic_test.go 的实测：旧实现第 5 次迭代即出现 61/64 混合。
func applyValueWith(p Policy, s Surface, v any) (any, bool) {
	if !p.enabled {
		return v, false
	}
	// 未登记面名**不在这里短路**：短路会返回裸字符串，把本函数「保持载荷结构」
	// 的契约砸掉（宿主按原结构解回，拿到的却是 string）。
	// 交给下面的递归即可——每个字符串叶子都过 applyWith，
	// 那里对未登记面已经 fail-closed，结构与阻断信号都自然保住。
	if !p.surfaces.Has(s) {
		if _, err := SurfaceBit(s); err == nil {
			// 快路径，不是语义守卫：面关闭时逐叶走 applyWith 本来也会透传，
			// 且 applyValue 保留 nil 容器，两者行为完全等价。
			// 这里提前返回只是为了省掉一次载荷遍历。
			// （实测把本分支拆掉，`surface_disabled_test.go` 的用例仍全绿——
			//  这行因此无法被行为断言覆盖，别再往这里加"语义"注释。）
			return v, false
		}
	}
	return applyValue(v,
		func(t string) (string, bool) { return applyWith(p, s, t) },
		func(k string) (string, bool) { return applyKeyWith(p, s, k) })
}

// applyKeyWith 键位处置：与值走**同一条判定面**（同一份快照、同一个 Detect、
// 同一套策略形态——键位命中同样产出 span 级处置与 blocked 信号）。
//
// # 为什么键不继承「未登记面名整段替换」这一档
//
// 值的 fail-closed 把整个字符串换成占位，是因为值本身就是被保护的对象；键还兼着
// **结构导航**的职责——整段替换会把普通键一并砸掉，宿主连「哪些字段出了问题」
// 都定位不到。键退到 span 级判定：PII 键按当前策略处置（不给明文留位），普通键
// 逐字保留，阻断信号照发。面被显式关闭时键与值一样透传（既定语义），走
// applyWith 的既有分支自然保住。
func applyKeyWith(p Policy, s Surface, k string) (string, bool) {
	if _, err := SurfaceBit(s); err != nil {
		return applySpansAs(k, p.strategy, p.digestKey)
	}
	return applyWith(p, s, k)
}

// applyValue 递归下探：字符串交给 fn 处置，映射键交给 fk 处置（键与值同一条
// 判定面，见 applyKeyWith），标量原样返回。
//
// # 为什么 nil 容器必须原样返回
//
// JSON 载荷里 null 与空容器是两件事（「没有」与「有一个但是空的」），审计链的下游
// 按这个形状做判断。make(...) 出来的非 nil 空容器会把 null 悄悄改写成 [] / {}，
// 且改写不可逆——事后没人能分辨该字段本来是 null 还是空。四个容器分支因此各自先判
// nil 并**返回原值**（连动态类型一起保留，调用方的类型断言仍成立）。
func applyValue(v any, fn, fk func(string) (string, bool)) (any, bool) {
	switch t := v.(type) {
	case string:
		return fn(t)
	case map[string]any:
		if t == nil {
			//nolint:gocritic // nilValReturn 不适用：此处必须返回**带类型的 nil**
			// （调用方依赖 map/slice 类型信息做断言，无类型 nil 会让断言失效——
			// TestApplyValueKeepsNilContainers 钉着这个语义）。
			return t, false
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		forms, blocked := disposeMapKeys(keys, fk)
		out := make(map[string]any, len(t))
		for i, k := range keys {
			ov, ob := applyValue(t[k], fn, fk)
			out[forms[i].final], blocked = ov, blocked || ob
		}
		return out, blocked
	case map[string]string:
		if t == nil {
			//nolint:gocritic // nilValReturn 不适用：此处必须返回**带类型的 nil**
			// （调用方依赖 map/slice 类型信息做断言，无类型 nil 会让断言失效——
			// TestApplyValueKeepsNilContainers 钉着这个语义）。
			return t, false
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		forms, blocked := disposeMapKeys(keys, fk)
		out := make(map[string]string, len(t))
		for i, k := range keys {
			sv, sb := fn(t[k])
			out[forms[i].final], blocked = sv, blocked || sb
		}
		return out, blocked
	case []any:
		if t == nil {
			//nolint:gocritic // nilValReturn 不适用：此处必须返回**带类型的 nil**
			// （调用方依赖 map/slice 类型信息做断言，无类型 nil 会让断言失效——
			// TestApplyValueKeepsNilContainers 钉着这个语义）。
			return t, false
		}
		out := make([]any, len(t))
		blocked := false
		for i, vv := range t {
			ov, ob := applyValue(vv, fn, fk)
			out[i], blocked = ov, blocked || ob
		}
		return out, blocked
	case []string:
		if t == nil {
			//nolint:gocritic // nilValReturn 不适用：此处必须返回**带类型的 nil**
			// （调用方依赖 map/slice 类型信息做断言，无类型 nil 会让断言失效——
			// TestApplyValueKeepsNilContainers 钉着这个语义）。
			return t, false
		}
		out := make([]string, len(t))
		blocked := false
		for i, vv := range t {
			sv, sb := fn(vv)
			out[i], blocked = sv, blocked || sb
		}
		return out, blocked
	default:
		// 未登记形状**不静默放行**（与「未登记面名」同档 fail-closed）：宿主没有
		// 手段区分「没命中敏感值」与「根本没看」，而这里的载荷恰恰常被解码成
		// []map[string]string / map[string][]string / []byte（json.RawMessage）——
		// 静默透传等于整段 PII 外发还报平安。整段替换 + 要求阻断，宿主在返回值里
		// 看见异常（取舍同未登记面名：不 panic，可恢复，但绝不「假装检查过」）。
		if shapeScalar(v) {
			return v, false
		}
		return redactedPlaceholder, true
	}
}

// shapeScalar 未登记形状里按「标量/空」放行的判据（ValidateShape 与运行期
// default 分支共用同一条，两处口径永不漂移）。
//
// 标量放行是**文档化的覆盖盲区**，不是疏漏：检出面只认 string，数字/布尔不是
// 本包的判定对象（USAGE.md「不下探标量」）；字符串命名类型（json.Number、
// 自定义 ~string）同理——type switch 只认 exact string，扩展它们属于支持面
// 扩张，须与「防止支持面扩大成误检面」的纪律一起另行决策。
//
// nil 容器（含未登记类型的 nil）原样保留：与 JSON null 同义，无可处置内容，
// 同四容器的 nil 纪律。
func shapeScalar(v any) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Invalid, // 无类型 nil
		reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128,
		reflect.String: // 含 json.Number 等字符串命名类型（文档化盲区）
		return true
	case reflect.Slice, reflect.Map, reflect.Pointer, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return rv.IsNil()
	}
	return false // Array / Struct / 其余复合：未登记，fail-closed
}

// keyForm 单个映射键的处置结果。
type keyForm struct {
	orig    string // 原键
	final   string // 处置后的键
	blocked bool   // 该键判定的阻断信号
}

// disposeMapKeys 把一批键送进与值**同一条判定面**（fk），并做与迭代顺序无关的
// 冲突消解。
//
// # 为什么需要冲突消解
//
// 键位参与处置后，不同原键可能处置出同一形态：redact 下所有 PII 键都变占位
// （必然同形）；mask 抹掉中段，同前缀同尾号也可能同形。直接写回会**静默合并
// 丢条目**——一次数据完整性事故，比漏打更难被发现。消解规则：同形组内按原键
// 字典序编 "#n"。编号只依赖键集合本身，Go map 的随机迭代序进不了结果——
// 同一载荷重复处置，键形态恒同（可关联性），条目数恒不变。
//
// blocked 为全部键判定信号的 OR（bool OR 与顺序无关）。
func disposeMapKeys(origs []string, fk func(string) (string, bool)) ([]keyForm, bool) {
	forms := make([]keyForm, len(origs))
	blocked := false
	for i, k := range origs {
		f, b := fk(k)
		forms[i] = keyForm{orig: k, final: f, blocked: b}
		blocked = blocked || b
	}
	byForm := make(map[string][]int, len(origs))
	for i := range forms {
		byForm[forms[i].final] = append(byForm[forms[i].final], i)
	}
	for _, idxs := range byForm {
		if len(idxs) < 2 {
			continue // 同形必同原键（map 键唯一），无冲突
		}
		group := make([]string, 0, len(idxs))
		for _, i := range idxs {
			group = append(group, forms[i].orig)
		}
		slices.Sort(group)
		rank := make(map[string]int, len(group))
		for j, o := range group {
			rank[o] = j + 1
		}
		for _, i := range idxs {
			forms[i].final += "#" + strconv.Itoa(rank[forms[i].orig])
		}
	}
	return forms, blocked
}
