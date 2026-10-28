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
// 单元格少于表头列数时补空串，多于表头列数时多出来的部分被丢弃——表格的
// 形状由表头决定，不能被一行数据撑歪。
func (t *Table) Append(cells ...string) {
	if t == nil {
		return
	}
	row := make([]string, len(t.headers))
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
func (t *Table) Render(w io.Writer) {
	if t == nil || w == nil {
		return
	}
	cols := len(t.headers)
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

// displayWidth 估算一段文本在终端里占几列。
//
// 中文、日文、韩文是全角，占两列；组合记号（重音等）不占列；其余按一列算。
// 这不是完整的 East Asian Width 表，但覆盖了命令行输出里实际会遇到的字符。
//
// 为什么不能直接用 len()：len 数的是字节，一个中文字符是 3 字节，按它算
// 列宽会让中文列比实际宽出一倍——表格的右边就会参差不齐。
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// runeWidth 返回一个字符占几列。
func runeWidth(r rune) int {
	switch {
	case r >= 0x0300 && r <= 0x036F:
		// 组合记号：附着在前一个字符上，不占列。
		return 0
	case r >= 0x1100 && r <= 0x115F:
		// 谚文字母。
		return 2
	case r >= 0x2E80 && r <= 0x303E:
		// 康熙部首、中日韩符号与标点。
		return 2
	case r >= 0x3041 && r <= 0x33FF:
		// 平假名、片假名、注音、CJK 兼容。
		return 2
	case r >= 0x3400 && r <= 0x4DBF:
		// CJK 扩展 A。
		return 2
	case r >= 0x4E00 && r <= 0x9FFF:
		// CJK 统一表意文字。
		return 2
	case r >= 0xA000 && r <= 0xA4CF:
		// 彝文。
		return 2
	case r >= 0xAC00 && r <= 0xD7A3:
		// 谚文音节。
		return 2
	case r >= 0xF900 && r <= 0xFAFF:
		// CJK 兼容表意文字。
		return 2
	case r >= 0xFE10 && r <= 0xFE6F:
		// 竖排标点与兼容形式。
		return 2
	case r >= 0xFF00 && r <= 0xFF60:
		// 全角形式。
		return 2
	case r >= 0xFFE0 && r <= 0xFFE6:
		// 全角符号。
		return 2
	case r >= 0x1F300 && r <= 0x1F64F:
		// 表情符号。
		return 2
	case r >= 0x20000 && r <= 0x3FFFD:
		// CJK 扩展 B 及以后。
		return 2
	}
	return 1
}
