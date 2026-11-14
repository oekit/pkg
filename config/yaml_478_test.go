package config

import (
	"strings"
	"testing"
)

// #478：yaml 双引号 \u 转义对代理区码点曾静默写成 U+FFFD 且不组合代理对
// （\ud83d\ude00 → 两个替换符）。成对出现组合成增补平面码点（与
// encoding/json 同口径）；孤立代理按「不支持的结构显式报错」原则拒绝。
func TestYAMLEscapeSurrogatePairs(t *testing.T) {
	type c struct {
		Emoji string `yaml:"emoji"`
	}
	path := writeCfgFile(t, "emoji: \"\\ud83d\\ude00\"\n")
	v := &c{}
	if err := Load(v, WithFile(path)); err != nil {
		t.Fatal(err)
	}
	if v.Emoji != "\U0001F600" {
		t.Fatalf("\\ud83d\\ude00 = %q, want 😀（代理对必须合并，此前静默 2×U+FFFD）", v.Emoji)
	}
}

func TestYAMLEscapeLoneSurrogateRejected(t *testing.T) {
	type c struct {
		V string `yaml:"v"`
	}
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"孤立高代理", "v: \"\\ud800\"\n"},
		{"孤立低代理", "v: \"\\udc00\"\n"},
		{"高代理后非转义", "v: \"\\ud83dabc\"\n"},
		{"高代理后非法低代理", "v: \"\\ud83d\\u0041\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeCfgFile(t, tc.raw)
			err := Load(&c{}, WithFile(path))
			if err == nil || !strings.Contains(err.Error(), "代理") {
				t.Fatalf("孤立代理应显式报错（此前静默 U+FFFD）: %v", err)
			}
		})
	}
}
