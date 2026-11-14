package timex

import (
	"testing"
	"time"
)

// #472：NDateBefore 负 n 曾在单日分支反号成未来日期、multi 分支静默返回空
// ——两种形态都无信号（日切统计/报表窗口静默取到未来日期）。
func TestNDateBeforeNonPositiveN(t *testing.T) {
	for _, n := range []int{0, -1, -3} {
		for _, multi := range []bool{false, true} {
			got := NDateBefore(n, multi, "")
			if len(got) != 0 {
				t.Errorf("NDateBefore(%d, %v) = %v, want empty", n, multi, got)
			}
		}
	}
	// 正 n 两种分支的长度语义不变。
	if got := NDateBefore(3, true, "20060102"); len(got) != 3 {
		t.Errorf("NDateBefore(3, true) len = %d, want 3", len(got))
	}
	if got := NDateBefore(3, false, "20060102"); len(got) != 1 {
		t.Errorf("NDateBefore(3, false) len = %d, want 1", len(got))
	}
}

// #473：ConvertTimezone 对 DST 前跳日**不存在**的墙钟时刻，ParseInLocation
// 曾把结果归一到前一天（America/Santiago 2026-09-06 00:30:00 → 09-05
// 23:00 的同族形态）而解析不报错。对齐 Date2Stamp #261 口径：锚定目标日
// **首个存在瞬间**再补回时刻偏移——输入日期不丢。
func TestConvertTimezone_DSTSpringForward_NoDayShift(t *testing.T) {
	_, scl := dstZones(t)
	// 输出按本机时区格式化：把 Local 钉到 fromLoc，断言就是确定性的
	// Santiago 墙钟（from=to 时区时转换应恒等）。
	withLocal(t, scl)

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"前跳缺口内 00:00:00", "2026-09-06 00:00:00", "2026-09-06 01:00:00"},
		{"前跳缺口内 00:30:00", "2026-09-06 00:30:00", "2026-09-06 01:30:00"},
		{"缺口后正常时刻", "2026-09-06 12:00:00", "2026-09-06 12:00:00"},
		{"正常日", "2026-09-05 00:00:00", "2026-09-05 00:00:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ConvertTimezone(tc.in, "America/Santiago")
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("ConvertTimezone(%q) = %q, want %q（缺口时刻必须落在当天）", tc.in, got, tc.want)
			}
		})
	}
}

// #473 对照：回拨日（重复墙钟，ParseInLocation 取较早一次）与正常日不触发
// 修正。
func TestConvertTimezone_DSTFallBack_SameDay(t *testing.T) {
	withLocal(t, time.UTC)
	got, err := ConvertTimezone("2026-11-01 01:30:00", "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// 回拨取较早一次（EDT，UTC-4）→ UTC 05:30，日期不变。
	if want := "2026-11-01 05:30:00"; got != want {
		t.Fatalf("ConvertTimezone fall-back = %q, want %q", got, want)
	}
}
