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
// childEnvCaseFold 环境变量名大小写归一开关（#489）：windows 环境变量名
// 大小写不敏感，白名单（envAllowed）已按大写归一，keep/literal 与去重键
// 用同一口径——否则按名透传静默丢变量（父进程是 Hf_Token、extra 写
// HF_TOKEN）、同名双条目（literal 小写没压住父进程大写）。包级变量以便
// 测试在非 windows 平台注入归一形态。
var childEnvCaseFold = runtime.GOOS == "windows"

// envName 环境变量名归一（childEnvCaseFold 口径）：windows 环境变量名大小写
// 不敏感，白名单/keep/literal/去重键统一按归一名比较，否则按名透传静默丢
// 变量、同名双条目。非 windows 恒等映射。
func envName(name string) string {
	if childEnvCaseFold {
		return strings.ToUpper(name)
	}
	return name
}

func ChildEnv(extra []string) []string {
	keep := map[string]bool{}
	literal := map[string]string{} // 归一名 → KEY=VALUE
	var litOrder []string
	for _, k := range extra {
		if i := strings.IndexByte(k, '='); i > 0 {
			name := envName(k[:i])
			if _, dup := literal[name]; !dup {
				litOrder = append(litOrder, name)
			}
			literal[name] = k
		} else {
			keep[envName(k)] = true
		}
	}
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if _, isLit := literal[envName(name)]; isLit {
			continue // 字面注入项优先，不透传父进程同名值（含异大小写）
		}
		if envAllowed(name) || keep[envName(name)] {
			env = append(env, kv)
		}
	}
	for _, name := range litOrder {
		env = append(env, literal[name])
	}
	// 先到先得去重（含 os.Environ() 自身可能出现的同名项；windows 按归一
	// 名去重）：显式给的压过缺省项，每 key 至多一项。
	out := make([]string, 0, len(env)+2)
	seen := make(map[string]bool, len(env)+2)
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		key := envName(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, kv)
	}
	for _, kv := range []string{"GIT_TERMINAL_PROMPT=0", "CI=1"} {
		name, _, _ := strings.Cut(kv, "=")
		if seen[envName(name)] {
			continue // 显式项在位：缺省值不得覆盖，也不得并列出第二项
		}
		seen[envName(name)] = true
		out = append(out, kv)
	}
	return out
}

// InheritAllExcept 返回「继承当前进程环境、剔除 except 名单」的子进程环境
// （自 oagentkit/runtime procx 下沉，runtime#26）：语义上等价于「用户在
// 自己的 shell 里跑这条命令」。
//
// # 什么时候该用它，什么时候绝对不该
//
// 它与 [ChildEnv] 服务的是**两套威胁模型**，选错哪一边都出事：
//
//   - **ChildEnv（白名单）**：子进程会执行**不可信代码**（cli agent / MCP
//     server 在用户工作目录里跑任务）。全量继承 = 把宿主凭证交给待执行任务，
//     所以必须白名单。
//   - **InheritAllExcept（继承）**：子进程是**用户主动发起的可信 CLI** 的
//     替身——SDK 包装 git / ssh / docker / aws 这类命令，替用户操作他自己的
//     仓库与账号。此时父进程环境**就是**用户的信任上下文，砍掉它才是不对的。
//
// 判断标准是「这段子进程代码是谁写的」：用户自己的工具 → 继承；别人投递来
// 待执行的代码 → 白名单。混淆这条线是本节存在的唯一理由。
//
// # 为什么可信场景不能用白名单「补枚举」
//
// 按名枚举在这个场景里是**不可收敛**的清单，不是「多补几个就够」：
//
//   - `GIT_*` 是开放式命名空间（GIT_AUTHOR_* / GIT_COMMITTER_* / GIT_SSH_* /
//     GIT_CONFIG_COUNT + GIT_CONFIG_KEY_n / GIT_TRACE*…），组合式配置靠约定
//     命名，静态列不全；
//   - `SSH_AUTH_SOCK`（agent 转发认证）与 `GIT_ASKPASS` / `SSH_ASKPASS` /
//     桌面 credential helper（经 PATH 发现）——凭据助手链是活的；
//   - locale 族（LC_* 逐项）影响编码与错误信息；
//   - 各类 SDK 自己的代理/配置变量。
//
// 新 git 版本读新变量、用户装新助手，枚举就漏——而漏的形态是「认证莫名失败」
// 或「读到别人的配置」，不是编译错误。
//
// # 失败方向声明（与 ChildEnv 相反）
//
// ChildEnv 的失败在安全侧（漏一个无害变量）；本函数的失败在**泄漏侧**——
// except 漏列一个凭据名，它就进了子进程环境。except 名单务必从「这个子进程
// 不该看到什么」出发逆向列举，而不是从「它需要什么」正举。
//
// # except 的口径
//
// except 是**变量名**名单（`[]string{"SSH_AUTH_SOCK"}`）；含 `=` 的项按
// `=` 之前的部分取名（`"GIT_TERMINAL_PROMPT=1"` 同样剔除该项并抑制下方缺省
// 追加），这样「我想剔掉它」与「我想改它的值」不会因为写法不同而行为不同。
// 变量名匹配按 childEnvCaseFold 口径（windows 大小写不敏感）。
//
// 尾部纪律与 ChildEnv 一致，且**剔除优先于追加**：
//
//   - 追加 `GIT_TERMINAL_PROMPT=0`（git 弹终端提问会挂到超时）与 `CI=1`
//     （对非 git 子进程无害）；
//   - 但若某项被 except 剔除，或父进程已有同名项，则不再追加——父进程的
//     值就是用户的值（`CI=0` 不该被悄悄改成 `CI=1`），且每个 key 至多出现
//     一次（environ 同名多项会让子进程取值依 libc 实现而异，runtime#31）。
//
// except 传 nil 即「全量继承 + 两条纪律」，这是最常见的用法。
func InheritAllExcept(except []string) []string {
	drop := make(map[string]bool, len(except))
	for _, k := range except {
		name, _, _ := strings.Cut(k, "=")
		if name != "" {
			drop[envName(name)] = true
		}
	}
	parent := os.Environ()
	out := make([]string, 0, len(parent)+2)
	seen := make(map[string]bool, len(parent)+2)
	for _, kv := range parent {
		name, _, _ := strings.Cut(kv, "=")
		key := envName(name)
		// drop 优先于 seen：被剔除的项不该因为「先到」而留下。
		if drop[key] || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, kv)
	}
	// 缺省项按名去重后追加，口径与 ChildEnv 逐字一致（先到先得 + 剔除优先）。
	for _, kv := range []string{"GIT_TERMINAL_PROMPT=0", "CI=1"} {
		name, _, _ := strings.Cut(kv, "=")
		key := envName(name)
		if drop[key] || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, kv)
	}
	return out
}
