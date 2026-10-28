package cli

// report.go 是自检报告（doctor 式清单）的渲染原语。
//
// 为什么要有它：doctor 这类输出几乎每个程序都要写一遍，而每个程序都写得
// 不太一样——状态标记三种宽度、检查项名称列宽写死一份、修复建议单独缩进
// 一行但缩进量和名称列对不上。收进包里之后，各程序只交「检查项」，形状
// 就一致了，而「哪些算坏了」仍然是调用方自己的判断。

import (
	"fmt"
	"io"
	"strings"
)

// 检查项的状态。取值固定，Report 之外的任何判断都按这三个读。
const (
	// StatusOK 通过。
	StatusOK = "ok"
	// StatusWarn 有问题但还能用——需要人看一眼，不算坏。
	StatusWarn = "warn"
	// StatusFail 坏了。
	StatusFail = "fail"
)

// ReportItem 是一个检查项。
type ReportItem struct {
	// Status 状态：StatusOK / StatusWarn / StatusFail。别的值按未知渲染。
	Status string
	// Name 检查项的名称。
	Name string
	// Detail 一句话说明这一项查出了什么。
	Detail string
	// Fix 修复建议。可为空——为空就不写这一行。
	Fix string
}

// Report 是自检报告。
//
// 它只负责渲染，不负责判断：哪些检查项算失败由调用方通过 Failures()/
// Warnings() 自己看，退出码也由调用方定。报告是给人看的，退出码是给脚本
// 看的，两者的口径不必相同。
type Report struct {
	title       string
	header      []string
	items       []ReportItem
	conclusions []string
}

// NewReport 建一份报告。title 是第一行（通常是「<程序名> doctor」）。
func NewReport(title string) *Report {
	return &Report{title: title}
}

// SetHeader 设置头部信息行（键值对已由调用方格式化好）。
//
// 头部与检查项之间空一行——它们是两类东西：上面是「这是什么环境」，下面
// 是「查出来什么」。
func (r *Report) SetHeader(lines ...string) *Report {
	if r == nil {
		return r
	}
	r.header = append(r.header[:0], lines...)
	return r
}

// Add 追加一个检查项。
func (r *Report) Add(status, name, detail string) *Report {
	return r.AddFix(status, name, detail, "")
}

// AddFix 追加一个检查项，并带一条修复建议。
//
// 修复建议单独一行，缩进到名称列的起点——它讲的是这一项怎么修，挂在这一
// 项下面才读得懂。
func (r *Report) AddFix(status, name, detail, fix string) *Report {
	if r == nil {
		return r
	}
	r.items = append(r.items, ReportItem{Status: status, Name: name, Detail: detail, Fix: fix})
	return r
}

// AddConclusion 追加一行结论（写在检查项之后、结果行之前）。
//
// 它承载的是「检查项合起来说明什么」——判定、建议、范围一类的文字。
func (r *Report) AddConclusion(line string) *Report {
	if r == nil {
		return r
	}
	r.conclusions = append(r.conclusions, line)
	return r
}

// Items 返回检查项（副本）。
func (r *Report) Items() []ReportItem {
	if r == nil {
		return nil
	}
	out := make([]ReportItem, len(r.items))
	copy(out, r.items)
	return out
}

// Failures 返回失败（StatusFail）的检查项个数。
func (r *Report) Failures() int { return r.count(StatusFail) }

// Warnings 返回警告（StatusWarn）的检查项个数。
func (r *Report) Warnings() int { return r.count(StatusWarn) }

// count 统计某种状态的检查项个数。
func (r *Report) count(status string) int {
	if r == nil {
		return 0
	}
	n := 0
	for _, it := range r.items {
		if it.Status == status {
			n++
		}
	}
	return n
}

// Status 返回整份报告的状态：失败压过警告，两者都没有才是 ok。
func (r *Report) Status() string {
	switch {
	case r.Failures() > 0:
		return StatusFail
	case r.Warnings() > 0:
		return StatusWarn
	default:
		return StatusOK
	}
}

// Render 把报告写到 w。
func (r *Report) Render(w io.Writer) {
	if r == nil || w == nil {
		return
	}
	_, _ = io.WriteString(w, r.String())
}

// String 返回报告的文本。
//
// 形状：
//
//	<标题>
//	  <头部行>
//
//	  [OK  ] 检查项名   详情
//	         fix: 怎么修
//
//	<结论行>
//	result: healthy
func (r *Report) String() string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	if r.title != "" {
		b.WriteString(r.title)
		b.WriteString("\n")
	}
	for _, line := range r.header {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	if len(r.items) > 0 {
		b.WriteString("\n")
		// 名称列宽取最长检查项名——写死一个宽度会让长短不一的检查项名
		// 参差不齐，而这一列正是扫读时要看的那一列。
		nameWidth := 0
		for _, it := range r.items {
			if w := displayWidth(it.Name); w > nameWidth {
				nameWidth = w
			}
		}
		for _, it := range r.items {
			mark := statusMark(it.Status)
			b.WriteString("  [")
			b.WriteString(mark)
			b.WriteString("] ")
			b.WriteString(it.Name)
			if it.Detail != "" {
				b.WriteString(strings.Repeat(" ", nameWidth-displayWidth(it.Name)+1))
				b.WriteString(it.Detail)
			}
			b.WriteString("\n")
			if it.Fix != "" {
				// 缩进到名称列的起点，修复建议才看得出是挂在哪一项下面。
				b.WriteString(strings.Repeat(" ", 2+len(statusMark(it.Status))+2+1))
				b.WriteString("fix: ")
				b.WriteString(it.Fix)
				b.WriteString("\n")
			}
		}
	}
	for _, line := range r.conclusions {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString(r.resultLine())
	b.WriteString("\n")
	return b.String()
}

// resultLine 生成最后一行结论。
func (r *Report) resultLine() string {
	switch {
	case r.Failures() > 0:
		return fmt.Sprintf("result: failed (%d problem(s) need fixing)", r.Failures())
	case r.Warnings() > 0:
		return fmt.Sprintf("result: needs attention (%d warning(s))", r.Warnings())
	default:
		return "result: healthy"
	}
}

// statusMark 把状态渲染成定宽的标记（四个字符）。
//
// 定宽是为了让详情列对齐——不齐的那一列没法扫读。
func statusMark(status string) string {
	switch status {
	case StatusOK:
		return "OK  "
	case StatusWarn:
		return "WARN"
	case StatusFail:
		return "FAIL"
	default:
		return "????"
	}
}
