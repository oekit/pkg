package cron

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	starBit = 1 << 63
)

// SpecSchedule 根据 cron 表达式调度任务。
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
	schedule.Dow, err = getField(fields[5], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("invalid day of week field: %w", err)
	}

	return schedule, nil
}

// Every 返回一个以固定间隔调度的 Schedule。
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
func (s *SpecSchedule) Next(t time.Time) time.Time {
	t = t.In(s.Location)

	// 移动到下一秒
	t = t.Add(1*time.Second - time.Duration(t.Nanosecond())*time.Nanosecond)

	yearLimit := t.Year() + 5

WRAP:
	if t.Year() > yearLimit {
		return time.Time{}
	}

	// 检查月份
	for 1<<uint(t.Month())&s.Month == 0 {
		// 先归一到**当月 1 号**再加一个月——顺序反了会整个跳过目标月：
		// AddDate(0,1,0) 在「当前日 > 目标月天数」时会溢出到再下个月
		// （1/31 + 1 月 → 3/3），随后的「复位到 1 号」就锁死在**溢出后**的
		// 月份 ⇒ 目标月被跳过（实测："0 0 0 1 2 *" 从 1/31 求值，下次触发
		// 晚一年：2025-01-31 → 2026-02-01；月底重启的月度/年度任务最常见）。
		t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, s.Location)
		t = t.AddDate(0, 1, 0)
		if t.Month() == time.January {
			goto WRAP
		}
	}

	// 检查日期
	for !s.dayMatches(t) {
		t = t.AddDate(0, 0, 1)
		t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, s.Location)
		if t.Day() == 1 {
			goto WRAP
		}
	}

	// 检查小时
	for 1<<uint(t.Hour())&s.Hour == 0 {
		t = t.Add(1 * time.Hour)
		t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, s.Location)
		if t.Hour() == 0 {
			goto WRAP
		}
	}

	// 检查分钟
	for 1<<uint(t.Minute())&s.Minute == 0 {
		t = t.Add(1 * time.Minute)
		t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, s.Location)
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

func getField(field string, min, max uint64) (uint64, error) {
	var bits uint64

	ranges := strings.Split(field, ",")
	for _, r := range ranges {
		if err := parseRange(r, min, max, &bits); err != nil {
			return 0, err
		}
	}
	return bits, nil
}

func parseRange(s string, min, max uint64, bits *uint64) error {
	if s == "*" {
		*bits |= starBit
		// 设置所有位
		for i := min; i <= max; i++ {
			*bits |= 1 << i
		}
		return nil
	}

	// 处理 */n 步长
	if strings.HasPrefix(s, "*/") {
		step, err := parseUint(s[2:])
		if err != nil {
			return err
		}
		if step == 0 {
			return fmt.Errorf("step cannot be zero")
		}
		for i := min; i <= max; i += step {
			*bits |= 1 << i
		}
		return nil
	}

	// 处理范围 a-b（可带步长 a-b/n）
	parts := strings.SplitN(s, "-", 2)
	if len(parts) == 2 {
		start, err := parseUint(parts[0])
		if err != nil {
			return err
		}
		// 上界可能带步长（"a-b/n"）——**先按 "/" 切分再分别解析**：
		// 直接 parseUint(parts[1]) 会拿 "5/2" 去解析而报错（ParseUint 全串
		// 严格校验），"a-b/n" 永远走不到下面的步长分支（该分支此前是死代码）。
		endPart := parts[1]
		step := uint64(1)
		if stepParts := strings.SplitN(parts[1], "/", 2); len(stepParts) == 2 {
			endPart = stepParts[0]
			step, err = parseUint(stepParts[1])
			if err != nil {
				return err
			}
			if step == 0 {
				return fmt.Errorf("step cannot be zero")
			}
		}
		end, err := parseUint(endPart)
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
			*bits |= 1 << i
		}
		return nil
	}

	// 单值步长 "N/step"（robfig/cron 语义：从 N 开始步进到字段最大值）
	if stepParts := strings.SplitN(s, "/", 2); len(stepParts) == 2 {
		start, err := parseUint(stepParts[0])
		if err != nil {
			return err
		}
		step, err := parseUint(stepParts[1])
		if err != nil {
			return err
		}
		if step == 0 {
			return fmt.Errorf("step cannot be zero")
		}
		if start < min || start > max {
			return fmt.Errorf("out of range [%d, %d]: %s", min, max, s)
		}
		for i := start; i <= max; i += step {
			*bits |= 1 << i
		}
		return nil
	}

	// 单个值
	val, err := parseUint(s)
	if err != nil {
		return err
	}
	if val < min || val > max {
		return fmt.Errorf("out of range [%d, %d]: %d", min, max, val)
	}
	*bits |= 1 << val
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

// Next 返回下一个调度时间。
func (w WrapSchedule) Next(t time.Time) time.Time {
	return w.Schedule.Next(t).Add(w.Delay)
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
func NewDelaySchedule(delay time.Duration) DelaySchedule {
	return DelaySchedule{Delay: delay}
}

// NewRandomDelaySchedule 创建一个随机延迟调度器。
func NewRandomDelaySchedule(min, max time.Duration) RandomDelaySchedule {
	return RandomDelaySchedule{Min: min, Max: max}
}

// RandomDelaySchedule 在随机延迟后触发。
type RandomDelaySchedule struct {
	Min, Max time.Duration
}

// Next 返回当前时间加上 [Min, Max] 范围内随机延迟后的时间。
func (r RandomDelaySchedule) Next(t time.Time) time.Time {
	delta := r.Max - r.Min
	if delta <= 0 {
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

// byTime 按时间排序 Entry 切片。
type byTime []Entry

func (s byTime) Len() int      { return len(s) }
func (s byTime) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byTime) Less(i, j int) bool {
	if s[i].Next.IsZero() {
		return false
	}
	if s[j].Next.IsZero() {
		return true
	}
	return s[i].Next.Before(s[j].Next)
}

// SortEntries 按下次执行时间排序。
func SortEntries(entries []Entry) {
	sort.Sort(byTime(entries))
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
	for i := 0; i < n; i++ {
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
