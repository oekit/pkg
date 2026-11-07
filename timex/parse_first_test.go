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
