package stringx

import (
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestNewReplacerErrors(t *testing.T) {
	if _, err := NewReplacer([]string{"a"}, []string{"1", "2"}); err == nil {
		t.Error("patterns/repls 长度不一致应报错")
	}
	if _, err := NewReplacer([]string{"a", ""}, []string{"1", "2"}); err == nil {
		t.Error("空 pattern 应报错")
	}
	if _, err := NewReplacer([]string{"\xcd"}, []string{"1"}); err == nil {
		t.Error("非法 UTF-8 pattern 应报错")
	}
}

func TestNewReplacerNilPatterns(t *testing.T) {
	r, err := NewReplacer(nil, nil)
	if err != nil {
		t.Fatalf("NewReplacer(nil, nil) err = %v, want nil", err)
	}
	if got := r.Replace("hello"); got != "hello" {
		t.Errorf("Replace(no-op) = %q, want %q", got, "hello")
	}
}

func TestReplacerBasic(t *testing.T) {
	r, err := NewReplacer([]string{"bar", "baz"}, []string{"**", "***"})
	if err != nil {
		t.Fatalf("NewReplacer err = %v", err)
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"全部命中", "foo bar baz", "foo ** ***"},
		{"部分命中", "barbar foo", "**** foo"},
		{"无命中原样返回", "hello world", "hello world"},
		{"空文本", "", ""},
		{"词尾边界", "end bar", "end **"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.Replace(tt.in); got != tt.want {
				t.Errorf("Replace(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestReplacerLongestMatch 同一起点命中多个 pattern 时取最长。
func TestReplacerLongestMatch(t *testing.T) {
	r, err := NewReplacer([]string{"ab", "abc"}, []string{"X", "Y"})
	if err != nil {
		t.Fatalf("NewReplacer err = %v", err)
	}
	if got := r.Replace("abc"); got != "Y" {
		t.Errorf("Replace(%q) = %q, want %q（最长匹配）", "abc", got, "Y")
	}
	if got := r.Replace("abd"); got != "Xd" {
		t.Errorf("Replace(%q) = %q, want %q", "abd", got, "Xd")
	}

	r2, err := NewReplacer([]string{"abcd", "b"}, []string{"W", "X"})
	if err != nil {
		t.Fatalf("NewReplacer err = %v", err)
	}
	if got := r2.Replace("xabcd"); got != "xW" {
		t.Errorf("Replace(%q) = %q, want %q", "xabcd", got, "xW")
	}
	if got := r2.Replace("bxab"); got != "XxaX" {
		t.Errorf("Replace(%q) = %q, want %q", "bxab", got, "XxaX")
	}
}

// TestReplacerEmptyReplacement 空替换串表示删除该词。
func TestReplacerEmptyReplacement(t *testing.T) {
	r, err := NewReplacer([]string{"foo"}, []string{""})
	if err != nil {
		t.Fatalf("NewReplacer err = %v", err)
	}
	if got := r.Replace("afoob"); got != "ab" {
		t.Errorf("Replace(%q) = %q, want %q", "afoob", got, "ab")
	}
}

// TestReplacerDuplicatePattern 同名 pattern 后写覆盖先写。
func TestReplacerDuplicatePattern(t *testing.T) {
	r, err := NewReplacer([]string{"a", "a"}, []string{"1", "2"})
	if err != nil {
		t.Fatalf("NewReplacer err = %v", err)
	}
	if got := r.Replace("aa"); got != "22" {
		t.Errorf("Replace(%q) = %q, want %q（后写覆盖）", "aa", got, "22")
	}
}

// TestReplacerMultibyte 中文等多字节字符按 rune 匹配，不撕裂 UTF-8。
func TestReplacerMultibyte(t *testing.T) {
	r, err := NewMask([]string{"赌博", "吸毒"}, '*')
	if err != nil {
		t.Fatalf("NewMask err = %v", err)
	}
	if got := r.Replace("禁止赌博和吸毒"); got != "禁止**和**" {
		t.Errorf("Replace = %q, want %q", got, "禁止**和**")
	}
	// 遮罩按词内 rune 数逐字替换
	r2, err := NewMask([]string{"敏感词"}, '#')
	if err != nil {
		t.Fatalf("NewMask err = %v", err)
	}
	if got := r2.Replace("这是敏感词内容"); got != "这是###内容" {
		t.Errorf("Replace = %q, want %q", got, "这是###内容")
	}
}

// TestReplacerPreservesInvalidBytes 文本含非法 UTF-8 字节时原字节保留，
// 不被改写为 U+FFFD（utf8.ValidString 语义）。
func TestReplacerPreservesInvalidBytes(t *testing.T) {
	r, err := NewReplacer([]string{"ab"}, []string{"X"})
	if err != nil {
		t.Fatalf("NewReplacer err = %v", err)
	}
	in := "a\xcd\xfe ab"
	if got := r.Replace(in); got != "a\xcd\xfe X" {
		t.Errorf("Replace(%q) = %q, want %q（非法字节原样保留）", in, got, "a\xcd\xfe X")
	}
}

func TestNewMaskErrors(t *testing.T) {
	if _, err := NewMask([]string{""}, '*'); err == nil {
		t.Error("NewMask 空 word 应报错")
	}
}

// TestReplacerConcurrency 构造后的 Replacer 只读，可并发使用（-race 验证）。
func TestReplacerConcurrency(t *testing.T) {
	r, err := NewMask([]string{"bad"}, '*')
	if err != nil {
		t.Fatalf("NewMask err = %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if got := r.Replace("a bad word"); got != "a *** word" {
					t.Errorf("并发 Replace = %q", got)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// FuzzReplacerMask 不含掩码字符的合法 UTF-8 词表，替换结果中不应再出现任何词。
// 输入限定合法 UTF-8：字节级 Contains 的不出现断言只在合法 UTF-8 下成立
// （合法子串必然 rune 对齐）；非法字节路径已由 TestReplacerPreservesInvalidBytes 覆盖。
func FuzzReplacerMask(f *testing.F) {
	f.Add("hello bad world", "bad|worse")
	f.Add("badworsebad", "bad|worse")
	f.Fuzz(func(t *testing.T, text, wordsJoined string) {
		if !utf8.ValidString(text) {
			t.Skip()
		}
		words := strings.FieldsFunc(wordsJoined, func(r rune) bool { return r == '|' })
		var clean []string
		for _, w := range words {
			if w != "" && utf8.ValidString(w) && !strings.ContainsRune(w, '*') && !strings.ContainsRune(w, '|') {
				clean = append(clean, w)
			}
		}
		r, err := NewMask(clean, '*')
		if err != nil {
			t.Fatalf("NewMask err = %v", err)
		}
		got := r.Replace(text)
		for _, w := range clean {
			if strings.Contains(got, w) {
				t.Fatalf("Replace(%q) = %q 仍含词 %q", text, got, w)
			}
		}
	})
}
