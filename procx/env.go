package procx

import (
	"os"
	"runtime"
	"strings"
)

// baseEnvAllow 子进程缺省环境白名单：路径/账户/临时目录/语言时区/终端/
// 代理/CA 信任/windows 运行必需组。绝不全量继承——执行不可信内容（用户
// 工作目录里的 agent 任务、匿名输入喂进来的 ffmpeg/ffprobe 处理）的子进程，
// 全量环境等于把宿主凭证（凭据变量、云 SDK token、内部服务地址）整箱交接。
//
// 本表是 pkg 侧子进程环境纪律的单一事实源（Run 的 WithEnv 与自行装配
// *exec.Cmd 的调用方共用 ChildEnv，纪律只有一份）；新增基础变量只改这里。
//
// 白名单口径是「子进程要正常工作所需的最小集」，不是「已知无害」全集：
// 工具特定配置（XDG_*、各家 CLI 的配置目录）走 extra 按名放行——每个调用点
// 显式声明自己需要什么。代理变量默认放行：内网部署下子进程靠它们拿出口，
// 砍掉代理等于静默断网。
var baseEnvAllow = map[string]bool{
	// 路径与账户（unix）
	"PATH": true, "HOME": true, "TMPDIR": true,
	"USER": true, "LOGNAME": true, "SHELL": true,
	// 语言/时区/终端
	"LANG": true, "LC_ALL": true, "TERM": true, "TZ": true,
	// 代理（大小写两族都放行——工具约定不一，砍哪族都断一批子进程的出口）
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true,
	"http_proxy": true, "https_proxy": true, "no_proxy": true,
	// CA 信任锚（私有 CA 场景子进程的 TLS 握手靠它）
	"SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
	// windows 运行必需组：SystemRoot 缺失时大量程序（含 Go runtime 的
	// DNS 解析）直接失能；TEMP/TMP/PATHEXT/ComSpec 是 shell 与临时文件
	// 约定；USERPROFILE/APPDATA/LOCALAPPDATA 是用户级配置目录约定。
	"SystemRoot": true, "SystemDrive": true, "windir": true,
	"ComSpec": true, "PATHEXT": true,
	"TEMP": true, "TMP": true,
	"USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true,
	"NUMBER_OF_PROCESSORS": true, "PROCESSOR_ARCHITECTURE": true, "OS": true,
}

// envAllowed 判变量名是否在白名单。windows 的环境变量名大小写不敏感
// （CreateProcess 与 GetEnvironmentVariable 都按不区分大小写解析），按
// 大写归一匹配——父环境给 "Path"、本表记 "PATH"，二者必须命中同一项，
// 否则 windows 子进程静默丢 PATH。
func envAllowed(name string) bool {
	if baseEnvAllow[name] {
		return true
	}
	if runtime.GOOS == "windows" {
		return baseEnvAllow[strings.ToUpper(name)]
	}
	return false
}

// ChildEnv 构造子进程最小环境（默认拒绝，pkg#314 补齐 pkg#294 承诺的
// env 半边）：环境集 = baseEnvAllow 白名单 ∪ extra 显式放行，白名单之外的
// 一切变量——包括未来新出现的敏感变量——一律不进子进程环境。刻意 allowlist
// 而非 denylist：denylist 只能列已知凭据名，新凭据永远漏；allowlist 的漏
// 只表现为「少了一个无害变量」，错了也错在安全侧。
//
// 经 procx.Run 执行时用 WithEnv 选项（Run 内部应用本函数）；自行装配
// *exec.Cmd 的调用方直接 cmd.Env = procx.ChildEnv(...)。
//
// extra 两种形态：纯名（如 "HF_TOKEN"）从当前进程按名透传（不存在则丢弃）；
// 含 "="（如 "HF_TOKEN=xxx"）按 KEY=VALUE 字面注入，且同名父进程值不透传
// （每 key 唯一——environ 同名两项时子进程取值依 libc 实现而异）。
//
// 尾部固定追加 GIT_TERMINAL_PROMPT=0（git 弹终端提问会挂到超时）与 CI=1
// （对非 git 子进程无害）；先到先得去重保证每 key 至多一项——显式给的
// （字面注入/按名透传/父环境已有同名）压过缺省项，缺省项不得覆盖显式值、
// 也不得并列出第二项。
//
// 与「可信 CLI 全量继承」场景的分工：本函数只服务「子进程会执行不可信
// 内容」的威胁模型。子进程是用户主动发起的可信 CLI 替身（git/ssh/docker
// 包装）时，父进程环境就是用户的信任上下文——Run/Command 缺省的继承语义
// （cmd.Env 不设即全量继承）就是那条路，不要用白名单「补枚举」可信场景
// （GIT_* 开放命名空间、SSH_AUTH_SOCK、credential helper 链是活的，静态
// 枚举列不全，漏的形态是认证莫名失败而非编译错误）。
func ChildEnv(extra []string) []string {
	keep := map[string]bool{}
	literal := map[string]string{} // name → KEY=VALUE
	var litOrder []string
	for _, k := range extra {
		if i := strings.IndexByte(k, '='); i > 0 {
			name := k[:i]
			if _, dup := literal[name]; !dup {
				litOrder = append(litOrder, name)
			}
			literal[name] = k
		} else {
			keep[k] = true
		}
	}
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if _, isLit := literal[name]; isLit {
			continue // 字面注入项优先，不透传父进程同名值
		}
		if envAllowed(name) || keep[name] {
			env = append(env, kv)
		}
	}
	for _, name := range litOrder {
		env = append(env, literal[name])
	}
	// 先到先得去重（含 os.Environ() 自身可能出现的同名项）：显式给的压过
	// 缺省项，每 key 至多一项。
	out := make([]string, 0, len(env)+2)
	seen := make(map[string]bool, len(env)+2)
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, kv)
	}
	for _, kv := range []string{"GIT_TERMINAL_PROMPT=0", "CI=1"} {
		name, _, _ := strings.Cut(kv, "=")
		if seen[name] {
			continue // 显式项在位：缺省值不得覆盖，也不得并列出第二项
		}
		seen[name] = true
		out = append(out, kv)
	}
	return out
}
