package urlguard

import "testing"

func TestWithExactHosts_OnlyPinnedHostAllowed(t *testing.T) {
	g := mustGuard(t, WithExactHosts("gitlab.example.com"))

	if err := g.CheckURL("https://gitlab.example.com/hook"); err != nil {
		t.Errorf("钉死的 host 应放行：%v", err)
	}
	// 与 WithAllowDomains 的关键差别：子域**不**随白名单放行。
	if err := g.CheckURL("https://api.gitlab.example.com/hook"); err == nil {
		t.Error("白名单外的子域应被拒绝（精确白名单不是后缀豁免）")
	}
	// 放行型模型的默认面：未钉死时任意公网域都放行，钉死后就不是了。
	if err := g.CheckURL("https://evil.com/hook"); err == nil {
		t.Error("白名单外的 host 应被拒绝")
	}
}

func TestWithExactHosts_DefaultGuardStillAllowsPublic(t *testing.T) {
	// 对照：没配精确白名单时，evil.com 本就不在阻断清单里（放行型模型）。
	g := mustGuard(t)
	if err := g.CheckURL("https://evil.com/hook"); err != nil {
		t.Errorf("未配精确白名单时公网域应放行：%v", err)
	}
}

func TestWithExactHosts_PortParticipates(t *testing.T) {
	g := mustGuard(t, WithExactHosts("gitlab.example.com:8443"))

	if err := g.CheckURL("https://gitlab.example.com:8443/hook"); err != nil {
		t.Errorf("端口一致时应放行：%v", err)
	}
	if err := g.CheckURL("https://gitlab.example.com/hook"); err == nil {
		t.Error("URL 缺端口而白名单带端口：按字面不命中，应拒绝")
	}
	if err := g.CheckURL("https://gitlab.example.com:9443/hook"); err == nil {
		t.Error("端口不一致应拒绝")
	}

	// 反向：白名单不带端口时，URL 带显式端口（哪怕是默认端口）也不命中。
	g2 := mustGuard(t, WithExactHosts("gitlab.example.com"))
	if err := g2.CheckURL("https://gitlab.example.com/hook"); err != nil {
		t.Errorf("端口都缺时应放行：%v", err)
	}
	if err := g2.CheckURL("https://gitlab.example.com:443/hook"); err == nil {
		t.Error("白名单不带端口时 URL 带显式端口应拒绝（不替调用方猜默认端口）")
	}
}

func TestWithExactHosts_CaseAndTrailingDot(t *testing.T) {
	g := mustGuard(t, WithExactHosts("GitLab.Example.COM"))

	if err := g.CheckURL("https://gitlab.example.com/hook"); err != nil {
		t.Errorf("host 大小写不敏感：%v", err)
	}
	if err := g.CheckURL("https://gitlab.example.com./hook"); err != nil {
		t.Errorf("host 尾点不敏感：%v", err)
	}
}

func TestWithExactHosts_UserinfoEscapeBlocked(t *testing.T) {
	// 逃逸形态：owner/repo 来自 webhook 载荷（含 "//" 或 "@"）时，
	// host 会跑到攻击者服务器——带 token 出站即泄露。
	g := mustGuard(t, WithExactHosts("gitlab.example.com"))

	for _, raw := range []string{
		"https://gitlab.example.com@evil.com/repo.git",
		"https://gitlab.example.com:x@evil.com/repo.git",
		"https://evil.com/repo.git",
	} {
		if err := g.CheckURL(raw); err == nil {
			t.Errorf("%s 的 host 逃逸应被拒绝", raw)
		}
	}
}

func TestWithExactHosts_StillSubjectToPrivateAndScheme(t *testing.T) {
	// 钉死 host 不等于放行私网：私有段判定照旧。
	g := mustGuard(t, WithExactHosts("127.0.0.1"))
	if err := g.CheckURL("http://127.0.0.1/hook"); err == nil {
		t.Error("钉死的 loopback 仍应被私网判定拒绝")
	}

	// 显式放行私网后，钉死的 host 才放行（两个维度独立）。
	g2 := mustGuard(t, WithAllowPrivate(), WithExactHosts("127.0.0.1"))
	if err := g2.CheckURL("http://127.0.0.1/hook"); err != nil {
		t.Errorf("显式放行私网后应放行：%v", err)
	}

	// 约定域名（.internal）同理：白名单不豁免阻断清单。
	g3 := mustGuard(t, WithExactHosts("svc.internal"))
	if err := g3.CheckURL("http://svc.internal/hook"); err == nil {
		t.Error("钉死的约定域名仍应被阻断清单拒绝")
	}

	// scheme 白名单照旧。
	g4 := mustGuard(t, WithExactHosts("gitlab.example.com"))
	if err := g4.CheckURL("ftp://gitlab.example.com/hook"); err == nil {
		t.Error("钉死 host 仍应走 scheme 白名单")
	}
}

func TestWithExactHosts_IndependentFromAllowDomains(t *testing.T) {
	// 两个维度独立：域豁免不解除钉死，钉死也不解除域豁免的语义。
	g := mustGuard(t, WithAllowDomains("evil.com"), WithExactHosts("gitlab.example.com"))

	if err := g.CheckURL("https://evil.com/hook"); err == nil {
		t.Error("域豁免不应绕过精确白名单")
	}
	if err := g.CheckURL("https://gitlab.example.com/hook"); err != nil {
		t.Errorf("钉死的 host 应放行：%v", err)
	}
}

func TestWithExactHosts_CheckHostLevel(t *testing.T) {
	g := mustGuard(t, WithExactHosts("gitlab.example.com"))

	if err := g.CheckHost("gitlab.example.com"); err != nil {
		t.Errorf("钉死的 host 应放行：%v", err)
	}
	if err := g.CheckHost("api.gitlab.example.com"); err == nil {
		t.Error("白名单外的 host 应被拒绝")
	}

	// CheckHost 的入参没有端口：端口级条目无法被它确认。
	g2 := mustGuard(t, WithExactHosts("gitlab.example.com:8443"))
	if err := g2.CheckHost("gitlab.example.com"); err == nil {
		t.Error("CheckHost 无法确认端口，不应放行端口级条目")
	}
	if err := g2.CheckURL("https://gitlab.example.com:8443/hook"); err != nil {
		t.Errorf("CheckURL 的 authority 级判定应放行：%v", err)
	}
}

func TestWithExactHosts_EmptyListDeniesAll(t *testing.T) {
	// 配了空清单 = 一个 host 都不许（fail-closed）。
	g := mustGuard(t, WithExactHosts())
	if err := g.CheckURL("https://example.com/hook"); err == nil {
		t.Error("空精确白名单应拒绝一切 host")
	}
}

func TestWithExactHosts_IPv6Literal(t *testing.T) {
	g := mustGuard(t, WithExactHosts("[2001:db8::1]:8080"))

	if err := g.CheckURL("http://[2001:db8::1]:8080/hook"); err != nil {
		t.Errorf("IPv6 字面量 + 端口应放行：%v", err)
	}
	if err := g.CheckURL("http://[2001:db8::1]/hook"); err == nil {
		t.Error("端口不一致应拒绝")
	}
	if err := g.CheckURL("http://[2001:db8::2]:8080/hook"); err == nil {
		t.Error("地址不一致应拒绝")
	}
}

func TestWithExactHosts_InvalidEntryFailsFast(t *testing.T) {
	bad := []string{
		"",                  // 空
		"   ",               // 纯空白
		"example.com/path",  // 含路径
		"user@example.com",  // 含用户信息
		"example.com:abc",   // 端口非法
		"example.com:99999", // 端口越界
		"example.com:",      // 空端口
	}
	for _, entry := range bad {
		if _, err := New(WithExactHosts(entry)); err == nil {
			t.Errorf("非法条目 %q 应让 New 报错", entry)
		}
	}
}

func TestWithExactHosts_ValidEntriesAccepted(t *testing.T) {
	good := []string{
		"example.com",
		"example.com:8443",
		"EXAMPLE.com",
		"example.com.",
		"127.0.0.1",
		"127.0.0.1:8080",
		"[2001:db8::1]",
		"[2001:db8::1]:8080",
	}
	for _, entry := range good {
		if _, err := New(WithExactHosts(entry)); err != nil {
			t.Errorf("合法条目 %q 不该报错：%v", entry, err)
		}
	}
}

// 端口按数值比较：前导零不应导致不命中。
func TestWithExactHosts_PortNumericCompare(t *testing.T) {
	g := mustGuard(t, WithExactHosts("example.com:8443"))
	if err := g.CheckURL("https://example.com:8443/hook"); err != nil {
		t.Errorf("同端口应放行：%v", err)
	}
	if err := g.CheckURL("https://example.com:08443/hook"); err != nil {
		t.Errorf("端口前导零应按数值命中：%v", err)
	}
}
