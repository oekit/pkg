package pathx

import "testing"

// #532：JoinURL 拼接规则契约（语义对齐 httpx#112 joinEndpoint）。

func TestJoinURL(t *testing.T) {
	cases := []struct {
		name string
		base string
		path string
		want string
	}{
		{"缺前导斜杠补齐", "http://h:8080", "api/x", "http://h:8080/api/x"},
		{"base 单尾斜杠去掉", "http://h:8080/", "api/x", "http://h:8080/api/x"},
		{"base 多尾斜杠全去掉", "http://h:8080///", "api/x", "http://h:8080/api/x"},
		{"path 带前导斜杠不重复补", "http://h:8080", "/api/x", "http://h:8080/api/x"},
		{"base 尾斜杠与 path 前导斜杠并存", "http://h:8080/", "/api/x", "http://h:8080/api/x"},
		{"path 为空返回 base 原样", "http://h:8080", "", "http://h:8080"},
		{"path 为空 base 尾斜杠保留", "http://h:8080/", "", "http://h:8080/"},
		{"base 空串补前导斜杠", "", "api/x", "/api/x"},
		{"两者皆空返回空串", "", "", ""},
		{"query 原样保留", "http://h:8080", "api/x?k=1&j=2", "http://h:8080/api/x?k=1&j=2"},
		{"path 尾斜杠保留", "http://h:8080", "api/", "http://h:8080/api/"},
		{"path 多前导斜杠不归一", "http://h:8080", "//api", "http://h:8080//api"},
		{"静态 endpoint 形态", "http://10.0.0.1:9000", "/v1/health", "http://10.0.0.1:9000/v1/health"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := JoinURL(tc.base, tc.path); got != tc.want {
				t.Errorf("JoinURL(%q, %q) = %q, want %q", tc.base, tc.path, got, tc.want)
			}
		})
	}
}

func TestJoinURL_PathEmpty_NeverTouchesBase(t *testing.T) {
	// path 为空的分支在 TrimRight 之前返回：base 的任何形态（含多尾斜杠）
	// 原样透出，不带出任何归一副作用。
	for _, base := range []string{"", "http://h:8080///", "relative/path/"} {
		if got := JoinURL(base, ""); got != base {
			t.Errorf("JoinURL(%q, \"\") = %q, want 原样 %q", base, got, base)
		}
	}
}
