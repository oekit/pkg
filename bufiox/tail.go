package bufiox

import (
	"bufio"
	"errors"
	"io"
	"math"
	"strings"
)

// TailLines 的参数边界。
var (
	// ErrBadBudget budget <= 0（尾部窗口预算必须为正）。
	ErrBadBudget = errors.New("bufiox: budget 必须为正数")
)

const (
	// tailMaxLineSize 单行扫描上限 1MB：CI 日志有超长 base64 行，
	// bufio.Scanner 默认 64KB 会把它们当扫描错误。超过该长度的行返回
	// bufio.ErrTooLong（原样透出——截半行没有意义）。
	tailMaxLineSize = 1 << 20

	// tailMaxLineFactor 行数软上限系数：保留的行数最多 budget×factor。
	// 字节预算对极小行（空行/单字符行）不构成约束（内容字节数为 0 也
	// 占一个 string 头），行数上限保证内存不随日志总长线性增长。
	tailMaxLineFactor = 4
)

// TailLines 读流并保留**尾部 budget 字节**的内容（#198），返回
// (tail, truncated, err)：tail 是行粒度对齐的尾部文本（截断点落在行边界，
// 不会把半行残根当尾部）；truncated 报告头部是否被丢弃——LLM 归因场景
// 用它给提示词标注「这是尾部，头部已省略」（比静默截断诚实）；读错误经
// err 原样透传（err 非 nil 时其余返回值不具意义）。
//
// 语义（CI 失败日志尾部归因的两代实现固化下来的口径）：
//
//   - **行粒度**扫描（bufio.Scanner，单行上限 1MB）：字节粒度截断会把
//     半行残根当尾部，首行不可读；
//   - 超预算从**头部按行丢弃**：窗口内保留行的内容字节总数 <= budget
//     （分隔符不计入），且行数 <= budget×4（软上限，防极小行无限堆积）；
//   - 单行自身超预算时整行保留（行不可分割——截半行等于不可读），
//     该行到达即视头部已丢弃；窗口内存上界为 budget + 1MB（单行上限）；
//   - 行界统一按 \n 重组（\r\n 归一为 \n，尾部不带换行符）。
//
// 用法：
//
//	tail, truncated, err := bufiox.TailLines(resp.Body, 16<<10)
//	if err != nil { ... }
//	if truncated { prompt += "\n(日志头部已省略，以下为尾部)" }
func TailLines(r io.Reader, budget int) (tail string, truncated bool, err error) {
	if budget <= 0 {
		return "", false, ErrBadBudget
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), tailMaxLineSize)

	// 行数软上限：budget×factor，乘法**前**判上界（#408，与 pagination 的
	// 既有做法同口径——int64 自身的乘法在 budget > 2^61 时一样回绕成负值，
	// 回绕后的 maxLines ≤ 0 让丢头循环恒真 ⇒ 尾窗塌成 1 行）。预算大到乘积
	// 超出 int64 时软上限退位为「永不生效」（budget×4 已不可表示，任何行数
	// 都满足「行数 <= budget×4」），饱和到 MaxInt64 即可——行数由字节预算
	// 与输入总长自然约束。
	maxLines := int64(math.MaxInt64)
	if b := int64(budget); b <= math.MaxInt64/tailMaxLineFactor {
		maxLines = b * tailMaxLineFactor
	}
	var (
		lines     []string
		contentSz int // 窗口内行的内容字节数（不含分隔符）
	)
	for sc.Scan() {
		line := sc.Text()
		lines = append(lines, line)
		contentSz += len(line)
		// 头部丢弃循环：字节预算或行数软上限任一超限即从头部按行丢弃；
		// 至少保留最新一行（单行超预算整行保留——行不可分割；它若是
		// 首行则头部无内容可丢，truncated 保持 false）。
		for len(lines) > 1 && (contentSz > budget || int64(len(lines)) > maxLines) {
			contentSz -= len(lines[0])
			lines = lines[1:]
			truncated = true
		}
	}
	if scanErr := sc.Err(); scanErr != nil {
		return "", false, scanErr
	}
	return strings.Join(lines, "\n"), truncated, nil
}
