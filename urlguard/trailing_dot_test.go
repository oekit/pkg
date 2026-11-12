package urlguard

import "testing"

// #413 回归：层 1 对 host **尾部**分隔符变体整体失明——单 ASCII 尾点被拦、
// 双尾点（..）与全角尾点（。．｡）全部 PASS。根因有二：canonicalHost 只剥
// 一个 ASCII 尾点；全角尾分隔符的折叠发生在剥点之后，折叠产物再也得不到剥点
// （net.ParseIP 不认尾点、exotic 判据按 5 段早退 ⇒ 落进「普通域名」分支）。
// WHATWG URL 解析器（浏览器/node）会把全部这些形态归一回裸 IP/裸域名再连接，
// 与 #266 同款威胁口径：判定放行 + 客户端连回环/云 metadata。
//
// 回归表按 issue 建议的口径全组断言 BLOCKED。
func TestCheckURL_TrailingSeparatorVariantsBlocked(t *testing.T) {
	g := mustGuard(t)
	blocked := []string{
		// localhost 约定域 + 尾部变体
		"http://localhost/",
		"http://localhost./",
		"http://localhost../",
		"http://localhost.../",
		"http://localhost。/",
		"http://localhost．/",
		"http://localhost｡/",
		"http://foo.localhost./",
		"http://foo.localhost../",
		"http://foo.localhost。/",
		// 回环 + 尾部变体
		"http://127.0.0.1/",
		"http://127.0.0.1./",
		"http://127.0.0.1../",
		"http://127.0.0.1。/",
		"http://127.0.0.1．/",
		"http://127.0.0.1｡/",
		"http://127.0.0.1。../",
		// 云 metadata + 尾部变体
		"http://169.254.169.254./",
		"http://169.254.169.254../",
		"http://169.254.169.254。/",
		"http://169.254.169.254.。/",
		// 未指定地址 + 全角尾点（issue 实测的漏网形态）
		"http://0.0.0.0。/",
	}
	for _, raw := range blocked {
		if err := g.CheckURL(raw); err == nil {
			t.Errorf("%s 应阻断，实际 PASS（尾部分隔符变体放行）", raw)
		}
	}
}

// TestCheckHost_TrailingSeparatorVariantsBlocked CheckHost 面同一口径：
// 归一（含折叠）之后重剥尾点，判定结论与 CheckURL 一致。
func TestCheckHost_TrailingSeparatorVariantsBlocked(t *testing.T) {
	g := mustGuard(t)
	blocked := []string{
		"localhost", "localhost.", "localhost..", "localhost。",
		"127.0.0.1", "127.0.0.1.", "127.0.0.1..", "127.0.0.1。",
		"169.254.169.254.。",
		"foo.localhost..",
		"...", // 全点：归一成空串 ⇒ empty host 拒绝（fail-closed）
	}
	for _, host := range blocked {
		if err := g.CheckHost(host); err == nil {
			t.Errorf("CheckHost(%q) 应阻断，实际 PASS", host)
		}
	}
	// 公网域名 + 尾点仍放行（尾点不敏感是 DNS 语义，不误杀合法 FQDN）。
	if err := g.CheckHost("example.com."); err != nil {
		t.Errorf("CheckHost(example.com.) 不应阻断: %v", err)
	}
	if err := g.CheckURL("https://example.com../"); err != nil {
		t.Errorf("CheckURL(example.com..) 不应阻断: %v", err)
	}
}

// TestCheckURL_InternalFullWidthSeparatorStillBlocked 基线保持：host **内部**
// 全角分隔符（#266 修复面）照旧阻断，本修复不得把它改松。
func TestCheckURL_InternalFullWidthSeparatorStillBlocked(t *testing.T) {
	g := mustGuard(t)
	if err := g.CheckURL("http://127。0。0。1/"); err == nil {
		t.Error("127。0。0。1（内部全角）应阻断")
	}
}
