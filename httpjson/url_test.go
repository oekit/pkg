package httpjson

import "testing"

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},    // 空串原样（未配置语义不改写）
		{"   ", ""}, // 纯空白归空
		{"zentao.example.com", "http://zentao.example.com"},  // 补 scheme
		{"zentao.example.com/", "http://zentao.example.com"}, // 补 scheme + 去尾斜杠
		{"http://a.b", "http://a.b"},                         // 已带 scheme 不动
		{"https://a.b/", "https://a.b"},                      // https 不补 http、去尾斜杠
		{"http://a.b//", "http://a.b"},                       // 多个尾斜杠全去
		{"  https://a.b/path/  ", "https://a.b/path"},        // 首尾空白
	}
	for _, c := range cases {
		if got := NormalizeBaseURL(c.in); got != c.want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
