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

// TestNormalizeBaseURL_SchemeCaseInsensitive 锁定 #239：scheme 前缀判定按
// RFC 3986 §3.1 大小写不敏感——大写/混合大小写 scheme 不再被二次补 http://
// （此前 HTTPS://EXAMPLE.COM 产出 http://HTTPS://EXAMPLE.COM，host 变 "HTTPS:"、
// scheme 静默降级 https→http）。scheme 归一为小写；host 大小写不属于本函数
// 职责（RFC 只对 scheme 定义大小写不敏感归一），保持原样。
func TestNormalizeBaseURL_SchemeCaseInsensitive(t *testing.T) {
	cases := []struct{ in, want string }{
		{"HTTPS://EXAMPLE.COM", "https://EXAMPLE.COM"},  // issue #239 表行 1
		{"Https://example.com", "https://example.com"},  // issue #239 表行 2
		{"HTTP://host", "http://host"},                  // issue #239 表行 3
		{"https://example.com/", "https://example.com"}, // 对照：小写不回归（issue #239 表行 4）
		{"HTTPs://Host/PaTh/", "https://Host/PaTh"},     // 混合大小写 + 路径尾斜杠
	}
	for _, c := range cases {
		if got := NormalizeBaseURL(c.in); got != c.want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestNormalizeBaseURL_TrailingSlashScope 锁定 #240：尾斜杠裁剪只作用于 path
// 段——scheme 的 // 不被截掉（scheme-only 输入保持 http://），query/fragment
// 的尾斜杠原样保留（不再被整串 TrimRight 静默改值）。
func TestNormalizeBaseURL_TrailingSlashScope(t *testing.T) {
	cases := []struct{ in, want string }{
		// 面 1：scheme-only 输入不再截成裸 scheme
		{"http://", "http://"},   // issue #240：此前 → "http:"（authority 丢失）
		{"https://", "https://"}, // 同上
		{"http:///", "http://"},  // issue #240：此前 → "http:"
		// path 段尾斜杠照旧全去（既有行为）
		{"https://a.com/", "https://a.com"},
		{"https://a.com/path/", "https://a.com/path"},
		{"https://a.com//", "https://a.com"},
		// 面 2：query/fragment 尾斜杠原样保留（此前被静默吃掉）
		{"http://host/?a=1/", "http://host?a=1/"},               // query 值 "1/" 不再变 "1"
		{"http://host/path/?x=/", "http://host/path?x=/"},       // query 值 "/" 不再变空
		{"http://host/#frag/", "http://host#frag/"},             // fragment 尾斜杠保留
		{"https://a.com/p/?q=1&#f/", "https://a.com/p?q=1&#f/"}, // ? 与 # 并存：首个 ? 后全保留
	}
	for _, c := range cases {
		if got := NormalizeBaseURL(c.in); got != c.want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
