package stringx

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// maskRules 固定规则表（仅存在于测试文件，库不内置任何词表）。
func maskRules(t *testing.T) []Rule {
	t.Helper()
	return []Rule{
		{Name: "kv-secret", Re: regexp.MustCompile(`password=([^\s]+)`), Keep: 2},
		{Name: "url-userinfo", Re: regexp.MustCompile(`://[^/\s:]+:[^/\s]+@`), Keep: 0},
		{Name: "private-key", Re: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`), Keep: 0},
	}
}

// TestNewMaskerErrors 构造期错误条件：nil Re、空 Name、重名、负 Keep、
// 非法形态标签字符。
func TestNewMaskerErrors(t *testing.T) {
	tests := []struct {
		name  string
		rules []Rule
	}{
		{"Re 为 nil", []Rule{{Name: "a", Re: nil, Keep: 0}}},
		{"Name 为空", []Rule{{Name: "", Re: regexp.MustCompile(`x`), Keep: 0}}},
		{"Name 重复", []Rule{
			{Name: "dup", Re: regexp.MustCompile(`x`), Keep: 0},
			{Name: "dup", Re: regexp.MustCompile(`y`), Keep: 0},
		}},
		{"Keep 为负", []Rule{{Name: "a", Re: regexp.MustCompile(`x`), Keep: -1}}},
		{"Name 含定界符", []Rule{{Name: "a«b", Re: regexp.MustCompile(`x`), Keep: 0}}},
		{"Name 含控制字符", []Rule{{Name: "a\nb", Re: regexp.MustCompile(`x`), Keep: 0}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMasker(tt.rules)
			if err == nil {
				t.Fatalf("NewMasker(%v) 应报错，实际返回 %v", tt.name, m)
			}
			if m != nil {
				t.Errorf("NewMasker 出错时应返回 nil masker，实际 %v", m)
			}
		})
	}
}

// TestNewMaskerEmpty 空规则表可用，Mask 原样返回输入。
func TestNewMaskerEmpty(t *testing.T) {
	m, err := NewMasker(nil)
	if err != nil {
		t.Fatalf("NewMasker(nil) err = %v, want nil", err)
	}
	if got := m.Mask("hello 世界"); got != "hello 世界" {
		t.Errorf("Mask = %q, want %q", got, "hello 世界")
	}
}

// TestMaskerFixedVector 固定向量：给定规则表与输入，断言精确输出。
func TestMaskerFixedVector(t *testing.T) {
	m, err := NewMasker(maskRules(t))
	if err != nil {
		t.Fatalf("NewMasker err = %v", err)
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "多形态混合",
			in:   "login password=hunter2 via postgres://u:p@host/db",
			want: "login pa«kv-secret:****»r2 via postgres«url-userinfo:****»host/db",
		},
		{
			name: "私钥块全掩",
			in:   "-----BEGIN RSA PRIVATE KEY-----\nMIIEabc\n",
			want: "«private-key:****»\nMIIEabc\n",
		},
		{
			name: "无命中",
			in:   "nothing to see here",
			want: "nothing to see here",
		},
		{
			name: "空输入",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := m.Mask(tt.in); got != tt.want {
				t.Errorf("Mask(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestMaskerDeterministic 同一输入同一规则表，输出逐字节确定。
func TestMaskerDeterministic(t *testing.T) {
	m, err := NewMasker(maskRules(t))
	if err != nil {
		t.Fatalf("NewMasker err = %v", err)
	}
	in := "login password=hunter2 via postgres://u:p@host/db"
	first := m.Mask(in)
	for i := 0; i < 50; i++ {
		if got := m.Mask(in); got != first {
			t.Fatalf("第 %d 次 Mask 输出不稳定: %q vs %q", i, got, first)
		}
	}
}

// TestMaskerKeepSemantics Keep 语义：命中长度 >= Keep*4 时保留首尾各 Keep 个
// 字符（位数封顶、不随原值长度变化）；不足 Keep*4 时整段全量打码。
func TestMaskerKeepSemantics(t *testing.T) {
	tests := []struct {
		name string
		re   string
		keep int
		in   string
		want string
		// digits 是该用例命中值的原样字符，全掩分支下不得出现在输出里。
		digits string
	}{
		{"Keep=0 全掩", `[0-9]{4}`, 0, "值 1234 尾", "值 «n:****» 尾", "1234"},
		{"Keep=2 长度够（n=8=Keep*4）", `[0-9]{8}`, 2, "值 12345678 尾", "值 12«n:****»78 尾", "12345678"},
		{"Keep=2 长度不够（n=7=Keep*4-1）全掩", `[0-9]{7}`, 2, "值 1234567 尾", "值 «n:****» 尾", "1234567"},
		{"Keep 超过原值长度（全掩）", `[0-9]{3}`, 5, "值 123 尾", "值 «n:****» 尾", "123"},
		{"Keep 超过原值长度（2 字符，全掩）", `[0-9]{2}`, 5, "值 12 尾", "值 «n:****» 尾", "12"},
		{"Keep 超过原值长度（1 字符→全掩）", `[0-9]{1}`, 5, "值 7 尾", "值 «n:****» 尾", "7"},
		{"Keep 大于原值长度且不越界", `[0-9]{1}`, 100, "值 7 尾", "值 «n:****» 尾", "7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMasker([]Rule{{Name: "n", Re: regexp.MustCompile(tt.re), Keep: tt.keep}})
			if err != nil {
				t.Fatalf("NewMasker err = %v", err)
			}
			got := m.Mask(tt.in)
			if got != tt.want {
				t.Errorf("Mask(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("Mask 输出非法 UTF-8: %q", got)
			}
			// 全掩分支：输出里不得残留原值的任何字符（防回归核心断言）。
			if got == "值 «n:****» 尾" && strings.Contains(got, tt.digits) {
				t.Errorf("全掩输出 %q 仍含原值字符 %q", got, tt.digits)
			}
		})
	}
}

// TestMaskerShortValueFullyMasked 契约（pkg#31）：命中长度不足 Keep*4 时整段
// 全量打码，输出里**不得包含命中的原文**。
//
// 回归背景：旧实现把保留位收缩成 min(Keep, n/2)，命中长度 <= 2*Keep 时首尾
// 两段保留位首尾相接、覆盖整段原文（掩码主体只是插在中间的装饰），原值
// 完整留在输出里——「留 8 藏 1 等于没藏」，掩码形同虚设。
func TestMaskerShortValueFullyMasked(t *testing.T) {
	tests := []struct {
		name  string
		keep  int
		value string
	}{
		{"n == Keep*4-1", 4, "0123456789abcde"}, // 15 = 4*4-1
		{"n == 2*Keep", 4, "01234567"},          // 8 = 2*4
		{"n < 2*Keep", 4, "012"},
		{"n == 1", 1, "0"},
		{"Keep=0", 0, "0123"},
		{"中文短值", 2, "机密"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewMasker([]Rule{{Name: "x", Re: regexp.MustCompile(`\S+`), Keep: tt.keep}})
			if err != nil {
				t.Fatalf("NewMasker err = %v", err)
			}
			got := m.Mask(tt.value)
			if want := "«x:****»"; got != want {
				t.Errorf("Mask(%q) = %q, want %q（短值应整段全掩）", tt.value, got, want)
			}
			// 核心断言：全掩输出不得包含命中的原文。
			if strings.Contains(got, tt.value) {
				t.Errorf("全掩输出 %q 仍含原值 %q", got, tt.value)
			}
			// 更强一档：原值的任何一个字符都不得出现在输出里。
			for _, r := range tt.value {
				if strings.ContainsRune(got, r) {
					t.Errorf("全掩输出 %q 仍含原值字符 %q", got, r)
				}
			}
			if !utf8.ValidString(got) {
				t.Errorf("Mask 输出非法 UTF-8: %q", got)
			}
		})
	}
}

// TestMaskerKeepRatioBoundary 契约（pkg#31）：n == Keep*4 是保留位的下边界——
// 保留首尾各 Keep 个字符，但原值**不得**被完整暴露（中间必须还有被藏住的部分）。
func TestMaskerKeepRatioBoundary(t *testing.T) {
	const keep = 4
	const orig = "0123456789abcdef" // 16 = keep*4

	m, err := NewMasker([]Rule{{Name: "probe", Re: regexp.MustCompile(`[0-9a-f]{15,16}`), Keep: keep}})
	if err != nil {
		t.Fatalf("NewMasker err = %v", err)
	}
	got := m.Mask(orig)
	want := "0123«probe:****»cdef"
	if got != want {
		t.Errorf("Mask(%q) = %q, want %q", orig, got, want)
	}
	if strings.Contains(got, orig) {
		t.Errorf("边界长度输出 %q 完整暴露了原值", got)
	}
	// 藏住的部分不能为空：去掉标签后剩下的字符不足以还原原文。
	kept := got[:keep] + got[len(got)-keep:]
	if kept == orig {
		t.Errorf("边界长度下保留位覆盖了整段原文（%q）", kept)
	}

	// 少一格就全掩。
	short := "0123456789abcde" // 15 = keep*4-1
	gotShort := m.Mask(short)
	if wantShort := "«probe:****»"; gotShort != wantShort {
		t.Errorf("Mask(%q) = %q, want %q", short, gotShort, wantShort)
	}
	if strings.Contains(gotShort, short) {
		t.Errorf("全掩输出 %q 仍含原值 %q", gotShort, short)
	}
}

// TestMaskerMultibyteShortValue 契约（pkg#31）：多字节短值同样整段全掩，
// 不切半个字（输出仍是合法 UTF-8，且不含原值字符）。
func TestMaskerMultibyteShortValue(t *testing.T) {
	m, err := NewMasker([]Rule{{Name: "cn", Re: regexp.MustCompile(`密码[:：](\S+)`), Keep: 2}})
	if err != nil {
		t.Fatalf("NewMasker err = %v", err)
	}
	// 命中值「密码：机密」共 5 个 rune < Keep*4=8 → 全掩。
	got := m.Mask("密码：机密")
	if want := "«cn:****»"; got != want {
		t.Errorf("Mask = %q, want %q", got, want)
	}
	if strings.Contains(got, "机密") {
		t.Errorf("全掩输出 %q 仍含原值", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("Mask 输出非法 UTF-8: %q", got)
	}

	// 够长的多字节值仍按 rune 边界保留首尾（不切半个字）。
	long := m.Mask("密码：超级机密123")
	if want := "密码«cn:****»23"; long != want {
		t.Errorf("Mask = %q, want %q", long, want)
	}
	if !utf8.ValidString(long) {
		t.Errorf("Mask 输出非法 UTF-8: %q", long)
	}
}

// TestMaskerKeepIsCapped 保留位数封顶：原值长度不同（但都 >= Keep*4），掩码
// 输出长度相同（掩码主体恒为 4 个 '*'，保留位数恒为 2*Keep），不泄露原值长度。
func TestMaskerKeepIsCapped(t *testing.T) {
	m, err := NewMasker([]Rule{{Name: "n", Re: regexp.MustCompile(`[0-9]+`), Keep: 2}})
	if err != nil {
		t.Fatalf("NewMasker err = %v", err)
	}
	short := m.Mask("12345678") // 8 = Keep*4
	long := m.Mask("123456789012345678")
	if short != "12«n:****»78" {
		t.Errorf("Mask(8 位) = %q, want %q", short, "12«n:****»78")
	}
	if long != "12«n:****»78" {
		t.Errorf("Mask(18 位) = %q, want %q", long, "12«n:****»78")
	}
	if len([]rune(short)) != len([]rune(long)) {
		t.Errorf("不同长度原值的掩码长度应相同: %d vs %d", len([]rune(short)), len([]rune(long)))
	}
	// 短于 Keep*4 的值走全掩分支：输出固定形态，且不含原值。
	full := m.Mask("1234567")
	if full != "«n:****»" {
		t.Errorf("Mask(7 位) = %q, want %q", full, "«n:****»")
	}
	if strings.Contains(full, "1234567") {
		t.Errorf("全掩输出 %q 仍含原值", full)
	}
}

// TestMaskerRuleOrder 多规则顺序：同长度时先注册者优先；更长匹配优先。
func TestMaskerRuleOrder(t *testing.T) {
	// 同长度匹配 → 先注册的规则胜出。
	same, err := NewMasker([]Rule{
		{Name: "first", Re: regexp.MustCompile(`secret`), Keep: 0},
		{Name: "second", Re: regexp.MustCompile(`secret`), Keep: 0},
	})
	if err != nil {
		t.Fatalf("NewMasker err = %v", err)
	}
	if got := same.Mask("a secret b"); got != "a «first:****» b" {
		t.Errorf("Mask = %q, want %q（同长度先注册优先）", got, "a «first:****» b")
	}

	// 长度不同 → 更长的匹配优先（扫描合并语义）。
	long, err := NewMasker([]Rule{
		{Name: "short", Re: regexp.MustCompile(`key`), Keep: 0},
		{Name: "long", Re: regexp.MustCompile(`keyring`), Keep: 0},
	})
	if err != nil {
		t.Fatalf("NewMasker err = %v", err)
	}
	if got := long.Mask("a keyring b"); got != "a «long:****» b" {
		t.Errorf("Mask = %q, want %q（最长优先）", got, "a «long:****» b")
	}
}

// TestMaskerNoSelfPollution 防自污染：规则 2 的模式能匹配规则 1 输出的
// 形态标签，但输出不得被二次处理。
func TestMaskerNoSelfPollution(t *testing.T) {
	m, err := NewMasker([]Rule{
		{Name: "kv", Re: regexp.MustCompile(`password=([^\s]+)`), Keep: 2},
		// 这条规则匹配的是「掩码标签」本身——顺序替换的实现会二次处理。
		{Name: "label", Re: regexp.MustCompile(`«[^»]*»`), Keep: 0},
	})
	if err != nil {
		t.Fatalf("NewMasker err = %v", err)
	}
	got := m.Mask("password=secret")
	want := "pa«kv:****»et"
	if got != want {
		t.Errorf("Mask = %q, want %q（标签不得被二次处理）", got, want)
	}
	if strings.Contains(got, "label") {
		t.Errorf("Mask = %q 含二次处理痕迹", got)
	}

	// 反证：规则 2 确实能匹配规则 1 输出的标签形态（顺序替换实现会中招）。
	only, err := NewMasker([]Rule{{Name: "label", Re: regexp.MustCompile(`«[^»]*»`), Keep: 0}})
	if err != nil {
		t.Fatalf("NewMasker err = %v", err)
	}
	if l := only.Mask("«kv:****»"); l != "«label:****»" {
		t.Errorf("规则 2 应能匹配标签形态，实际 = %q", l)
	}
}

// TestMaskerMultibyte 多字节字符：保留位按 rune 边界切，不切半个字。
func TestMaskerMultibyte(t *testing.T) {
	m, err := NewMasker([]Rule{{Name: "cn", Re: regexp.MustCompile(`密码[:：](\S+)`), Keep: 2}})
	if err != nil {
		t.Fatalf("NewMasker err = %v", err)
	}
	got := m.Mask("账号密码：超级机密123")
	want := "账号密码«cn:****»23"
	if got != want {
		t.Errorf("Mask = %q, want %q", got, want)
	}
	if !utf8.ValidString(got) {
		t.Errorf("Mask 输出非法 UTF-8: %q", got)
	}

	// 单个中文字符（短值）→ 保留位收缩为 0，全掩。
	m2, err := NewMasker([]Rule{{Name: "cn", Re: regexp.MustCompile(`密$`), Keep: 2}})
	if err != nil {
		t.Fatalf("NewMasker err = %v", err)
	}
	got2 := m2.Mask("机密")
	if want2 := "机«cn:****»"; got2 != want2 {
		t.Errorf("Mask = %q, want %q", got2, want2)
	}
	if !utf8.ValidString(got2) {
		t.Errorf("Mask 输出非法 UTF-8: %q", got2)
	}
}

// TestMaskerEmptyMatchIgnored 零宽匹配被忽略，不会导致扫描不前进。
func TestMaskerEmptyMatchIgnored(t *testing.T) {
	m, err := NewMasker([]Rule{{Name: "star", Re: regexp.MustCompile(`a*`), Keep: 0}})
	if err != nil {
		t.Fatalf("NewMasker err = %v", err)
	}
	if got := m.Mask("bbb"); got != "bbb" {
		t.Errorf("Mask = %q, want %q（零宽匹配应被忽略）", got, "bbb")
	}
}

// TestMaskerNilMasker nil Masker 安全返回原文。
func TestMaskerNilMasker(t *testing.T) {
	var m *Masker
	if got := m.Mask("abc"); got != "abc" {
		t.Errorf("Mask = %q, want %q", got, "abc")
	}
}

// TestMaskerConcurrency 构造后的 Masker 只读，可并发使用（-race 验证）。
func TestMaskerConcurrency(t *testing.T) {
	m, err := NewMasker(maskRules(t))
	if err != nil {
		t.Fatalf("NewMasker err = %v", err)
	}
	const in = "login password=hunter2 via postgres://u:p@host/db"
	const want = "login pa«kv-secret:****»r2 via postgres«url-userinfo:****»host/db"
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if got := m.Mask(in); got != want {
					t.Errorf("并发 Mask = %q, want %q", got, want)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func ExampleNewMasker() {
	rules := []Rule{
		{Name: "kv-secret", Re: regexp.MustCompile(`token=([^\s]+)`), Keep: 2},
	}
	m, err := NewMasker(rules)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(m.Mask("call api with token=abcdef123456"))
	// Output: call api with to«kv-secret:****»56
}
