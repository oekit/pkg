package cron

import (
	"fmt"
	"math"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	starBit = 1 << 63
)

// SpecSchedule 根据 cron 表达式调度任务。
// Location 为 nil 时使用 time.Local；零值没有可匹配的月份，Next 返回零值时间。
type SpecSchedule struct {
	Second, Minute, Hour, Dom, Month, Dow uint64
	Location                              *time.Location
}

// ConstantDelaySchedule 以固定时间间隔调度。
type ConstantDelaySchedule struct {
	Delay time.Duration
}

// Schedule 调度接口。
type Schedule interface {
	Next(time.Time) time.Time
}

// Parse 解析 cron 表达式并返回 Schedule。
// 支持标准 5 字段格式：分 时 日 月 周
// 也支持 6 字段格式：秒 分 时 日 月 周
func Parse(spec string) (Schedule, error) {
	if spec == "@every" {
		return nil, fmt.Errorf("missing duration after @every")
	}

	if strings.HasPrefix(spec, "@every ") {
		d, err := time.ParseDuration(spec[7:])
		if err != nil {
			return nil, fmt.Errorf("failed to parse duration %s: %w", spec[7:], err)
		}
		return Every(d), nil
	}

	switch spec {
	case "@yearly", "@annually":
		spec = "0 0 0 1 1 *"
	case "@monthly":
		spec = "0 0 0 1 * *"
	case "@weekly":
		spec = "0 0 0 * * 0"
	case "@daily", "@midnight":
		spec = "0 0 0 * * *"
	case "@hourly":
		spec = "0 0 * * * *"
	}

	fields := strings.Fields(spec)
	if len(fields) == 5 {
		// 5 字段格式，添加秒字段
		fields = append([]string{"0"}, fields...)
	}

	if len(fields) != 6 {
		return nil, fmt.Errorf("expected 5 or 6 fields, got %d: %s", len(fields), spec)
	}

	schedule := &SpecSchedule{
		Location: time.Local,
	}

	var err error
	schedule.Second, err = getField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("invalid second field: %w", err)
	}
	schedule.Minute, err = getField(fields[1], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("invalid minute field: %w", err)
	}
	schedule.Hour, err = getField(fields[2], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("invalid hour field: %w", err)
	}
	schedule.Dom, err = getField(fields[3], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("invalid day of month field: %w", err)
	}
	schedule.Month, err = getField(fields[4], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("invalid month field: %w", err)
	}
	// dow 接受 7≡Sunday（[#186] / [#215]）：vixie cron / cronie / crontab(5) /
	// robfig 都接受 7，且**在列表与范围的端点同样接受**（`6,7`、`5-7`）——
	// #186 只按整串判等归一的写法覆盖不到复合形态，从系统 crontab 迁移
	// `0 2 * * 5-7` 会加载失败，而报错文案却写着「7≡Sunday 已支持」，排错
	// 方向被反向误导。方言差异（MON/JAN 名字、L/W/# 均不支持）见 doc.go。
	schedule.Dow, err = getDowField(fields[5])
	if err != nil {
		return nil, fmt.Errorf("invalid day of week field %q (0/7 都是 Sunday; MON/JAN 等名字与 L/W/# 扩展不支持): %w", fields[5], err)
	}

	return schedule, nil
}

// Every 返回一个以固定间隔调度的 Schedule。
//
// ⚠️ 语义三件套（[#184](https://github.com/oekit/pkg/issues/184)，此前零声明）：
//   - duration < 1s **静默钳到 1s**（本调度器秒级粒度，ConstantDelaySchedule
//     沿用 robfig 形态）——配 Every(500ms) 期望半秒节奏会得到 1s，调度密度
//     减半无任何信号；
//   - 亚秒部分**截断**（1500ms ⇒ 1s）；
//   - Next 按传入时间的秒对齐，实际触发间隔 = delay 但不保证与「上次执行
//     完成」挂钩（固定时刻表，非完成后间隔；后者用 oekit/concurrency/loop
//     的 IntervalAfterCompletion）。
//
// 需要亚秒级高频节奏不该用 cron——直接 go-timer/loop。
func Every(duration time.Duration) ConstantDelaySchedule {
	if duration < time.Second {
		duration = time.Second
	}
	return ConstantDelaySchedule{Delay: duration - time.Duration(duration.Nanoseconds())%time.Second}
}

// Next 返回下一个调度时间。
func (s ConstantDelaySchedule) Next(t time.Time) time.Time {
	return t.Add(s.Delay - time.Duration(t.Nanosecond())*time.Nanosecond)
}

// Next 返回下一个调度时间。
//
// ⚠️ 返回值**保证严格晚于入参**（除非返回零值 time.Time 表示放弃）。调度器的
// 标准消费形态是 `for { next := sched.Next(prev); time.Sleep(next.Sub(now)); prev = next }`，
// 一旦 Next 返回不晚于入参的时刻，该循环就变成**原地热转**且永不前进——任务在
// 一个窗口里被疯狂重复执行，进程忙等（[#214] 的失效形态）。内部各循环已全部
// 改成绝对时间推进（见各循环注释），这里再留一道兜底：真出现不前进时从入参
// 往后挪一秒重算，最多 3 次。
func (s *SpecSchedule) Next(t time.Time) time.Time {
	next := s.next(t)
	for i := 0; i < 3 && !next.IsZero() && !next.After(t); i++ {
		t = t.Add(time.Second)
		next = s.next(t)
	}
	return next
}

func (s *SpecSchedule) next(t time.Time) time.Time {
	location := s.Location
	if location == nil {
		location = time.Local
	}
	t = t.In(location)

	// 移动到下一秒
	t = t.Add(1*time.Second - time.Duration(t.Nanosecond())*time.Nanosecond)

	// ⚠️ 不可满足表达式（如 `0 0 31 2 *`：2 月永无 31 日）在此静默返回零值
	// time.Time（[#185]）：无 error 通道、5 年硬搜索上限后放弃。调用方拿到
	// 零值后 time.Until(zero) 是极大负值——据此 sleep 会立即触发风暴，据此
	// 判「已过期」语义相反。调度器消费 Next 前应判 IsZero。
	yearLimit := t.Year() + 5

WRAP:
	if t.Year() > yearLimit {
		return time.Time{}
	}

	// 检查月份
	for 1<<uint(t.Month())&s.Month == 0 {
		// 按整月推进到「目标月 1 日的最早存在的时刻」。不写成
		// 「归一到当月 1 号再 AddDate(0,1,0)」的原因有两条：
		//  1. AddDate(0,1,0) 在「当前日 > 目标月天数」时会溢出到再下个月
		//     （1/31 + 1 月 → 3/3），随后的复位就锁死在溢出后的月份 ⇒ 目标月
		//     被跳过（"0 0 0 1 2 *" 从 1/31 求值，下次触发晚一年）；
		//  2. 月初的凌晨可能**不存在**（DST 前跳跨过午夜），而 time.Date 会把
		//     不存在的墙钟归一回去 ⇒ 原地打转（见 firstExistingOnDay）。
		t = nextMonthStart(t)
		if t.Month() == time.January {
			goto WRAP
		}
	}

	// 检查日期
	for !s.dayMatches(t) {
		t = nextDayStart(t)
		if t.Day() == 1 {
			goto WRAP
		}
	}

	// 检查小时
	//
	// 用**绝对时间**推进到下一个墙钟整点，不再「Add(1h) 后按墙钟重建」：
	// DST 回拨时同一墙钟小时出现两次，而 time.Date 对歧义墙钟一律解析到
	// **较早**那次 ⇒ 重建会让绝对时间倒退（实测 America/New_York 2026-11-01：
	// 01:59 EDT +1min 走到 01:00 EST，重建却回到 01:00 EDT，倒退 59 分钟）。
	// 倒退后 Minute()==0 触发 WRAP，hour/minute/second 全部命中 ⇒ Next 返回
	// **早于入参**的时刻，`for { next := Next(prev) }` 的调度器立即热循环且永不
	// 前进（[#214]）。绝对推进天然单调，且到处「墙钟分秒归零」（现代时区偏移
	// 都是整分钟 ⇒ 绝对整点必然落在墙钟整分上）。
	for 1<<uint(t.Hour())&s.Hour == 0 {
		t = nextHourStart(t)
		if t.Hour() == 0 {
			goto WRAP
		}
	}

	// 检查分钟（同理，绝对推进）
	for 1<<uint(t.Minute())&s.Minute == 0 {
		t = nextMinuteStart(t)
		if t.Minute() == 0 {
			goto WRAP
		}
	}

	// 检查秒
	for 1<<uint(t.Second())&s.Second == 0 {
		t = t.Add(1 * time.Second)
		if t.Second() == 0 {
			goto WRAP
		}
	}

	return t
}

// nextHourStart 返回严格晚于 t 的下一个**墙钟整点**。
//
// 只用绝对时间加法，不按墙钟重建：现代时区偏移都是整分钟 ⇒ 绝对时间每前进
// 60 分钟，墙钟分秒必然归零；而「按墙钟重建」在 DST 回拨窗口会倒退（见 Next
// 小时循环的说明）。
func nextHourStart(t time.Time) time.Time {
	return t.Add(time.Hour - time.Duration(t.Minute())*time.Minute - time.Duration(t.Second())*time.Second)
}

// nextMinuteStart 返回严格晚于 t 的下一个**墙钟整分**（理由同 nextHourStart）。
func nextMinuteStart(t time.Time) time.Time {
	return t.Add(time.Minute - time.Duration(t.Second())*time.Second)
}

// nextDayStart 返回严格晚于 t 的下一个**本地日起点**（该日最早存在的时刻）。
func nextDayStart(t time.Time) time.Time {
	// 用**正午**做加法的锚点：任何真实时区都不在正午跳变，故 time.Date 对
	// 「d+1 日正午」不会落入歧义/不存在的墙钟；日/月溢出由 time.Date 归一。
	y, m, d := t.Date()
	n := time.Date(y, m, d+1, 12, 0, 0, 0, t.Location())
	ny, nm, nd := n.Date()
	return firstExistingOnDay(ny, nm, nd, t.Location())
}

// nextMonthStart 返回严格晚于 t 的**下一个月 1 日**的最早存在的时刻。
func nextMonthStart(t time.Time) time.Time {
	y, m, _ := t.Date()
	n := time.Date(y, m+1, 1, 12, 0, 0, 0, t.Location())
	ny, nm, _ := n.Date()
	return firstExistingOnDay(ny, nm, 1, t.Location())
}

// firstExistingOnDay 返回 loc 时区 y-m-d 这天**最早存在的时刻**（正常即 00:00）。
//
// 存在两种异常，都是 Go time.Date 的行为而不是猜测：
//   - 该日 00:00 **不存在**（DST 前跳跨过午夜，如 America/Santiago 2026-09-06
//     的 00:00 直接跳到 01:00）：time.Date 把它归一到**前一天 23:00**（实测），
//     于是「归一到当天 0 点」的写法会把日期留在前一天 ⇒ 日期循环原地打转、
//     Next 直接死循环。
//   - 归一到别的日子：从归一结果按分钟推进，落到目标日即止。上界 24h 远大于
//     任何真实跳变（历史最大值约 3h）。
func firstExistingOnDay(y int, m time.Month, d int, loc *time.Location) time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, loc)
	if t.Year() == y && t.Month() == m && t.Day() == d {
		return t
	}
	for i := 1; i <= 24*60; i++ {
		c := t.Add(time.Duration(i) * time.Minute)
		if c.Year() == y && c.Month() == m && c.Day() == d {
			return c
		}
	}
	return t
}

func (s *SpecSchedule) dayMatches(t time.Time) bool {
	var (
		domMatch = 1<<uint(t.Day())&s.Dom != 0
		dowMatch = 1<<uint(t.Weekday())&s.Dow != 0
	)

	if s.Dom&starBit != 0 || s.Dow&starBit != 0 {
		return domMatch && dowMatch
	}
	return domMatch || dowMatch
}

// maxDow 是 day-of-week 字段**输入**的上界。域是 0–6（0 = Sunday），
// 而 7 是 Sunday 的别名——vixie cron / cronie / robfig 都这样接受，系统
// crontab 里 `5-7`（周五到周日）、`6,7` 是常见写法。
const maxDow = 7

// dowValue 把输入值映射到实际位：7 是 0 的别名。
func dowValue(v uint64) uint64 {
	if v == maxDow {
		return 0
	}
	return v
}

// getDowField 解析 day-of-week 字段，7≡Sunday 在**所有形态**下都成立：
// 单值（`7`）、列表（`6,7`）、范围（`5-7`）、带步长（`5-7/2`、`0-7/2`）。
// 全部走通用 parseRange + dowValue 归一钩子（#375 收敛单源——dow 的范围/
// 步进循环此前在 parseDowPart 手写一份，与通用 parseRange 平行）。
//
// 注意**不能**把 token 里的 "7" 文本替换成 "0"：范围 `5-7` 归一后是**环形**
// 区间（5,6,0），而 `start > end` 的检查会把 `5-0` 判成非法区间——这正是
// 「只在 getField 层做数值归一」的坑。故范围按**端点原值**校验/遍历、
// 落位时才经 dowValue 折回（钩子语义见 parseRange）。
func getDowField(field string) (uint64, error) {
	var bits uint64
	for _, part := range strings.Split(field, ",") {
		if err := parseRange(part, 0, maxDow, &bits, dowValue); err != nil {
			return 0, err
		}
	}
	return bits, nil
}

// splitStep 把 "a/b" 切成 ("a", b)；不含步长时 step = 1。步长切分的单源
// （#375）：parseRange 的全部形态（a-b/n、N/step、*/n）都经此剥步长，
// "step cannot be zero" 的校验只剩这一处。
func splitStep(s string) (body string, step uint64, err error) {
	body, stepStr, ok := strings.Cut(s, "/")
	if !ok {
		return s, 1, nil
	}
	step, err = parseUint(stepStr)
	if err != nil {
		return "", 0, err
	}
	if step == 0 {
		return "", 0, fmt.Errorf("step cannot be zero")
	}
	return body, step, nil
}

func getField(field string, min, max uint64) (uint64, error) {
	var bits uint64

	ranges := strings.Split(field, ",")
	for _, r := range ranges {
		if err := parseRange(r, min, max, &bits, nil); err != nil {
			return 0, err
		}
	}
	return bits, nil
}

// parseRange 解析 cron 字段的一个逗号分段，把命中的值置位到 bits——全部
// 字段共用的范围/步长解析单源（#375）。
//
// normalize 是「落位前值归一」钩子：dow 场景传 dowValue 保留「7≡0 环形
// 别名」语义——输入域仍按 [min, max] 校验（dow 传 0..maxDow=7，单值/列表/
// 范围的端点都可写 7），落位与步进统一经 normalize 折回真实位；其余字段
// 传 nil（恒等）。
//
// 支持形态：`*`、`*/n`、`a-b`、`a-b/n`、`N`、`N/step`（单值步长从 N 步进到
// 字段最大值，robfig/cron 语义）。
func parseRange(s string, min, max uint64, bits *uint64, normalize func(uint64) uint64) error {
	norm := func(v uint64) uint64 {
		if normalize != nil {
			return normalize(v)
		}
		return v
	}
	// 域含别名（normalize 非 nil）时，单值步进的终点取别名前的最后一个值：
	// dow 的 7 与 0 是同一个 Sunday，把 7 也步进一遍等于重复遍历一圈。
	stepTop := max
	if normalize != nil {
		stepTop = max - 1
	}

	// splitStep 统一剥步长（#375：三处内联步长切分不再各写一遍）——
	// 之后 body 只剩 "*"、"a-b"、"N" 三种形态。
	body, step, err := splitStep(s)
	if err != nil {
		return err
	}

	if body == "*" {
		if s == "*" {
			// 纯星号：整域全置位 + starBit（「未指定」语义，dayMatches 用）。
			// "*/n" 是指定了节奏，**不得**置 starBit。
			*bits |= starBit
			for i := min; i <= max; i++ {
				*bits |= 1 << norm(i)
			}
			return nil
		}
		// "*/n"：整域步进
		for i := min; i <= max; i += step {
			*bits |= 1 << norm(i)
		}
		return nil
	}

	// 范围 a-b（步长已剥，含 a-b/n 形态）
	if startStr, endStr, ok := strings.Cut(body, "-"); ok {
		var start, end uint64
		start, err = parseUint(startStr)
		if err != nil {
			return err
		}
		end, err = parseUint(endStr)
		if err != nil {
			return err
		}
		if start > end {
			return fmt.Errorf("invalid range: %s", s)
		}
		if start < min || end > max {
			return fmt.Errorf("out of range [%d, %d]: %s", min, max, s)
		}
		for i := start; i <= end; i += step {
			*bits |= 1 << norm(i)
		}
		return nil
	}

	// 单值（含 "N/step"）
	var start uint64
	start, err = parseUint(body)
	if err != nil {
		return err
	}
	if start < min || start > max {
		// "N/step" 报整段（既有文案口径），纯单值报值本身
		if strings.ContainsRune(s, '/') {
			return fmt.Errorf("out of range [%d, %d]: %s", min, max, s)
		}
		return fmt.Errorf("out of range [%d, %d]: %d", min, max, start)
	}
	if strings.ContainsRune(s, '/') {
		// 单值步长 "N/step"：从 N（归一后）步进到字段最大值（终点见
		// stepTop 说明）
		start = norm(start)
		for i := start; i <= stepTop; i += step {
			*bits |= 1 << i
		}
		return nil
	}
	// 纯单值（含 dow 的 "7"）：只置位，不步进
	*bits |= 1 << norm(start)
	return nil
}

func parseUint(s string) (uint64, error) {
	// fmt.Sscanf("%d") 不校验剩余输入，"5/2" 会被静默解析为 5、
	// "10abc" 被解析为 10——非法表达式静默错排。strconv 全串严格校验。
	return strconv.ParseUint(s, 10, 64)
}

// WrapSchedule 包装一个 Schedule，在每次触发后添加延迟。
type WrapSchedule struct {
	Schedule Schedule
	Delay    time.Duration
}

// Next 返回内层 Schedule 的下一次触发时间加 Delay。
//
// ⚠️ 零值哨兵穿透（#383）：内层 Next 返回零值 time.Time（不可满足表达式
// 放弃，如 `0 0 31 2 *`——2 月永无 31 日）时**原样返回零值**，不加 Delay。
// 此前对零值直接 Add——time.Time{}.Add(delay) 是公元 1 年 + delay 的非零
// 具体时刻，内层的「放弃」哨兵经包装后失效：调用方 IsZero 判定落空、
// time.Until 为极大负值 ⇒ 立即触发、prev 停在远古 ⇒ 内层 SpecSchedule 从
// 远古重扫 5 年 ⇒ 调度器热转。哨兵语义必须穿透组合子，Next 的 godoc 契约
// 「返回值严格晚于入参，除非返回零值表示放弃」对包装器同样成立。
func (w WrapSchedule) Next(t time.Time) time.Time {
	next := w.Schedule.Next(t)
	if next.IsZero() {
		return next
	}
	return next.Add(w.Delay)
}

// DelaySchedule 在固定延迟后触发。
type DelaySchedule struct {
	Delay time.Duration
}

// Next 返回当前时间加上延迟后的时间。
func (d DelaySchedule) Next(t time.Time) time.Time {
	return t.Add(d.Delay)
}

// NewDelaySchedule 创建一个延迟调度器。
//
// delay < 0 panic（#227）：负延迟让 Next 返回**过去**时刻——调度器收到
// 不晚于当前时间的触发点，可能原地热转或跳过任务，是非法配置而非可钳制
// 的边界（对比 Every 对 <1s 的静默钳制：亚秒只是精度损失，负值是语义
// 错误）。fail-fast 在构造期，不静默。
func NewDelaySchedule(delay time.Duration) DelaySchedule {
	if delay < 0 {
		panic(fmt.Sprintf("cron: NewDelaySchedule 参数非法: delay 不能为负, got %v", delay))
	}
	return DelaySchedule{Delay: delay}
}

// NewRandomDelaySchedule 创建一个随机延迟调度器。
//
// 构造期校验 0 <= min <= max，非法 panic（#227，fail-fast）：
//
//   - min > max：delta 为负会命中 Next 的 delta<=0 分支，随机延迟**静默
//     退化**为固定延迟 min——配置错误无任何信号；
//   - min < 0 或 max < 0：Next 产生负值延迟 ⇒ 返回过去时刻——调度器
//     收到不晚于当前时间的触发点，可能原地热转或跳过任务。
//
// 签名不返回 error：非法区间是调用方的常量配置错误（同 NewDelaySchedule
// 的 panic 口径；直接构造 RandomDelaySchedule{...} 字面量绕过校验属自担
// 行为，Next 保留 delta<=0 的防御分支）。
func NewRandomDelaySchedule(min, max time.Duration) RandomDelaySchedule {
	if min < 0 || max < 0 || min > max {
		panic(fmt.Sprintf("cron: NewRandomDelaySchedule 参数非法: 需要 0 <= min <= max, got min=%v max=%v", min, max))
	}
	return RandomDelaySchedule{Min: min, Max: max}
}

// RandomDelaySchedule 在随机延迟后触发。
type RandomDelaySchedule struct {
	Min, Max time.Duration
}

// Next 返回当前时间加上 [Min, Max] 范围内随机延迟后的时间。
// min == max 时确定性地返回 t + min（delta == 0 无随机空间）。
func (r RandomDelaySchedule) Next(t time.Time) time.Time {
	delta := r.Max - r.Min
	if delta <= 0 {
		// 防御分支：构造期已校验 0 <= min <= max，正常路径不会进入；
		// 字面量直构绕过校验时按固定延迟 min 兜底。
		return t.Add(r.Min)
	}
	jitter := time.Duration(rand.Int63n(int64(delta)))
	return t.Add(r.Min + jitter)
}

// Entry 表示一个 cron 条目。
type Entry struct {
	Schedule Schedule
	Next     time.Time
	Prev     time.Time
	Job      func()
}

// SortEntries 按下次执行时间排序（零值 Next 排最后——「尚未计算」不是
// 「最早」，与既有 sort.Interface 实现同口径）。
func SortEntries(entries []Entry) {
	slices.SortFunc(entries, func(a, b Entry) int {
		switch {
		case a.Next.IsZero() && b.Next.IsZero():
			return 0
		case a.Next.IsZero():
			return 1
		case b.Next.IsZero():
			return -1
		}
		return a.Next.Compare(b.Next)
	})
}

// NextSchedule 返回最近的 N 个调度时间。
//
// n <= 0 返回空切片（不做任何调度计算）——负数此前会传给 make([]time.Time, 0, n)
// 直接 panic（makeslice: cap out of range）。
func NextSchedule(s Schedule, from time.Time, n int) []time.Time {
	if n <= 0 {
		return []time.Time{}
	}
	times := make([]time.Time, 0, n)
	t := from
	for range n {
		t = s.Next(t)
		if t.IsZero() {
			break
		}
		times = append(times, t)
	}
	return times
}

// RoundDuration 将时间向上取整到指定间隔。
func RoundDuration(d time.Duration, precision time.Duration) time.Duration {
	if precision == 0 {
		return d
	}
	return time.Duration(math.Ceil(float64(d)/float64(precision))) * precision
}
