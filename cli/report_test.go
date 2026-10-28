package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestReport_RendersChecksAndResult 断言报告的基本形状。
func TestReport_RendersChecksAndResult(t *testing.T) {
	var buf bytes.Buffer
	r := NewReport("mytool doctor")
	r.SetHeader("base_url: https://example.invalid")
	r.Add(StatusOK, "api", "the bastion answered")
	r.AddFix(StatusFail, "key_type", "the key is ed25519", "generate an RSA key")
	r.Render(&buf)

	out := buf.String()
	for _, want := range []string{
		"mytool doctor",
		"base_url: https://example.invalid",
		"[OK  ] api",
		"[FAIL] key_type",
		"fix: generate an RSA key",
		"result: failed (1 problem(s) need fixing)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("报告缺少 %q，实际 =\n%s", want, out)
		}
	}
}

// TestReport_StatusPrecedence 断言失败压过警告。
func TestReport_StatusPrecedence(t *testing.T) {
	r := NewReport("t")
	r.Add(StatusWarn, "w", "warning")
	if got := r.Status(); got != StatusWarn {
		t.Errorf("只有警告时状态 = %q，期望 %q", got, StatusWarn)
	}
	r.Add(StatusFail, "f", "failure")
	if got := r.Status(); got != StatusFail {
		t.Errorf("有失败时状态 = %q，期望 %q", got, StatusFail)
	}
	if r.Failures() != 1 || r.Warnings() != 1 {
		t.Errorf("Failures=%d Warnings=%d，期望 1/1", r.Failures(), r.Warnings())
	}
}

// TestReport_ResultLineReflectsCounts 断言结果行随检查项变化。
func TestReport_ResultLineReflectsCounts(t *testing.T) {
	cases := []struct {
		name  string
		items []ReportItem
		want  string
	}{
		{"全好", []ReportItem{{Status: StatusOK}}, "result: healthy"},
		{"有警告", []ReportItem{{Status: StatusWarn}}, "result: needs attention (1 warning(s))"},
		{"有失败", []ReportItem{{Status: StatusFail}}, "result: failed (1 problem(s) need fixing)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := NewReport("t")
			for _, it := range c.items {
				r.Add(it.Status, it.Name, it.Detail)
			}
			if got := r.String(); !strings.Contains(got, c.want) {
				t.Errorf("报告缺少 %q，实际 =\n%s", c.want, got)
			}
		})
	}
}

// TestReport_ConclusionBeforeResult 断言结论行排在结果行之前。
func TestReport_ConclusionBeforeResult(t *testing.T) {
	r := NewReport("t")
	r.Add(StatusOK, "api", "ok")
	r.AddConclusion("verdict: all good")
	out := r.String()
	vi := strings.Index(out, "verdict:")
	ri := strings.Index(out, "result:")
	if vi < 0 || ri < 0 || vi > ri {
		t.Errorf("结论行应当排在结果行之前，实际 =\n%s", out)
	}
}

// TestReport_FixIndentUnderName 断言修复建议缩进到名称列的起点。
//
// 这一条是「看得懂这一项怎么修」的前提：缩进对不上，修复建议就不知道
// 挂在哪一项下面。
func TestReport_FixIndentUnderName(t *testing.T) {
	r := NewReport("t")
	r.AddFix(StatusFail, "key_type", "wrong type", "regenerate")
	out := r.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	var checkLine, fixLine string
	for _, line := range lines {
		if strings.Contains(line, "[FAIL]") {
			checkLine = line
		}
		if strings.Contains(line, "fix:") {
			fixLine = line
		}
	}
	if checkLine == "" || fixLine == "" {
		t.Fatalf("报告缺少检查行或修复行，实际 =\n%s", out)
	}
	nameAt := strings.Index(checkLine, "key_type")
	fixAt := strings.Index(fixLine, "fix:")
	if fixAt != nameAt {
		t.Errorf("修复建议起点 = %d，期望与名称列起点 %d 相同，实际 =\n%s", fixAt, nameAt, out)
	}
}

// TestReport_NameColumnAligned 断言检查项名称列对齐（扫读要用）。
func TestReport_NameColumnAligned(t *testing.T) {
	r := NewReport("t")
	r.Add(StatusOK, "a", "first")
	r.Add(StatusOK, "long_name", "second")
	out := r.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	var first, second string
	for _, line := range lines {
		if strings.Contains(line, "first") {
			first = line
		}
		if strings.Contains(line, "second") {
			second = line
		}
	}
	if first == "" || second == "" {
		t.Fatalf("报告缺少检查行，实际 =\n%s", out)
	}
	at := displayWidth(second[:strings.Index(second, "second")])
	if at != displayWidth(first[:strings.Index(first, "first")]) {
		t.Errorf("详情列起点不一致：%q vs %q", first, second)
	}
}

// TestReport_EmptyReport 断言空报告不炸，且结论仍是 healthy。
func TestReport_EmptyReport(t *testing.T) {
	r := NewReport("t")
	out := r.String()
	if !strings.Contains(out, "result: healthy") {
		t.Errorf("空报告应当仍给出结果行，实际 = %q", out)
	}
}
