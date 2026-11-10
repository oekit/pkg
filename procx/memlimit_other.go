//go:build !linux

package procx

import "fmt"

// setupMemoryLimit 非 linux 如实降级声明（pkg#359）：内存上限要动内核接口
// （cgroup v2），windows/darwin 等没有等价原语——不假装支持，回「不能兑现」
// 并拒绝执行，绝不静默降级成不限。
func setupMemoryLimit(int64) (memGroup, error) {
	return nil, fmt.Errorf("procx: WithMemoryLimit: %w（仅 linux cgroup v2 兑现）", ErrMemoryLimitUnsupported)
}

func memoryLimitCapable() error {
	return fmt.Errorf("procx: MemoryLimitCapable: %w（仅 linux cgroup v2 兑现）", ErrMemoryLimitUnsupported)
}
