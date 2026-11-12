package webhook

// 注册表测试（#270）：per-platform 路由、未注册平台错误、热替换、
// Platforms 有序快照、并发安全。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestRegistry_RouteAndUnknown(t *testing.T) {
	rg := NewRegistry()
	github := NewGitHubValidator([]byte("gh-secret"))
	rg.Register("github", github)

	got, ok := rg.Validator("github")
	if !ok || got != github {
		t.Fatalf("应取回注册的验证器")
	}
	body := []byte("b")
	err := rg.Validate("github", mkReq(t, map[string]string{"X-Hub-Signature-256": "sha256=" + hexSign(t, "gh-secret", body)}, body), body)
	if err != nil {
		t.Fatalf("github 路由应通过, got %v", err)
	}
	err = rg.Validate("unknown", httptest.NewRequest(http.MethodPost, "/", nil), body)
	if !errors.Is(err, ErrUnknownPlatform) {
		t.Fatalf("未注册平台应 ErrUnknownPlatform, got %v", err)
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("错误应带平台名, got %q", err.Error())
	}
}

func TestRegistry_Replace(t *testing.T) {
	rg := NewRegistry()
	old := NewGitHubValidator([]byte("old-secret"))
	new := NewGitHubValidator([]byte("new-secret"))
	rg.Register("github", old)
	rg.Register("github", new) // 后写生效（密钥轮换热更）

	got, _ := rg.Validator("github")
	if got != new {
		t.Fatal("重复注册应后写生效")
	}
}

func TestRegistry_Platforms(t *testing.T) {
	rg := NewRegistry()
	for _, name := range []string{"gitcode", "github", "gitee", "forgejo"} {
		rg.Register(name, NewGiteaValidator([]byte("s")))
	}
	got := rg.Platforms()
	want := []string{"forgejo", "gitcode", "gitee", "github"} // 有序
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Platforms 应有序, got %v want %v", got, want)
	}
	// 快照独立性：改返回切片不影响注册表
	got[0] = "mutated"
	if got2 := rg.Platforms(); got2[0] != "forgejo" {
		t.Fatalf("Platforms 应返回独立快照")
	}
}

func TestRegistry_FailFast(t *testing.T) {
	rg := NewRegistry()
	cases := []struct {
		name string
		fn   func()
	}{
		{"空平台名", func() { rg.Register("", NewGiteaValidator([]byte("s"))) }},
		{"nil验证器", func() { rg.Register("x", nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("构造期非法值应 panic（仓规总则①）")
				}
			}()
			tc.fn()
		})
	}
}

func TestRegistry_Concurrent(t *testing.T) {
	rg := NewRegistry()
	rg.Register("stable", NewGiteaValidator([]byte("s")))
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			rg.Register("hot", NewGiteaValidator([]byte("s")))
			_ = rg.Platforms()
		})
	}
	for range 4 {
		wg.Go(func() {
			body := []byte("b")
			_ = rg.Validate("stable", httptest.NewRequest(http.MethodPost, "/", nil), body)
		})
	}
	wg.Wait()
}
