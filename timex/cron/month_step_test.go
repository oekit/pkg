package cron

import (
	"testing"
	"time"
)

// TestSpecSchedule_Next_MonthNotSkipped 回归测试：月份扫描不得跳过目标月。
//
// 缺陷成因：月份推进是「先 AddDate(0,1,0) 再复位到 1 号」——AddDate 在
// 「当前日 > 目标月天数」时会溢出到再下个月（1/31 + 1 月 → 3/3），随后的
// 复位就锁死在**溢出后**的月份 ⇒ 目标月被整个跳过。
//
// 实测（修前）："0 0 0 1 2 *"（2 月 1 日）从 2025-01-31 求值 → 2026-02-01，
// 晚了一年；月底重启的月度/年度任务最常见。
//
// 注：Parse 产出的 Schedule 用 time.Local（见 Parse 的 Location 字段），
// 期望值同样按 Local 构造。
func TestSpecSchedule_Next_MonthNotSkipped(t *testing.T) {
	loc := time.Local
	cases := []struct {
		spec string
		from time.Time
		want time.Time
	}{
		{"0 0 0 1 2 *", time.Date(2025, 1, 31, 0, 0, 0, 0, loc), time.Date(2025, 2, 1, 0, 0, 0, 0, loc)},
		{"0 0 0 1 4 *", time.Date(2025, 3, 31, 0, 0, 0, 0, loc), time.Date(2025, 4, 1, 0, 0, 0, 0, loc)},
		// 跨年：12 月 → 次年 1 月。
		{"0 0 0 1 1 *", time.Date(2025, 12, 31, 0, 0, 0, 0, loc), time.Date(2026, 1, 1, 0, 0, 0, 0, loc)},
		// 闰年 2 月：2024-02-29。
		{"0 0 0 29 2 *", time.Date(2024, 1, 31, 0, 0, 0, 0, loc), time.Date(2024, 2, 29, 0, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		s, err := Parse(c.spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.spec, err)
		}
		got := s.Next(c.from)
		if !got.Equal(c.want) {
			t.Errorf("Parse(%q).Next(%v) = %v，期望 %v", c.spec, c.from, got, c.want)
		}
	}
}

// TestSpecSchedule_Next_NoSuchDate 目标月里没有那个日号（如 2 月 30 日）
// 时不返回任何时间（零值）——既有契约，不受本次修复影响。
func TestSpecSchedule_Next_NoSuchDate(t *testing.T) {
	s, err := Parse("0 0 0 30 2 *")
	if err != nil {
		t.Fatal(err)
	}
	got := s.Next(time.Date(2025, 1, 31, 0, 0, 0, 0, time.Local))
	if !got.IsZero() {
		t.Fatalf("2 月 30 日不存在，应返回零值时间，实得 %v", got)
	}
}

// TestSpecSchedule_Next_MonthNotSkipped_LongRun 连续 12 个月逐月推进，
// 每月都必须恰好命中一次（跳过任何一个月都会在此暴露）。
func TestSpecSchedule_Next_MonthNotSkipped_LongRun(t *testing.T) {
	s, err := Parse("0 0 0 1 * *") // 每月 1 日
	if err != nil {
		t.Fatal(err)
	}
	cur := time.Date(2025, 1, 31, 12, 0, 0, 0, time.Local)
	for i := 0; i < 12; i++ {
		next := s.Next(cur)
		want := time.Date(2025, time.Month(2+i%12), 1, 0, 0, 0, 0, time.Local)
		if next.Year() != want.Year() || next.Month() != want.Month() {
			t.Fatalf("第 %d 次推进得到 %v，期望落在 %v（目标月被跳过）", i+1, next, want)
		}
		cur = next
	}
}

// TestParse_RangeWithStep 回归测试："a-b/n"（区间带步长）必须被接受。
//
// 缺陷成因：上界先被 parseUint("5/2") 整串严格校验拦下（报
// "invalid ... field: parsing \"5/2\""），后面那段处理步长的代码是死代码。
//
// 注：cron 6 字段格式为 秒 分 时 日 月 周。
func TestParse_RangeWithStep(t *testing.T) {
	cases := []struct {
		spec  string
		field func(*SpecSchedule) uint64
		want  []uint
	}{
		// 小时 1-5/2 ⇒ 1,3,5
		{"0 0 1-5/2 * * *", func(s *SpecSchedule) uint64 { return s.Hour }, []uint{1, 3, 5}},
		// 小时 0-6/3 ⇒ 0,3,6
		{"0 0 0-6/3 * * *", func(s *SpecSchedule) uint64 { return s.Hour }, []uint{0, 3, 6}},
		// 分钟 0-10/5 ⇒ 0,5,10
		{"0 0-10/5 * * * *", func(s *SpecSchedule) uint64 { return s.Minute }, []uint{0, 5, 10}},
	}
	for _, c := range cases {
		s, err := Parse(c.spec)
		if err != nil {
			t.Fatalf("Parse(%q) 应被接受，实得 %v", c.spec, err)
		}
		spec, ok := s.(*SpecSchedule)
		if !ok {
			t.Fatalf("Parse(%q) 应返回 *SpecSchedule，实得 %T", c.spec, s)
		}
		bits := c.field(spec)
		for _, v := range c.want {
			if bits&(1<<v) == 0 {
				t.Errorf("Parse(%q) 未包含 %d（bits=%b）", c.spec, v, bits)
			}
		}
	}

	// 步长外的值不得被包含（小时 1-5/2 ⇒ 不含 2/4）。
	s, err := Parse("0 0 1-5/2 * * *")
	if err != nil {
		t.Fatal(err)
	}
	spec := s.(*SpecSchedule)
	for _, v := range []uint{0, 2, 4} {
		if spec.Hour&(1<<v) != 0 {
			t.Errorf("Parse(\"0 0 1-5/2 * * *\") 不应包含 %d（bits=%b）", v, spec.Hour)
		}
	}
}

// TestParse_RangeWithStep_Invalid 步长非法时仍须报错（不得静默接受）。
func TestParse_RangeWithStep_Invalid(t *testing.T) {
	for _, spec := range []string{"0 0 1-5/0 * * *", "0 0 1-5/x * * *", "0 0 1-5/ * * *"} {
		if _, err := Parse(spec); err == nil {
			t.Errorf("Parse(%q) 应报错", spec)
		}
	}
}

// TestNextSchedule_NonPositiveN 回归测试：n <= 0 不得 panic。
//
// 缺陷成因：make([]time.Time, 0, n) 遇负数直接 panic（makeslice: cap out of range）。
func TestNextSchedule_NonPositiveN(t *testing.T) {
	s, err := Parse("0 0 0 * * *")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{-1, -100, 0} {
		got := NextSchedule(s, time.Now(), n)
		if len(got) != 0 {
			t.Errorf("NextSchedule(n=%d) 应返回空切片，实得 %d 项", n, len(got))
		}
	}
}
