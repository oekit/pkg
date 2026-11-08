package timex

// ParseFirstInLocation 测试（#271）：无时区 layout 按 loc 解析、带时区
// layout 的 loc 不生效（逐 layout 独立判定）、nil loc 回退 UTC、边界与
// ParseFirst 逐条一致。

import (
	"testing"
	"time"
)

func TestParseFirstInLocation(t *testing.T) {
	cst, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	cases := []struct {
		name    string
		s       string
		loc     *time.Location
		layouts []string
		want    time.Time
		wantLoc *time.Location // want 为 nil 时只断言时刻（Equal）不断言 Location
		ok      bool
	}{
		{
			name:    "无时区layout按loc解析-CST不偏移",
			s:       "2026-11-07 16:45:00",
			loc:     cst,
			layouts: []string{time.RFC3339, "2006-01-02 15:04:05"},
			want:    time.Date(2026, 11, 7, 16, 45, 0, 0, cst),
			wantLoc: cst,
			ok:      true,
		},
		{
			name:    "带时区layout的loc不生效-RFC3339按自带偏移",
			s:       "2026-11-07T08:30:00Z",
			loc:     cst,
			layouts: []string{time.RFC3339},
			want:    time.Date(2026, 11, 7, 8, 30, 0, 0, time.UTC),
			wantLoc: time.UTC,
			ok:      true,
		},
		{
			name:    "带数字偏移layout的loc不生效",
			s:       "2026-11-07T08:30:00+08:00",
			loc:     time.UTC,
			layouts: []string{time.RFC3339},
			// 时刻与 Location 都按自带偏移：+08:00 固定偏移区
			want: time.Date(2026, 11, 7, 8, 30, 0, 0, cst),
			ok:   true,
		},
		{
			name:    "混合layout列表-逐layout独立判定",
			s:       "2026-11-07 09:00:00",
			loc:     cst,
			layouts: []string{time.RFC3339, "2006-01-02 15:04:05"},
			want:    time.Date(2026, 11, 7, 9, 0, 0, 0, cst),
			wantLoc: cst,
			ok:      true,
		},
		{
			name:    "日期only按loc午夜",
			s:       "2026-11-07",
			loc:     cst,
			layouts: []string{"2006-01-02"},
			want:    time.Date(2026, 11, 7, 0, 0, 0, 0, cst),
			wantLoc: cst,
			ok:      true,
		},
		{
			name:    "nilloc回退UTC-与ParseFirst同口径",
			s:       "2026-11-07 16:45:00",
			loc:     nil,
			layouts: []string{"2006-01-02 15:04:05"},
			want:    time.Date(2026, 11, 7, 16, 45, 0, 0, time.UTC),
			wantLoc: time.UTC,
			ok:      true,
		},
		{
			name:    "全失败返回零值false",
			s:       "not-a-date",
			loc:     cst,
			layouts: []string{time.RFC3339, "2006-01-02"},
			ok:      false,
		},
		{
			name:    "空输入串返回false",
			s:       "",
			loc:     cst,
			layouts: []string{time.RFC3339},
			ok:      false,
		},
		{
			name:    "layouts为空返回false",
			s:       "2026-11-07",
			loc:     cst,
			layouts: nil,
			ok:      false,
		},
		{
			name:    "任一空layout返回false",
			s:       "2026-11-07",
			loc:     cst,
			layouts: []string{time.RFC3339, ""},
			ok:      false,
		},
		{
			name:    "首命中即返回",
			s:       "2026-11-07",
			loc:     cst,
			layouts: []string{"2006-01-02", "2006-01-02 15:04:05"},
			want:    time.Date(2026, 11, 7, 0, 0, 0, 0, cst),
			wantLoc: cst,
			ok:      true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseFirstInLocation(tc.s, tc.loc, tc.layouts...)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (got %v)", ok, tc.ok, got)
			}
			if !tc.ok {
				if !got.IsZero() {
					t.Fatalf("失败时零值, got %v", got)
				}
				return
			}
			if !got.Equal(tc.want) {
				t.Fatalf("got %v (%s), want %v (%s)", got, got.Location(), tc.want, tc.want.Location())
			}
			if tc.wantLoc != nil && got.Location() != tc.wantLoc {
				t.Fatalf("Location = %s, want %s", got.Location(), tc.wantLoc)
			}
		})
	}
}

// TestParseFirstInLocation_UpgradePath #271 实证形态：Gitee CST 服务端的
// 无时区字段——ParseFirst 按 UTC 偏 8 小时，ParseFirstInLocation 修正。
func TestParseFirstInLocation_UpgradePath(t *testing.T) {
	cst, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	const giteeDueOn = "2026-11-07 16:45:00"
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"}

	utc, ok := ParseFirst(giteeDueOn, layouts...)
	if !ok {
		t.Fatal("ParseFirst 应命中")
	}
	loc, ok := ParseFirstInLocation(giteeDueOn, cst, layouts...)
	if !ok {
		t.Fatal("ParseFirstInLocation 应命中")
	}
	if offset := utc.Sub(loc); offset != 8*time.Hour {
		t.Fatalf("按 UTC 解析的时刻应比按 CST 解析晚 8 小时, got %v", offset)
	}
	// ParseFirst 侧：墙钟 16:45 原样落 UTC（偏 8 小时的静默偏差所在）
	if want := time.Date(2026, 11, 7, 16, 45, 0, 0, time.UTC); !utc.Equal(want) {
		t.Fatalf("ParseFirst 侧应为 UTC 16:45, got %v", utc)
	}
}

// TestParseFirstInLocation_EqualsParseFirstUTC 无时区 layout 下
// ParseFirstInLocation(s, time.UTC, ...) 与 ParseFirst 逐条一致（退化调用
// 等价性）。
func TestParseFirstInLocation_EqualsParseFirstUTC(t *testing.T) {
	cst, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	inputs := []string{
		"2026-11-07T08:30:00Z",
		"2026-11-07T08:30:00+08:00",
		"2026-11-07 16:45:00",
		"2026-11-07",
		"not-a-date",
		"",
	}
	layoutSets := [][]string{
		{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"},
		{"2006-01-02"},
		{time.RFC3339, ""},
		nil,
	}
	for _, s := range inputs {
		for _, layouts := range layoutSets {
			a, aok := ParseFirst(s, layouts...)
			b, bok := ParseFirstInLocation(s, time.UTC, layouts...)
			c, cok := ParseFirstInLocation(s, cst, layouts...)
			// 退化调用等价：loc=UTC 时与 ParseFirst 逐条一致（含时刻）。
			if aok != bok || !a.Equal(b) {
				t.Fatalf("ParseFirst(%q) = (%v,%v), ParseFirstInLocation(UTC) = (%v,%v)",
					s, a, aok, b, bok)
			}
			// loc 只影响无时区 layout 的墙钟解释（时刻可不同），不影响
			// 「能否解析」的 ok 判定；命中时 Location 落在给定 loc。
			if aok != cok {
				t.Fatalf("ParseFirst(%q) ok=%v, ParseFirstInLocation(CST) ok=%v——ok 判定不应随 loc 变化",
					s, aok, cok)
			}
			if cok && c.Location() != cst && c.Location() != time.UTC {
				t.Fatalf("ParseFirstInLocation(%q, CST) Location = %s——带时区 layout 落自带区、无时区 layout 落 CST",
					s, c.Location())
			}
		}
	}
}
