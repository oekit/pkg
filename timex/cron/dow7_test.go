package cron

import (
	"testing"
	"time"
)

// 本文件覆盖 dow 字段 7≡Sunday 在**复合形态**下的归一（[#215] / [#206]）。
//
// #186 的修法是整串判等：`if dowSpec == "7" { dowSpec = "0" }`。于是
//
//	Parse("0 2 * * 6,7")  → out of range [0, 6]: 7
//	Parse("0 2 * * 5-7")  → out of range [0, 6]: 5-7
//
// 而报错文案写着「7≡Sunday 已支持」——把用户指向「你表达式写错了」。
// vixie cron / cronie / robfig 在列表与范围端点同样接受 7，`5-7`（周五到
// 周日：0=Sunday 编号下 5=Fri、6=Sat、7≡0=Sun）是从系统 crontab 迁移的
// 高频写法。

func dowBits(t *testing.T, spec string) uint64 {
	t.Helper()
	s, err := Parse(spec)
	if err != nil {
		t.Fatalf("Parse(%q) 报错：%v", spec, err)
	}
	ss, ok := s.(*SpecSchedule)
	if !ok {
		t.Fatalf("Parse(%q) 应返回 *SpecSchedule，实得 %T", spec, s)
	}
	return ss.Dow
}

func bitsOf(days ...uint64) uint64 {
	var b uint64
	for _, d := range days {
		b |= 1 << d
	}
	return b
}

// TestParse_DowSevenComposite 7≡Sunday 必须在单值 / 列表 / 范围 / 步长四种
// 形态下等价于 0。
func TestParse_DowSevenComposite(t *testing.T) {
	cases := []struct {
		spec string
		want uint64
	}{
		{"0 2 * * 7", bitsOf(0)},                     // 单值（#186 已覆盖）
		{"0 2 * * 6,7", bitsOf(6, 0)},                // 列表端点
		{"0 2 * * 7,6", bitsOf(0, 6)},                // 列表里的位置无关
		{"0 2 * * 5-7", bitsOf(5, 6, 0)},             // 范围端点（环形：5,6,0）
		{"0 2 * * 6-7", bitsOf(6, 0)},                // 范围只到 7
		{"0 2 * * 7-7", bitsOf(0)},                   // 退化范围
		{"0 2 * * 0-7", bitsOf(0, 1, 2, 3, 4, 5, 6)}, // 全周
		{"0 2 * * 5-7/2", bitsOf(5, 0)},              // 范围 + 步长：5, 7→0
		{"0 2 * * 7/2", bitsOf(0, 2, 4, 6)},          // 单值步长：7→0，再从 0 步到 6
		{"0 2 * * 0-6", bitsOf(0, 1, 2, 3, 4, 5, 6)}, // 既有写法不受影响
		{"0 2 * * 1,3,5", bitsOf(1, 3, 5)},           // 既有列表不受影响
	}
	for _, c := range cases {
		if got := dowBits(t, c.spec); got != c.want {
			t.Fatalf("%s：Dow bits = %07b，want %07b", c.spec, got, c.want)
		}
	}
}

// TestParse_DowSevenOutOfRange 真正越界的值仍然必须报错，且文案不再自称
// 「7 已支持」这种与实际相反的话。
func TestParse_DowSevenOutOfRange(t *testing.T) {
	for _, spec := range []string{"0 2 * * 8", "0 2 * * 5-8", "0 2 * * 7,8", "0 2 * * 9-7"} {
		if _, err := Parse(spec); err == nil {
			t.Fatalf("Parse(%q) 应报错（越界）", spec)
		}
	}
}

// TestNext_DowSevenComposite_FiresOnWeekend 行为面验证：`5-7` 真的落在
// 周五/周六/周日三天（0=Sunday 编号下 5=Fri、6=Sat、7≡0=Sun），且周一至
// 周四不触发。
//
// 从 2026-11-06（周五）12:00 UTC 出发，序列应为：
// 11-07(六) → 11-08(日) → 11-13(五) —— 若 7 被丢弃，第三条会跳到 11-20
// （漏掉周日之后的那个周五）；若被当成周六，第二条会重复 11-07。
func TestNext_DowSevenComposite_FiresOnWeekend(t *testing.T) {
	sched := mustSpec(t, "0 0 0 * * 5-7", "UTC")
	cur := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

	wantDays := []int{7, 8, 13}
	for i, want := range wantDays {
		next := sched.Next(cur)
		if next.Day() != want || next.Month() != time.November {
			t.Fatalf("第 %d 次 Next(%v) = %v，want 2026-11-%02d", i, cur, next, want)
		}
		if next.Weekday() == time.Monday {
			t.Fatalf("第 %d 次触发落在周一：%v", i, next)
		}
		cur = next
	}
}
