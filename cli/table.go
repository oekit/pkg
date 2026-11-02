package cli

// table.go 是列对齐的文本表格。
//
// 为什么要有它：CLI 的表格输出是最容易各写各的形状——列宽写死一份、换一
// 份就歪、终端宽一点窄一点就对不上。把「列宽自适应」收进包里，各程序只
// 交数据，形状就一致了。

import (
	"io"
	"strings"
)

// Table 是列对齐的文本表格。
//
// 列宽自适应：每列取该列所有单元格（含表头）的最大显示宽度，列与列之间
// 隔两个空格。最后一列不补尾随空格。
//
// 单元格里的换行原样写出（表格不折行）——折行会让「一格一行」的读法失效，
// 要折行请自己把内容拆成多行。
type Table struct {
	headers []string
	rows    [][]string
}

// NewTable 建一个带表头的表格。
//
// 表头可以不给（传空即可），此时只有数据行。
func NewTable(headers ...string) *Table {
	return &Table{headers: headers}
}

// Append 追加一行。
//
// 单元格少于列数时补空串，多于列数时多出来的部分被丢弃——表格的形状由
// 表头决定，不能被一行数据撑歪。没有表头时，形状由第一行决定。
func (t *Table) Append(cells ...string) {
	if t == nil {
		return
	}
	cols := len(t.headers)
	if cols == 0 {
		// 无表头：第一行定形状，之后的行按它截断。
		if len(t.rows) == 0 {
			cols = len(cells)
		} else {
			cols = len(t.rows[0])
		}
	}
	row := make([]string, cols)
	for i := range row {
		if i < len(cells) {
			row[i] = cells[i]
		}
	}
	t.rows = append(t.rows, row)
}

// Render 把表格写到 w。
//
// 表头与数据行之间不加分隔线：CLI 表格通常很短，一条横线会让人误以为
// 下面那段是另一个区段。
//
// 没有表头时，形状由第一行决定——有些输出（如工具目录）本来就没有列名，
// 硬塞一个表头反而改变了它的形状。
func (t *Table) Render(w io.Writer) {
	if t == nil || w == nil {
		return
	}
	cols := len(t.headers)
	if cols == 0 && len(t.rows) > 0 {
		cols = len(t.rows[0])
	}
	if cols == 0 {
		return
	}
	widths := make([]int, cols)
	for i, h := range t.headers {
		widths[i] = displayWidth(h)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if w := displayWidth(cell); w > widths[i] {
				widths[i] = w
			}
		}
	}
	if len(t.headers) > 0 {
		writeRow(w, t.headers, widths)
	}
	for _, row := range t.rows {
		writeRow(w, row, widths)
	}
}

// writeRow 写一行：按列宽左对齐，最后一列不补尾随空格。
func writeRow(w io.Writer, cells []string, widths []int) {
	var b strings.Builder
	for i, cell := range cells {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(cell)
		if i < len(cells)-1 {
			b.WriteString(strings.Repeat(" ", widths[i]-displayWidth(cell)))
		}
	}
	b.WriteString("\n")
	_, _ = io.WriteString(w, b.String())
}

// runeWidth 与 displayWidth 见 width.go——终端列宽的判定与 Unicode 表都放在
// 那儿，表格本身只管排版。
