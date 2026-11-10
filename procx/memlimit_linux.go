//go:build linux

package procx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// memCgroupPrefix 是内存上限组目录名前缀（组名含 pid 便于残组排查）。
const memCgroupPrefix = "procx-"

// memCgroup 是一个 cgroup v2 内存上限组。
type memCgroup struct {
	dir string
}

// setupMemoryLimit 在 cgroup v2 建内存上限组并写 memory.max（pkg#359）：
//
//  1. 定位 cgroup v2 挂载点（可写优先）与本进程 cgroup 路径（/proc/self/cgroup
//     的 0:: 行；缺该行即 cgroup v1，如实报不能兑现）；
//  2. 从本进程所在 cgroup **向上**找第一个已启用/可启用 memory 控制器的
//     祖先落位——memory.max 只在父级 subtree_control 启用了 memory 的子组
//     里存在，而 cgroup v2 的 no-internal-process 约束是双向的（有进程的组
//     启用不了 subtree_control、也迁不进已启用的组），本进程所在组通常不
//     具备启用条件；root 组豁免该约束 ⇒ root 运行的宿主总能落位；
//  3. 建组 procx-<pid>-<nanos>，写 memory.max = limit。
//
// 任一步失败即整体报错——fail closed：兑现不了就拒绝执行，不静默降级成
// 不限。组的创建/写入在子进程 Start **之前**完成：上限兑现不了时子进程
// 根本不会被执行（「建不出组也照跑」正是要堵的静默形态）。
func setupMemoryLimit(limit int64) (memGroup, error) {
	rel, ok := selfCgroupRel()
	if !ok {
		return nil, fmt.Errorf("procx: WithMemoryLimit: %w（cgroup v1 不兑现，仅 v2）", ErrMemoryLimitUnsupported)
	}
	mounts, err := cgroup2Mounts()
	if err != nil {
		return nil, err
	}
	var joined error
	for _, mount := range mounts {
		host, err := memoryCgroupHost(mount, filepath.Join(mount, rel))
		if err != nil {
			joined = errors.Join(joined, err)
			continue
		}
		dir := filepath.Join(host, fmt.Sprintf("%s%d-%d", memCgroupPrefix, os.Getpid(), time.Now().UnixNano()))
		if err := os.Mkdir(dir, 0o755); err != nil {
			joined = errors.Join(joined, fmt.Errorf("procx: WithMemoryLimit: 建 cgroup %s: %w", dir, err))
			continue
		}
		g := &memCgroup{dir: dir}
		if err := g.write("memory.max", strconv.FormatInt(limit, 10)); err != nil {
			_ = g.destroy()
			joined = errors.Join(joined, err)
			continue
		}
		// swap 同步钉 0：memory.max 只帽 RSS + page cache，系统有 swap 时超限
		// 是「狂换页继续跑」而不是「超限被杀」，OOM 语义落空（Docker VM 实测）。
		// 钉 0 后超限必被内核 OOM-kill ⇒ 退出状态可辨「兜底生效」。文件不存在
		// = 内核无 swap 记账、无可逃逸，无需钉。
		if _, err := os.Stat(filepath.Join(g.dir, "memory.swap.max")); err == nil {
			if err := g.write("memory.swap.max", "0"); err != nil {
				_ = g.destroy()
				joined = errors.Join(joined, err)
				continue
			}
		}
		return g, nil
	}
	return nil, fmt.Errorf("procx: WithMemoryLimit: %w（建不出内存上限组：%v）", ErrMemoryLimitUnsupported, joined)
}

// memoryLimitCapable 真实探测：建组 + 写 memory.max + 回收，全程走通才算
// 能兑现（不看脸色猜权限——「建不出即拒绝」）。
func memoryLimitCapable() error {
	g, err := setupMemoryLimit(1 << 20)
	if err != nil {
		return err
	}
	return g.destroy()
}

// selfCgroupRel 返回本进程在 cgroup v2 层级里的相对路径（"" = 根）。
// 没有 0:: 行即 cgroup v1/混合挂载下的 v1 语义，ok=false。
func selfCgroupRel() (string, bool) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, found := strings.CutPrefix(line, "0::"); found {
			return rest, true
		}
	}
	return "", false
}

// cgroup2Mounts 列出全部 cgroup2 挂载点（同一棵树可能被挂多次，可写的那
// 个才是落位点——只读挂载上读得到但建不了组）。
func cgroup2Mounts() ([]string, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, fmt.Errorf("procx: WithMemoryLimit: 读 mountinfo: %w", err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		left := strings.Fields(parts[0])
		right := strings.Fields(parts[1])
		if len(left) < 5 || len(right) < 1 || right[0] != "cgroup2" {
			continue
		}
		out = append(out, left[4])
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("procx: WithMemoryLimit: %w（未挂载 cgroup v2）", ErrMemoryLimitUnsupported)
	}
	return out, nil
}

// memoryCgroupHost 从 base 向上找到第一个已启用/可启用 memory 控制器的
// cgroup 作为建组落位点。base 自己通常不具备启用条件（no-internal-process
// 约束：有进程的组启用不了 subtree_control），root 组豁免该约束。
func memoryCgroupHost(mount, base string) (string, error) {
	root := filepath.Clean(mount)
	dir := filepath.Clean(base)
	for {
		if memoryReady(dir) {
			return dir, nil
		}
		// 尽力启用：无权限/被约束挡住就失败，继续向上找
		_ = os.WriteFile(filepath.Join(dir, "cgroup.subtree_control"), []byte("+memory"), 0o644)
		if memoryReady(dir) {
			return dir, nil
		}
		if dir == root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir || !strings.HasPrefix(dir, root+string(filepath.Separator)) {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("%s 向上至 %s 无可启用 memory 控制器的 cgroup", base, root)
}

// memoryReady 判 dir 的 subtree_control 是否已启用 memory（启用后其子组
// 才有 memory.max 文件）。
func memoryReady(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "cgroup.subtree_control"))
	if err != nil {
		return false
	}
	return hasWord(string(data), "memory")
}

func hasWord(s, word string) bool {
	for _, f := range strings.Fields(s) {
		if f == word {
			return true
		}
	}
	return false
}

// attach 把子进程移入组。失败时调用方负责收场（Run 会杀掉刚启动的子进程
// 树）——上限不生效还照跑就是「静默不限」，坚决不做。
func (g *memCgroup) attach(pid int) error {
	return g.write("cgroup.procs", strconv.Itoa(pid))
}

func (g *memCgroup) write(name, val string) error {
	if err := os.WriteFile(filepath.Join(g.dir, name), []byte(val), 0o644); err != nil {
		return fmt.Errorf("procx: WithMemoryLimit: 写 %s=%s: %w", name, val, err)
	}
	return nil
}

// destroy 回收组：先只 rmdir（等被杀进程的收尸窗口），仍有残余进程才把它们
// 移回父组兜底再删（如实声明：移出的进程失去上限保护——但 Run 的树终止/
// 残留清扫已把该死的都打死，此处只兜「刚被杀还没收完尸」的竞态）。最终
// 删不掉如实报错，不留静默残组。
func (g *memCgroup) destroy() error {
	if g == nil || g.dir == "" {
		return nil
	}
	dir := g.dir
	var lastErr error
	for i := 0; i < 50; i++ {
		if err := os.Remove(dir); err == nil {
			g.dir = ""
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(10 * time.Millisecond)
	}
	parent := filepath.Dir(dir)
	for i := 0; i < 50; i++ {
		for _, p := range readCgroupProcs(dir) {
			_ = os.WriteFile(filepath.Join(parent, "cgroup.procs"), []byte(p), 0o644)
		}
		if err := os.Remove(dir); err == nil {
			g.dir = ""
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("procx: WithMemoryLimit: 回收 cgroup %s: %w", dir, lastErr)
}

func readCgroupProcs(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "cgroup.procs"))
	if err != nil {
		return nil
	}
	return strings.Fields(string(data))
}
