package stringx

import (
	"strings"
	"testing"
	"unicode"

	"github.com/oekit/pkg/slicex"
)

func TestContains(t *testing.T) {
	tests := []struct {
		name string
		list []string
		str  string
		want bool
	}{
		{"found", []string{"a", "b", "c"}, "b", true},
		{"not found", []string{"a", "b", "c"}, "d", false},
		{"empty list", []string{}, "a", false},
		{"nil list", nil, "a", false},
		{"contains empty string", []string{"", "a"}, "", true},
		{"spaces and semicolon", []string{"a b", "c;d"}, "c;d", true},
		{"single quote", []string{"it's"}, "it's", true},
	}
	// #242：实现转发 slicex.Contains[string] 后，逐项同时钉住两函数一致
	//（语义单源在 slicex，本包不再有第二份循环体）。
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Contains(tt.list, tt.str); got != tt.want {
				t.Errorf("Contains() = %v, want %v", got, tt.want)
			}
			if got := slicex.Contains(tt.list, tt.str); got != tt.want {
				t.Errorf("slicex.Contains() = %v, want %v（两函数必须一致）", got, tt.want)
			}
		})
	}
}

// TestFilter 钉住 #173 语义对齐：Filter=保留命中（与 slicex.Filter 同向）。
// 历史行为（剔除命中）迁移至 DeleteFunc。
func TestFilter(t *testing.T) {
	// Filter 保留满足条件的字符（与 slicex.Filter / 主流 filter 语义一致）
	result := Filter("abc123def456", func(r rune) bool {
		return r >= '0' && r <= '9'
	})
	if result != "123456" {
		t.Errorf("Filter() = %v, want %v", result, "123456")
	}
}

// TestKeepFunc 对偶入口：KeepFunc=保留命中，与 Filter 同语义（方向在调用点自解释）。
func TestKeepFunc(t *testing.T) {
	pred := func(r rune) bool { return r >= '0' && r <= '9' }
	result := KeepFunc("abc123def456", pred)
	if result != "123456" {
		t.Errorf("KeepFunc() = %v, want %v", result, "123456")
	}
	if result != Filter("abc123def456", pred) {
		t.Errorf("KeepFunc 应与 Filter 同语义，got %v", result)
	}
}

// TestDeleteFunc 对偶入口：DeleteFunc=剔除命中（Filter v0.32 及以前的行为，
// #173 语义对齐后迁移至此）。
func TestDeleteFunc(t *testing.T) {
	result := DeleteFunc("abc123def456", func(r rune) bool {
		return r >= '0' && r <= '9'
	})
	if result != "abcdef" {
		t.Errorf("DeleteFunc() = %v, want %v", result, "abcdef")
	}
}

// TestDeleteFunc_ControlChars 钉住 #173 实测事故场景（domain-manage echoSafe）：
// 404 文案回显路径参数的净化——剔除控制字符（该回显面还接收 %00 类输入）。
// 方向自解释后不再写反：DeleteFunc 剔除命中；Filter=保留命中，剔除须取反谓词。
func TestDeleteFunc_ControlChars(t *testing.T) {
	raw := "ok\x00\x07\r\n\u007f!" // 正常字符夹控制字符
	want := "ok!"
	if got := DeleteFunc(raw, unicode.IsControl); got != want {
		t.Errorf("DeleteFunc(control) = %q, want %q", got, want)
	}
	if got := Filter(raw, func(r rune) bool { return !unicode.IsControl(r) }); got != want {
		t.Errorf("Filter(!control) = %q, want %q", got, want)
	}
}

// TestFilter_AlignsWithSlicex（#173）：同库 Filter 语义一致——stringx.Filter
// 与 slicex.Filter 都是「保留命中」（修复前一个删一个留、同名反方向，写反零报错）。
func TestFilter_AlignsWithSlicex(t *testing.T) {
	pred := func(r rune) bool { return r >= '0' && r <= '9' }
	s := "abc123def456"
	want := "123456"

	if got := Filter(s, pred); got != want {
		t.Fatalf("stringx.Filter = %q, want %q", got, want)
	}
	if got := string(slicex.Filter([]rune(s), pred)); got != want {
		t.Fatalf("slicex.Filter = %q, want %q（应与 stringx.Filter 同向）", got, want)
	}
}

func TestFirstN(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		n        int
		ellipsis []string
		want     string
	}{
		{"short enough", "hello", 10, nil, "hello"},
		{"with ellipsis", "hello world", 5, []string{"..."}, "hello..."},
		{"exact", "hello", 5, nil, "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FirstN(tt.s, tt.n, tt.ellipsis...); got != tt.want {
				t.Errorf("FirstN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLastN(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		n        int
		ellipsis []string
		want     string
	}{
		{"short enough", "hello", 10, nil, "hello"},
		{"exact", "hello", 5, nil, "hello"},
		{"keep tail", "hello world", 5, nil, "world"},
		{"with ellipsis", "hello world", 5, []string{"..."}, "...world"},
		{"no ellipsis when untruncated", "hello", 5, []string{"..."}, "hello"},
		{"multibyte keep tail", "世界你好", 2, nil, "你好"},
		{"multibyte with ellipsis", "世界你好", 2, []string{"…"}, "…你好"},
		{"multibyte boundary", "你好world", 5, nil, "world"},
		{"empty", "", 3, nil, ""},
		{"zero keeps nothing", "hello", 0, nil, ""},
		{"zero with ellipsis", "hello", 0, []string{"..."}, "..."},
		{"negative keeps nothing", "hello", -1, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LastN(tt.s, tt.n, tt.ellipsis...); got != tt.want {
				t.Errorf("LastN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasEmpty(t *testing.T) {
	if !HasEmpty("a", "", "c") {
		t.Error("HasEmpty() should return true")
	}
	if HasEmpty("a", "b", "c") {
		t.Error("HasEmpty() should return false")
	}
}

func TestNotEmpty(t *testing.T) {
	if !NotEmpty("a", "b", "c") {
		t.Error("NotEmpty() should return true")
	}
	if NotEmpty("a", "", "c") {
		t.Error("NotEmpty() should return false")
	}
}

func TestJoin(t *testing.T) {
	result := Join(',', "a", "b", "c")
	if result != "a,b,c" {
		t.Errorf("Join() = %v, want %v", result, "a,b,c")
	}
}

func TestRemove(t *testing.T) {
	result := Remove([]string{"a", "b", "c", "b"}, "b")
	if len(result) != 2 || result[0] != "a" || result[1] != "c" {
		t.Errorf("Remove() = %v, want [a c]", result)
	}
}

func TestReverse(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want string
	}{
		{"ascii", "hello", "olleh"},
		{"unicode", "你好", "好你"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Reverse(tt.s); got != tt.want {
				t.Errorf("Reverse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSubstr(t *testing.T) {
	result, err := Substr("hello world", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello" {
		t.Errorf("Substr() = %v, want %v", result, "hello")
	}
}

func TestTakeOne(t *testing.T) {
	if got := TakeOne("a", "b"); got != "a" {
		t.Errorf("TakeOne() = %v, want a", got)
	}
	if got := TakeOne("", "b"); got != "b" {
		t.Errorf("TakeOne() = %v, want b", got)
	}
}

func TestToCamelCase(t *testing.T) {
	if got := ToCamelCase("Hello"); got != "hello" {
		t.Errorf("ToCamelCase() = %v, want hello", got)
	}
}

func TestUnion(t *testing.T) {
	result := Union([]string{"a", "b"}, []string{"b", "c"})
	if len(result) != 3 {
		t.Errorf("Union() length = %v, want 3", len(result))
	}
}

func TestJoinWith(t *testing.T) {
	result := JoinWith([]string{"a", "b", "c"}, "-")
	if result != "a-b-c" {
		t.Errorf("JoinWith() = %v, want %v", result, "a-b-c")
	}
}

func TestSplitWith(t *testing.T) {
	result := SplitWith("a,b;c", []string{",", ";"})
	if len(result) != 3 {
		t.Errorf("SplitWith() length = %v, want 3", len(result))
	}
}

func TestSplitByComma(t *testing.T) {
	result := SplitByComma("a,b,c")
	if len(result) != 3 {
		t.Errorf("SplitByComma() length = %v, want 3", len(result))
	}
}

func TestJoinByComma(t *testing.T) {
	result := JoinByComma([]string{"a", "b", "c"})
	if result != "a,b,c" {
		t.Errorf("JoinByComma() = %v, want %v", result, "a,b,c")
	}
}

// TestPluralize_NegativeCount 回归：手写 itoa 对负数返回空串，
// Pluralize(-3, "file") 曾输出 " files"。
func TestPluralize_NegativeCount(t *testing.T) {
	if got := Pluralize(-3, "file"); got != "-3 files" {
		t.Errorf("Pluralize(-3, file) = %q, want %q", got, "-3 files")
	}
}

// TestFirstN_Negative_FullTruncate 钉住 #183：n<0 对齐 Trunc 家族「非法上限=
// 全截断」口径——此前静默返回整串（截断护栏失效，与 LastN 自认的怪癖相反）。
func TestFirstN_Negative_FullTruncate(t *testing.T) {
	if got := FirstN("hello", -1); got != "" {
		t.Fatalf("FirstN(hello,-1) = %q, want \"\"", got)
	}
	if got := FirstN("hello", -1, "…"); got != "…" {
		t.Fatalf("FirstN(hello,-1,…) = %q, want \"…\"", got)
	}
	// n>=0 行为不变
	if got := FirstN("hello", 3); got != "hel" {
		t.Fatalf("FirstN(hello,3) = %q", got)
	}
}

// TestUnion_OrderStable 钉住 #187：Union 保序（先 first 后 second）——
// 此前 map 迭代构造，每次调用顺序不同。
func TestUnion_OrderStable(t *testing.T) {
	first := []string{"b", "a", "c"}
	second := []string{"a", "d"}
	want := []string{"b", "a", "c", "d"}
	for i := 0; i < 20; i++ {
		got := Union(first, second)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("run %d: got %v want %v（顺序应稳定）", i, got, want)
		}
	}
}
