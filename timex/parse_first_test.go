package timex

// ParseFirst 多 layout 容错解析测试（#197）：first-match 语义、空串/空
// layout 边界、全失败零值。

import (
	"testing"
	"time"
)

func TestParseFirst(t *testing.T) {
	cases := []struct {
		name    string
		s       string
		layouts []string
		want    time.Time
		ok      bool
	}{
		{
			name:    "RFC3339命中",
			s:       "2026-11-07T08:30:00Z",
			layouts: []string{time.RFC3339, "2006-01-02"},
			want:    time.Date(2026, 11, 7, 8, 30, 0, 0, time.UTC),
			ok:      true,
		},
		{
			name:    "日期only命中第二layout",
			s:       "2026-11-07",
			layouts: []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"},
			want:    time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC),
			ok:      true,
		},
		{
			name:    "Gitee wire形命中中间layout",
			s:       "2026-11-07 16:45:00",
			layouts: []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"},
			want:    time.Date(2026, 11, 7, 16, 45, 0, 0, time.UTC),
			ok:      true,
		},
		{
			name:    "首命中即返回-顺序即优先序",
			s:       "2026-11-07",
			layouts: []string{"2006-01-02", time.RFC3339},
			want:    time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC),
			ok:      true,
		},
		{
			name:    "全失败返回零值false",
			s:       "not-a-date",
			layouts: []string{time.RFC3339, "2006-01-02"},
			ok:      false,
		},
		{
			name:    "空输入串返回false",
			s:       "",
			layouts: []string{time.RFC3339},
			ok:      false,
		},
		{
			name:    "layouts为空返回false",
			s:       "2026-11-07",
			layouts: nil,
			ok:      false,
		},
		{
			name:    "layout含空串返回false",
			s:       "2026-11-07",
			layouts: []string{time.RFC3339, ""},
			ok:      false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseFirst(tc.s, tc.layouts...)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && !got.Equal(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if !ok && !got.IsZero() {
				t.Fatalf("失败时应返回零值, got %v", got)
			}
		})
	}
}

// TestParseFirst_FirstMatchStopsParsing 钉住 first-match：坏 layout 在前、
// 可命中 layout 在后仍成功；反之全部不可命中则 false。
func TestParseFirst_FirstMatchStopsParsing(t *testing.T) {
	if _, ok := ParseFirst("2026-11-07", "2006-01-02 15:04:05", "2006-01-02"); !ok {
		t.Fatal("坏 layout 在前不应影响后续命中")
	}
	if _, ok := ParseFirst("2026-11-07", "2006-01-02 15:04:05", time.RFC3339); ok {
		t.Fatal("全部 layout 不可命中应返回 false")
	}
}

// TestParseFirst_AbbrevZoneLayoutTrapsUTC 钉住 #425：时区**缩写名** layout
// （CST/MST…）的解析依赖 Location 的 zone 表——time.Parse 用 Local
// （TZ=Asia/Shanghai 时 CST → +0800），ParseFirst 恒委托 UTC 口径（无 CST
// 表项 → 按墙钟落 UTC）⇒ 差 8 小时，且 CI（TZ=UTC）恒绿掩盖偏移。本测试
// 文档化该陷阱（修法拍板为文档+测试钉住，不 panic）：两个入口在缩写名
// layout 下的差异必须与 time.Parse/ParseInLocation 各自语义一致——
// 「与 time.Parse 一致」的错误承诺已从 godoc 删除。
func TestParseFirst_AbbrevZoneLayoutTrapsUTC(t *testing.T) {
	layout := "2006-01-02 15:04 MST"
	value := "2026-03-10 12:00 CST"

	// ParseFirst（UTC 口径）：CST 在 UTC 表无表项 ⇒ 墙钟落 UTC。
	got, ok := ParseFirst(value, layout)
	if !ok {
		t.Fatal("缩写名 layout 应解析成功")
	}
	wantUTC, err := time.ParseInLocation(layout, value, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(wantUTC) {
		t.Fatalf("ParseFirst(UTC 口径) = %v, want %v（与 ParseInLocation(UTC) 同语义）", got, wantUTC)
	}

	// 与 time.Parse（Local）的差异是**系统性**的：Local=UTC 环境下两者一致；
	// Local=+8 环境下差 8h——测试不依赖 TZ，直接对照两个 Location 的解析
	// 结果差值，钉住「缩写名 layout 必须传产线 loc」的指引。
	cst, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("环境无 tzdata：", err)
	}
	gotCST, ok := ParseFirstInLocation(value, cst, layout)
	if !ok {
		t.Fatal("ParseFirstInLocation(CST) 应解析成功")
	}
	// 12:00 CST（+0800）比 12:00 UTC **早** 8 小时 ⇒ 差值 -8h。
	if diff := gotCST.Sub(got); diff != -8*time.Hour {
		t.Fatalf("UTC 口径与产线 loc 口径差 %v, want -8h（差值即缩写名陷阱的偏移量）", diff)
	}
}
