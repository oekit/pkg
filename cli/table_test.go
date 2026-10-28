package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestTable_RendersAlignedColumns 断言列宽自适应。
func TestTable_RendersAlignedColumns(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("NAME", "RISK")
	tbl.Append("jms_a", "read")
	tbl.Append("jms_a_much_longer_name", "read")
	tbl.Render(&buf)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("渲染了 %d 行，期望 3 行，实际 =\n%s", len(lines), buf.String())
	}
	// 表头行单独看：第二列必须在表头名之后。
	if !strings.Contains(lines[0], "RISK") {
		t.Errorf("表头行缺少第二列，实际 = %q", lines[0])
	}
	// 两行数据的第二列起点必须一致——这是「对齐」的全部含义。
	first := strings.Index(lines[1], "read")
	second := strings.Index(lines[2], "read")
	if first != second {
		t.Errorf("第二列起点不一致：%d vs %d，实际 =\n%s", first, second, buf.String())
	}
}

// TestTable_ChineseCellAlignment 断言中文单元格的列宽算得对。
//
// 这一条是本包存在的理由：按字节算列宽，中文列会比实际宽出一倍。
func TestTable_ChineseCellAlignment(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("名称", "值")
	tbl.Append("读", "ok")
	tbl.Append("检查连接性", "ok")
	tbl.Render(&buf)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("渲染了 %d 行，期望 3 行", len(lines))
	}
	// 第二列的显示起点：表头的「值」与数据的「ok」必须在同一显示列。
	// 用字节偏移比较是不对的——中文字符的字节数与显示列数不是一回事。
	// 列宽取第一列最长的内容（「检查连接性」= 10 显示列），不是表头的宽度。
	want := displayWidth("检查连接性") + 2
	headerAt := displayWidth(lines[0][:strings.Index(lines[0], "值")])
	if headerAt != want {
		t.Errorf("表头第二列显示起点 = %d，期望 %d，实际 = %q", headerAt, want, lines[0])
	}
	for i := 1; i < len(lines); i++ {
		at := displayWidth(lines[i][:strings.Index(lines[i], "ok")])
		if at != want {
			t.Errorf("第 %d 行第二列显示起点 = %d，期望 %d，实际 = %q", i, at, want, lines[i])
		}
	}
}

// TestTable_ExtraCellsDropped 断言多出来的单元格被丢弃——形状由表头决定。
func TestTable_ExtraCellsDropped(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("A", "B")
	tbl.Append("1", "2", "3")
	tbl.Render(&buf)
	out := buf.String()
	if strings.Contains(out, "3") {
		t.Errorf("多出的单元格不应被渲染，实际 = %q", out)
	}
}

// TestTable_MissingCellsPadded 断言缺列补空串，不塌行。
func TestTable_MissingCellsPadded(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable("A", "B")
	tbl.Append("1")
	tbl.Render(&buf)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("渲染了 %d 行，期望 2 行", len(lines))
	}
	if strings.TrimSpace(lines[1]) != "1" {
		t.Errorf("缺列的行 = %q，期望 %q", lines[1], "1")
	}
}

// TestDisplayWidth 断言全角与组合记号的宽度。
func TestDisplayWidth(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"ASCII", "abc", 3},
		{"中文", "检查", 4},
		{"全角标点", "（）", 4},
		{"混排", "ok 中文", 7},
		{"组合记号", "e\u0301", 1},
		{"空串", "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := displayWidth(c.in); got != c.want {
				t.Errorf("displayWidth(%q) = %d，期望 %d", c.in, got, c.want)
			}
		})
	}
}

// TestTable_NoHeader 断言无表头时形状由第一行决定。
//
// 有些输出（工具目录）本来就没有列名，硬塞一个表头反而改变了它的形状。
func TestTable_NoHeader(t *testing.T) {
	var buf bytes.Buffer
	tbl := NewTable()
	tbl.Append("jms_a", "read", "set confirm=true")
	tbl.Append("jms_b", "write", "")
	tbl.Render(&buf)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("渲染了 %d 行，期望 2 行，实际 =\n%s", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "jms_a") || !strings.Contains(lines[1], "jms_b") {
		t.Errorf("数据行丢失，实际 =\n%s", buf.String())
	}
}
