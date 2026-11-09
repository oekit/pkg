package procx

import (
	"strings"
	"testing"
)

// envEntry 返回环境里 name= 开头的项（无则空串）。前缀含 "="，因此
// "TMP=" 不会误命中 "TMPDIR=x"。
func envEntry(env []string, name string) string {
	prefix := name + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return kv
		}
	}
	return ""
}

// pkg#314 验收判据（env 清洗）：父进程设了敏感变量，白名单环境里必须
// 扫不到该值，同时 PATH 必须在。变异负对照口径内嵌于变量选型：
//   - 若实现是 denylist（只剔 PB_ 前缀）⇒ AWS_SECRET_ACCESS_KEY 泄漏 ⇒ 红；
//   - 若实现只剔固定几个已知变量名 ⇒ X_API_TOKEN / SWEEP_NOISE_VAR 泄漏 ⇒ 红。
//
// 只有「默认拒绝 + 白名单放行」能全绿。
func TestChildEnv_DenyByDefault(t *testing.T) {
	const (
		secretPB  = "pb-secret-value"
		secretAWS = "aws-secret-value"
		secretX   = "x-secret-value"
		noise     = "noise-value"
	)
	t.Setenv("PB_ADMIN_PASSWORD", secretPB)
	t.Setenv("AWS_SECRET_ACCESS_KEY", secretAWS)
	t.Setenv("X_API_TOKEN", secretX)
	t.Setenv("SWEEP_NOISE_VAR", noise)
	env := ChildEnv(nil)
	joined := strings.Join(env, "\n")
	for _, v := range []string{secretPB, secretAWS, secretX, noise} {
		if strings.Contains(joined, v) {
			t.Fatalf("敏感/无关变量值 %q 泄漏进白名单环境:\n%s", v, joined)
		}
	}
	if envEntry(env, "PATH") == "" {
		t.Fatalf("PATH 必须在白名单环境里（子进程找不到可执行文件即失能）:\n%s", joined)
	}
}

// 结构化口径的「OS 环境不泄漏」：产出的每一项，键必须是白名单键或尾部
// 纪律缺省项——不逐个列敏感名，而是白名单外的键一律不该出现（ChildEnv
// 的过滤路径被改坏时在此红灯，谓词本身由 TestChildEnv_DenyByDefault 从
// 行为侧钉住）。
func TestChildEnv_OnlyAllowlistedKeys(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "x")
	t.Setenv("SWEEP_NOISE_VAR", "x")
	for _, kv := range ChildEnv(nil) {
		name, _, _ := strings.Cut(kv, "=")
		if !envAllowed(name) && name != "GIT_TERMINAL_PROMPT" && name != "CI" {
			t.Fatalf("白名单外的键 %q 出现在产出环境里: %s", name, kv)
		}
	}
}

func TestChildEnv_ExtraPassthrough(t *testing.T) {
	t.Setenv("HF_TOKEN", "from-parent")
	env := ChildEnv([]string{"HF_TOKEN", "NOT_SET_ANYWHERE"})
	if got := envEntry(env, "HF_TOKEN"); got != "HF_TOKEN=from-parent" {
		t.Fatalf("按名透传应取父进程值，got %q（全量 %v）", got, env)
	}
	if envEntry(env, "NOT_SET_ANYWHERE") != "" {
		t.Fatalf("父进程不存在的按名放行应静默丢弃: %v", env)
	}
}

func TestChildEnv_LiteralInjectionOverridesParent(t *testing.T) {
	t.Setenv("HF_TOKEN", "from-parent")
	env := ChildEnv([]string{"HF_TOKEN=literal-value"})
	if got := envEntry(env, "HF_TOKEN"); got != "HF_TOKEN=literal-value" {
		t.Fatalf("字面注入应压过父进程同名值，got %q（全量 %v）", got, env)
	}
	if n := strings.Count(strings.Join(env, "\n"), "HF_TOKEN="); n != 1 {
		t.Fatalf("每 key 至多一项，实际 %d 项: %v", n, env)
	}
}

// 尾部纪律：缺省追加 GIT_TERMINAL_PROMPT=0 与 CI=1；显式给过同名项时
// 缺省项让位，且不并列出第二项（每 key 唯一——environ 同名两项时子进程
// 取值依 libc 实现而异）。
func TestChildEnv_TailDiscipline(t *testing.T) {
	env := ChildEnv(nil)
	if envEntry(env, "GIT_TERMINAL_PROMPT") != "GIT_TERMINAL_PROMPT=0" {
		t.Fatalf("缺省应追加 GIT_TERMINAL_PROMPT=0: %v", env)
	}
	if envEntry(env, "CI") != "CI=1" {
		t.Fatalf("缺省应追加 CI=1: %v", env)
	}
	env = ChildEnv([]string{"CI=0", "GIT_TERMINAL_PROMPT=1"})
	if envEntry(env, "CI") != "CI=0" || envEntry(env, "GIT_TERMINAL_PROMPT") != "GIT_TERMINAL_PROMPT=1" {
		t.Fatalf("显式项应压过缺省纪律项: %v", env)
	}
	joined := strings.Join(env, "\n")
	if strings.Count(joined, "CI=") != 1 || strings.Count(joined, "GIT_TERMINAL_PROMPT=") != 1 {
		t.Fatalf("每 key 至多一项: %v", env)
	}
}

// 代理变量默认放行（issue 明确的诉求点：内网部署下砍掉代理 = 子进程静默
// 失去出口）。
func TestChildEnv_ProxyKeptByDefault(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://corp-proxy:3128")
	t.Setenv("NO_PROXY", "localhost,.corp")
	env := ChildEnv(nil)
	if envEntry(env, "HTTPS_PROXY") != "HTTPS_PROXY=http://corp-proxy:3128" {
		t.Fatalf("HTTPS_PROXY 应在白名单: %v", env)
	}
	if envEntry(env, "NO_PROXY") != "NO_PROXY=localhost,.corp" {
		t.Fatalf("NO_PROXY 应在白名单: %v", env)
	}
}
