package procx

import (
	"strings"
	"testing"
)

// withCaseFold 临时切换归一开关（默认按本机 GOOS），在非 windows 平台模拟
// windows 的大小写不敏感语义。测试串行执行，恢复安全。
func withCaseFold(t *testing.T, on bool) {
	t.Helper()
	old := childEnvCaseFold
	childEnvCaseFold = on
	t.Cleanup(func() { childEnvCaseFold = old })
}

func hasEntryCI(env []string, name string) bool {
	upper := strings.ToUpper(name)
	for _, kv := range env {
		n, _, _ := strings.Cut(kv, "=")
		if strings.ToUpper(n) == upper {
			return true
		}
	}
	return false
}

// #489：keep/literal 在 windows 大小写不敏感环境未归一——白名单归一了，
// extra 两形态没归一：按名透传静默丢变量、同名双条目。
func TestChildEnvExtraCaseInsensitiveOnWindows(t *testing.T) {
	withCaseFold(t, true)
	t.Setenv("HF_TOKEN", "parent-secret")

	// keep 形态：extra 写大写、父进程是小写异形体——归一后必须透传。
	env := ChildEnv([]string{"hf_token"})
	if !hasEntryCI(env, "HF_TOKEN") {
		t.Fatalf("keep 归一后应透传父进程 HF_TOKEN: %v", env)
	}

	// literal 形态：extra 字面注入小写名，父进程大写同名——归一后不得
	// 双条目（literal 压过父进程值）。
	env2 := ChildEnv([]string{"hf_token=lit"})
	count := 0
	for _, kv := range env2 {
		if strings.HasPrefix(strings.ToUpper(kv), "HF_TOKEN=") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("literal 与父进程同名异大小写应去重为一条: %v", env2)
	}
	if !strings.Contains(strings.Join(env2, "\n"), "hf_token=lit") {
		t.Fatalf("字面注入值应在位: %v", env2)
	}
}

// 非 windows 平台保持原语义（精确匹配）作为对照。
func TestChildEnvExtraCaseSensitiveOffWindows(t *testing.T) {
	withCaseFold(t, false)
	t.Setenv("HF_TOKEN", "parent-secret")
	env := ChildEnv([]string{"hf_token"})
	if hasEntryCI(env, "HF_TOKEN") {
		t.Fatalf("非 windows 平台按名精确匹配，大小写异形不透传: %v", env)
	}
}
