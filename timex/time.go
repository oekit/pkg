package timex

import (
	"strconv"
	"strings"
	"time"
)

const (
	stdLayout = "2006-01-02 15:04:05"
)

// TimestampString 返回当前 Unix 时间戳的字符串。
// 时间戳始终为 UTC 秒数，不含时区偏移（Unix 时间戳是时区无关的）。
func TimestampString() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}

// GetCurrentTime 返回当前 Unix 时间戳（秒，int64）。
//
// 宽度与同族对齐（#388）：GetCurrentDayStartTime / Date2Stamp / StampInt2Date
// 全部是 int64，唯独「当前时间戳」这个最高频入口曾是 int——32 位平台
// （GOARCH=386/arm）上 2038-01 起回绕为负，且调用方常拿它与 int64 时间戳
// 比较/入库。（0.x 阶段允许 breaking，minor 直接改签名对齐同族，不留
// GetCurrentTime64 并行入口。）
func GetCurrentTime() int64 {
	return time.Now().Unix()
}

// GetCurrentDate 返回当前日期字符串，默认格式为 "2006-01-02"。
// **按本机时区格式化**（time.Unix 的 Format 绑定本机 zone）：跨时区部署时
// 同一时刻的日期字符串随机器时区变化（#493，与 TimestampString 的 UTC 口径、
// Date2Stamp 的本地解析口径并存，各有声明）；需要固定时区请自行 time.Now().
// In(loc) 后 Format。
func GetCurrentDate(format string) string {
	timestamp := time.Now().Unix()
	tm := time.Unix(timestamp, 0)
	if format == "" {
		format = "2006-01-02"
	}
	return tm.Format(format)
}

// GetCurrentDayStartTime 返回今天 00:00:00 的 Unix 时间戳。
func GetCurrentDayStartTime() int64 {
	currDate := GetCurrentDate("")
	currDate += " 00:00:00"
	return Date2Stamp(currDate, stdLayout)
}

// StampString2Date 将时间戳字符串转换为日期格式。
// **按本机时区格式化**（time.Unix 的 Format 绑定本机 zone，#493）：跨时区
// 部署时同一时间戳的日期字符串随机器时区变化。
// 解析失败时返回空字符串。
func StampString2Date(timestampStr, format string) string {
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return ""
	}
	tm := time.Unix(timestamp, 0)

	if format == "" {
		format = stdLayout
	}
	return tm.Format(format)
}

// StampInt2Date 将 Unix 时间戳转换为日期格式。
// **按本机时区格式化**（time.Unix 的 Format 绑定本机 zone，#493）：跨时区
// 部署时同一时间戳的日期字符串随机器时区变化。
func StampInt2Date(timestamp int64, format string) string {
	tm := time.Unix(timestamp, 0)
	if format == "" {
		format = stdLayout
	}
	return tm.Format(format)
}

// Date2Stamp 将日期字符串转换为 Unix 时间戳。
// 使用本地时区解析（time.ParseInLocation），避免 UTC 偏移问题。
// 解析失败时返回 0。
//
// # DST 前跳日的墙钟归一修正（[#261]）
//
// 前跳日的 00:00:00 这类**不存在**的墙钟时刻，Go 的 ParseInLocation 会把
// 结果归一到**前一天**（America/Santiago 2026-09-06 00:00:00 → 09-05
// 23:00）——时间戳偏移一整天，而解析不报错。GetCurrentDayStartTime 走的
// 正是这条路径：DST 前跳日当天「今天 00:00」拿到的是昨天 ⇒ 日切统计、
// 日限额、日报窗口全部偏移一天。同一个坑在 boundary.go 的 dayStart 已修过
// 一次，本函数对齐同一口径：解析结果的墙钟**日期**与输入不一致（被归一
// 挪走）时，改锚定目标日的**首个存在瞬间**，再补回时刻偏移——宁可是当天
// 01:00，也不是前一天 23:00。
//
// layout 含时区元素（MST / ±hh / Z 家族）时时刻由输入串里的偏移决定，
// 不存在本地墙钟歧义，跳过修正。
func Date2Stamp(date string, format string) int64 {
	if format == "" {
		format = stdLayout
	}
	loc := time.Local
	tm, err := time.ParseInLocation(format, date, loc)
	if err != nil {
		return 0
	}
	if layoutHasZone(format) {
		return tm.Unix()
	}
	// 歧义检测：用无 DST 的 UTC 解析同一字符串，拿到「输入写的墙钟字段」。
	// 日期一致（含回拨日的重复墙钟——ParseInLocation 取较早一次，日期不变）
	// 则无归一篡改，按解析结果返回。
	ref, err := time.ParseInLocation(format, date, time.UTC)
	if err != nil {
		return tm.Unix()
	}
	if tm.Year() == ref.Year() && tm.Month() == ref.Month() && tm.Day() == ref.Day() {
		return tm.Unix()
	}
	// 输入的墙钟日期被归一挪走：锚定目标日首个存在瞬间，再补回当天内的
	// 时刻偏移（00:30 这类落在缺口内的时刻顺延到缺口之后，日期不丢）。
	st := dayStart(ref.Year(), ref.Month(), ref.Day(), loc)
	off := time.Duration(ref.Hour())*time.Hour +
		time.Duration(ref.Minute())*time.Minute +
		time.Duration(ref.Second())*time.Second +
		time.Duration(ref.Nanosecond())
	return st.Add(off).Unix()
}

// layoutHasZone 报告 layout 是否含时区元素（时区名 MST、短偏移 -07、
// ISO 家族 Z0700/-0700/-07:00 等）。这些元素全部以 MST、"-07"、"Z07"
// 起头，Contains 覆盖全集；layout 里恰好出现这些字面量只会让修正被
// 安全地跳过，不会误触发。
func layoutHasZone(layout string) bool {
	return strings.Contains(layout, "MST") ||
		strings.Contains(layout, "-07") ||
		strings.Contains(layout, "Z07")
}

// NDateBefore 获取前 n 天的日期列表。
// isMulti 为 true 时返回前 n 天每一天的日期，否则只返回第 n 天的日期。
//
// n <= 0 返回空切片（#472）：「前 0 天」「前负数天」没有良定义语义——
// 此前负 n 在单日分支经 AddDate(0,0,-1*n) 反号成**未来日期**、multi 分支
// 静默返回空，两种形态都无信号（日切统计/报表窗口静默取到未来日期）。
func NDateBefore(n int, isMulti bool, format string) []string {
	if n <= 0 {
		return []string{}
	}
	var rs []string
	nTime := time.Now()

	if format == "" {
		format = "20060102"
	}

	if isMulti {
		j := -1
		for range n {
			yesTime := nTime.AddDate(0, 0, j)
			j--
			logDay := yesTime.Format(format)
			rs = append(rs, logDay)
		}
	} else {
		yesTime := nTime.AddDate(0, 0, -1*n)
		logDay := yesTime.Format(format)
		rs = append(rs, logDay)
	}
	return rs
}

// ConvertTimezone 将日期时间从指定时区转换为本地时区。
//
// DST 前跳修正（#473，对齐 Date2Stamp #261 同一口径）：fromLoc 时区里
// **不存在**的墙钟时刻（如 America/Santiago 2026-09-06 00:00:00 前跳直接
// 到 01:00），ParseInLocation 会把结果静默归一到**前一天**而解析不报错
// ——输入日期被改写无信号。检测到归一挪日时锚定目标日**首个存在瞬间**，
// 再补回当天内的时刻偏移（宁可是当天 01:00，也不是前一天 23:00）。
func ConvertTimezone(datetime string, fromLoc string) (string, error) {
	loc, err := time.LoadLocation(fromLoc)
	if err != nil {
		return "", err
	}
	t, err := time.ParseInLocation(stdLayout, datetime, loc)
	if err != nil {
		return "", err
	}
	// 歧义检测（同 Date2Stamp）：用 UTC 解析同一墙钟串拿到「输入写的字段」。
	// 日期一致（含回拨日的重复墙钟——ParseInLocation 取较早一次，日期不变）
	// 则无归一篡改，按解析结果返回；UTC 对照解析失败（非法输入已在上一步
	// 拦下，此处防御）同样按解析结果返回。
	ref, refErr := time.ParseInLocation(stdLayout, datetime, time.UTC)
	if refErr == nil {
		if t.Year() == ref.Year() && t.Month() == ref.Month() && t.Day() == ref.Day() {
			return time.Unix(t.Unix(), 0).Format(stdLayout), nil
		}
		// 输入的墙钟日期被归一挪走：锚定目标日首个存在瞬间，补回当天内的
		// 时刻偏移（00:30 这类落在缺口内的时刻顺延到缺口之后，日期不丢）。
		st := dayStart(ref.Year(), ref.Month(), ref.Day(), loc)
		off := time.Duration(ref.Hour())*time.Hour +
			time.Duration(ref.Minute())*time.Minute +
			time.Duration(ref.Second())*time.Second
		return time.Unix(st.Add(off).Unix(), 0).Format(stdLayout), nil
	}
	return time.Unix(t.Unix(), 0).Format(stdLayout), nil
}
