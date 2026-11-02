package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestDisplayWidth_EmojiStatus 断言 emoji 状态符号的列宽（#162）。
//
// 这一条是 #162 的根因：⚠️（U+26A0 + U+FE0F）、✅（U+2705）、❌（U+274C）在
// 终端里都是两列，按一列算 ⇒ 状态列整体右移、表格错位。
//
// ⚠（U+26A0）单写是**文本呈现**，一列；跟上 VS16（U+FE0F）才是 emoji 呈现，
// 两列。这一对是「变体选择符必须按序列看」的最小样本。
func TestDisplayWidth_EmojiStatus(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"⚠ 文本呈现", "⚠", 1},
		{"⚠️ emoji 呈现", "⚠️", 2},
		{"✅", "✅", 2},
		{"❌", "❌", 2},
		{"✔ 文本呈现", "✔", 1},
		{"✔️ emoji 呈现", "✔️", 2},
		{"✓ 不是 emoji 字符", "✓", 1},
		{"✓️ 不是 emoji 字符，VS16 不提升", "✓️", 1},
		{"© 文本呈现", "©", 1},
		{"©️ emoji 呈现", "©️", 2},
		{"A 不在 emoji 变体表里", "A️", 1},
		{"状态符号后跟文字", "⚠️ warn", 7},
		{"中文后跟 emoji", "检查 ✅", 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := displayWidth(c.in); got != c.want {
				t.Errorf("displayWidth(%q) = %d，期望 %d", c.in, got, c.want)
			}
		})
	}
}

// TestDisplayWidth_VariationSelector 断言变体选择符本身不占列。
//
// VS16（U+FE0F）请求 emoji 呈现，把窄的基字符提升成两列；VS15（U+FE0E）请求
// 文本呈现，不改基字符的宽度（多数终端也没有对应的窄字形，降级反而会算错）。
func TestDisplayWidth_VariationSelector(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"VS16 自己不占列", "️", 0},
		{"VS15 自己不占列", "︎", 0},
		{"VS15 不把宽字符降级", "✅︎", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := displayWidth(c.in); got != c.want {
				t.Errorf("displayWidth(%q) = %d，期望 %d", c.in, got, c.want)
			}
		})
	}
}

// TestDisplayWidth_EmojiCluster 断言 emoji 序列按「一个字形」算宽度。
//
// 肤色修饰符、键帽、标签都是修饰基字符的，不额外占列；ZWJ 把两个 emoji 连成
// 一个字形；两个区域指示符配成一个旗帜，也是一个字形。
func TestDisplayWidth_EmojiCluster(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"肤色修饰符", "🫱🏻", 2},
		{"肤色 + ZWJ 链", "🫱🏻‍🫲🏼", 2},
		{"家庭 ZWJ 序列", "👨‍👩‍👧‍👦", 2},
		{"旗帜", "🇨🇳", 2},
		{"键帽", "1️⃣", 2},
		{"❤️ 文本基 + VS16", "❤️", 2},
		{"❤ 文本基单写", "❤", 1},
		{"ZWJ 落在非 emoji 之间", "a‍b", 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := displayWidth(c.in); got != c.want {
				t.Errorf("displayWidth(%q) = %d，期望 %d", c.in, got, c.want)
			}
		})
	}
}

// TestDisplayWidth_ZeroWidth 断言零宽字符不占列。
func TestDisplayWidth_ZeroWidth(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"组合记号", "e\u0301", 1},
		{"控制符", "\t", 0},
		{"行分隔符", " ", 0},
		{"零宽连接符落单", "‍", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := displayWidth(c.in); got != c.want {
				t.Errorf("displayWidth(%q) = %d，期望 %d", c.in, got, c.want)
			}
		})
	}
}

// TestTable_EmojiStatusAlignment 断言含 emoji 状态符号的表格不错位。
//
// 这是 #162 的可复现形态：状态列在中间，右边还有一列。状态符号按一列算时，
// 状态列的补白少一列 ⇒ 后面的列整体左移一列，看着就是「表格歪了」。
func TestTable_EmojiStatusAlignment(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("NAME", "STATUS", "NOTE")
	tbl.Append("a", "⚠️", "warn")
	tbl.Append("b", "✅", "ok")
	tbl.Append("c", "❌", "fail")
	tbl.Render(&buf)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("渲染了 %d 行，期望 4 行，实际 =\n%s", len(lines), buf.String())
	}
	// 最后一列的显示起点必须逐行一致——这是「对齐」的全部含义。
	// 用字节偏移比是不对的：emoji 与中文的字节数都不是显示列数。
	want := -1
	for i, line := range lines {
		at := strings.Index(line, "NOTE")
		if i > 0 {
			at = strings.Index(line, "warn")
			if at < 0 {
				at = strings.Index(line, "ok")
			}
			if at < 0 {
				at = strings.Index(line, "fail")
			}
		}
		got := displayWidth(line[:at])
		if want < 0 {
			want = got
			continue
		}
		if got != want {
			t.Errorf("第 %d 行最后一列的显示起点 = %d，期望 %d，实际 = %q", i, got, want, line)
		}
	}
	// 状态列自身必须占两列——否则补白少一列，后面那列就左移了。
	if got := displayWidth("⚠️"); got != 2 {
		t.Errorf("⚠️ 的显示宽度 = %d，期望 2", got)
	}
}
