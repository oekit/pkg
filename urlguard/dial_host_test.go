package urlguard

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

// 本文件钉住「拨号层与写面的 host 判定同源」（pkg#204）。
//
// 修复前 DialFunc 只跑消费方钩子（checkHostPolicy），内置的约定域后缀判定
// 完全没走 —— 只要 `foo.local` / `x.internal` 解析出的 IP 不在私网段，拨号层
// 就放行，而同一目标经 CheckURL 是被拒的。写面拒绝、拨号面放行是最难发现的
// 那种不一致：单测各自都绿，只有把两个面摆在一起比才看得出来。

// publicIPv4 是「不在私网段」的地址，用来把后缀判定的作用与 IP 范围判定的
// 作用隔离开 —— 解析结果公网可达时，唯一能拦下拨号的只剩域名规则。
var publicIPv4 = net.ParseIP("93.184.216.34")

// withFakeLookup 把 DNS 固定成「解析出公网 IP」，避免用例依赖真实解析。
func withFakeLookup(t *testing.T, g *Guard) *Guard {
	t.Helper()
	g.lookup = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{publicIPv4}, nil
	}
	return g
}

// TestDialFunc_BlocksBuiltinDomainSuffixes 钉住拨号层也执行内置约定域判定。
func TestDialFunc_BlocksBuiltinDomainSuffixes(t *testing.T) {
	hosts := []string{
		"service.local:80",
		"api.internal:443",
		"localhost:8080",
		"db.localdomain:5432",
		// 等于后缀本身也算命中，不能只比对点分后缀
		"local:80",
		"internal:80",
	}
	g := withFakeLookup(t, mustGuard(t))
	dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
		t.Error("被阻断的 host 不得进入底层拨号")
		return nil, errors.New("should not dial")
	})

	for _, addr := range hosts {
		t.Run(addr, func(t *testing.T) {
			_, err := dial(context.Background(), "tcp", addr)
			if err == nil {
				t.Fatalf("拨号 %q 应被内置约定域判定阻断，got nil err", addr)
			}
			if !strings.Contains(err.Error(), "blocked domain") {
				t.Fatalf("拨号 %q 的错误应来自约定域判定，got %v", addr, err)
			}
		})
	}
}

// TestDialFunc_AllowsPublicHost 是对上的负对照（成对判据）：
// 同一套 fake lookup、同样的拨号包装，公网域名必须放行到 next。
//
// 没有这一条，上一条的「全绿」可能只是因为拨号层把一切都拒了。
func TestDialFunc_AllowsPublicHost(t *testing.T) {
	g := withFakeLookup(t, mustGuard(t))
	called := false
	dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
		called = true
		return nil, errors.New("stop here")
	})

	_, err := dial(context.Background(), "tcp", "example.com:443")
	if !called {
		t.Fatalf("公网域名应放行到 next，got err=%v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "stop here") {
		t.Fatalf("应停在底层拨号处（next 的哨兵错误），got %v", err)
	}
}

// TestDialFunc_HostVerdictMatchesCheckHost 是本次修复的核心判据：**同源**。
//
// 逐条比对 `CheckHost` 与拨号层的放行/拒绝结论。两边只要漂移一条就红——
// 包括 allowPrivate 短路、allowDomains 优先这两条容易「各写一份」的规则。
func TestDialFunc_HostVerdictMatchesCheckHost(t *testing.T) {
	cases := []struct {
		name string
		opts []Option
		host string
	}{
		{"约定域 .local", nil, "svc.local"},
		{"约定域 .internal", nil, "api.internal"},
		{"约定域 localhost", nil, "localhost"},
		{"约定域 .localdomain", nil, "box.localdomain"},
		{"公网域名", nil, "example.com"},
		{"私网 IP 字面量", nil, "10.0.0.1"},
		{"公网 IP 字面量", nil, "93.184.216.34"},
		{"exotic IPv4 字面量", nil, "127.1"},
		{"allowPrivate 下约定域", []Option{WithAllowPrivate()}, "svc.local"},
		{"allowDomains 显式放行约定域", []Option{WithAllowDomains("svc.local")}, "svc.local"},
		{"allowDomains 不覆盖兄弟域", []Option{WithAllowDomains("svc.local")}, "other.local"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			g := withFakeLookup(t, mustGuard(t, tt.opts...))

			checkErr := g.CheckHost(tt.host)
			dialed := false
			dial := g.DialFunc(func(context.Context, string, string) (net.Conn, error) {
				dialed = true
				return nil, errors.New("reach next")
			})
			_, dialErr := dial(context.Background(), "tcp", net.JoinHostPort(tt.host, "80"))

			if (checkErr == nil) != dialed {
				t.Fatalf("判定漂移：CheckHost(%q) err=%v（放行=%v），但拨号层%s",
					tt.host, checkErr, checkErr == nil, map[bool]string{true: "放行了", false: "没放行"}[dialed])
			}
			if checkErr != nil && dialErr == nil {
				t.Fatalf("CheckHost 拒绝但拨号层通过：%v", checkErr)
			}
		})
	}
}
