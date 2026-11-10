package config

// yaml 子集解析器单测：形态覆盖 + 不支持结构逐一报错（fail-fast 不静默）。

import (
	"reflect"
	"testing"
)

func TestParseYAML_BlockMapping(t *testing.T) {
	src := `
# 整行注释
service: git-ferry      # 行尾注释
replicas: 3
ratio: 0.75
debug: true
empty:                  # 空值 → null
db:
  host: localhost
  port: 5432
  options:
    max_conns: 20
`
	got, err := parseYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"service":  "git-ferry",
		"replicas": int64(3),
		"ratio":    0.75,
		"debug":    true,
		"empty":    nil,
		"db": map[string]any{
			"host": "localhost",
			"port": int64(5432),
			"options": map[string]any{
				"max_conns": int64(20),
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestParseYAML_Sequences(t *testing.T) {
	src := `
tags:
  - alpha
  - beta
providers:            # 序列与父键同列的缩进形态
- gitee
- 工蜂: x
servers:
  - name: a
    port: 1
  - name: b
    port: 2
matrix:
  - - 1
    - 2
`
	got, err := parseYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if !reflect.DeepEqual(m["tags"], []any{"alpha", "beta"}) {
		t.Fatalf("tags = %#v", m["tags"])
	}
	if !reflect.DeepEqual(m["providers"], []any{"gitee", map[string]any{"工蜂": "x"}}) {
		t.Fatalf("providers = %#v", m["providers"])
	}
	wantServers := []any{
		map[string]any{"name": "a", "port": int64(1)},
		map[string]any{"name": "b", "port": int64(2)},
	}
	if !reflect.DeepEqual(m["servers"], wantServers) {
		t.Fatalf("servers = %#v", m["servers"])
	}
	if !reflect.DeepEqual(m["matrix"], []any{[]any{int64(1), int64(2)}}) {
		t.Fatalf("matrix = %#v", m["matrix"])
	}
}

func TestParseYAML_ScalarInference(t *testing.T) {
	cases := map[string]any{
		"true":    true,
		"False":   false,
		"42":      int64(42),
		"-7":      int64(-7),
		"+7":      int64(7),
		"3.14":    3.14,
		"1e3":     1000.0,
		"null":    nil,
		"~":       nil,
		"v1.2.3":  "v1.2.3", // 版本串不是数字
		"1_000":   "1_000",  // 下划线数字不支持，按字符串
		"inf":     "inf",    // 非数字开头不尝试数值解析
		"32s":     "32s",
		"a: b":    "a: b",    // 冒号不带空格不是键值分隔
		"foo#bar": "foo#bar", // # 前无空白不是注释
	}
	for in, want := range cases {
		if got := inferScalar(in); !reflect.DeepEqual(got, want) {
			t.Errorf("inferScalar(%q) = %#v, want %#v", in, got, want)
		}
	}
	// 空文档
	if v, err := parseYAML(""); err != nil || v != nil {
		t.Fatalf("空文档: %v %v", v, err)
	}
}

func TestParseYAML_QuotesAndComments(t *testing.T) {
	src := `
single: 'it''s #1'
double: "line1\nline2"
unicode: "\u4e2d\u6587"
plain: unquoted # tail comment
"quoted key": v1
'key: with colon': v2
url: "http://x/#frag"   # 引号内 # 不是注释
`
	got, err := parseYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if m["single"] != "it's #1" {
		t.Errorf("single = %q", m["single"])
	}
	if m["double"] != "line1\nline2" {
		t.Errorf("double = %q", m["double"])
	}
	if m["unicode"] != "中文" {
		t.Errorf("unicode = %q", m["unicode"])
	}
	if m["plain"] != "unquoted" {
		t.Errorf("plain = %q", m["plain"])
	}
	if m["quoted key"] != "v1" {
		t.Errorf("quoted key = %v", m["quoted key"])
	}
	if m["key: with colon"] != "v2" {
		t.Errorf("key with colon = %v", m["key: with colon"])
	}
	if m["url"] != "http://x/#frag" {
		t.Errorf("url = %q", m["url"])
	}
}

func TestParseYAML_DocSeparator(t *testing.T) {
	// 文件头部单个 --- 容忍
	if _, err := parseYAML("---\na: 1\n"); err != nil {
		t.Fatalf("头部 --- 应容忍: %v", err)
	}
	// 第二段 --- 报错（多文档流不支持）
	if _, err := parseYAML("a: 1\n---\nb: 2\n"); err == nil {
		t.Fatal("第二段 --- 应报错")
	}
}

func TestParseYAML_UnsupportedErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"flow 序列", "a: [1, 2]\n"},
		{"flow 映射", "a: {k: v}\n"},
		{"锚点", "a: &x 1\n"},
		{"别名", "a: *x\n"},
		{"显式标签", "a: !!str 1\n"},
		{"块标量 |", "key: |\n  line1\n  line2\n"},
		{"块标量 >", "key: >\n  folded\n"},
		{"多文档", "a: 1\n---\nb: 2\n"},
		{"文档结束符", "a: 1\n...\n"},
		{"重复键", "a: 1\na: 2\n"},
		{"tab 缩进", "a:\n\tb: 1\n"},
		{"引号未闭合", `a: "abc` + "\n"},
		{"缩进异常", "a:\n    b: 1\n  c: 2\n"},
		{"非键值行", "just a scalar line\n"},
		{"残留内容", "a: 1\n- stray\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := parseYAML(c.src); err == nil {
				t.Fatalf("%s 应报错，got nil", c.name)
			}
		})
	}
}
