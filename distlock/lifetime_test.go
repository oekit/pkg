package distlock

import (
	"testing"
	"time"
)

// #534：MaxLifetime 三态契约（语义照搬 redisx/distlock renewMaxLifetime）。

func TestMaxLifetime(t *testing.T) {
	const ttl = 30 * time.Second
	cases := []struct {
		name       string
		configured time.Duration
		ttl        time.Duration
		want       time.Duration
	}{
		{"未设置取 10 倍 ttl", 0, ttl, 300 * time.Second},
		{"未设置且 ttl 极小", 0, time.Nanosecond, 10 * time.Nanosecond},
		{"负值不限返回 0", -1, ttl, 0},
		{"负大值同样不限", -24 * time.Hour, ttl, 0},
		{"正值为显式上限", 45 * time.Second, ttl, 45 * time.Second},
		{"正值小于 ttl 亦显式保留", time.Second, ttl, time.Second},
		{"正下界 1ns 保留", time.Nanosecond, ttl, time.Nanosecond},
		{"ttl 为零且未设置自然归零", 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaxLifetime(tc.configured, tc.ttl); got != tc.want {
				t.Fatalf("MaxLifetime(%v, %v) = %v, want %v", tc.configured, tc.ttl, got, tc.want)
			}
		})
	}
}
