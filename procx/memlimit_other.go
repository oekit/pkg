//go:build !linux

package procx

import (
	"context"
	"fmt"
	"runtime"
)

// 非 Linux 的降级路径是**如实声明**（pkg#359：不假装支持）：四个平台钩子
// 都以 [ErrMemLimitUnsupported] 报错，WithMemoryLimit 的 Run 因此拒绝执行
// ——「静默不限」是本包禁止的形态，宁可不跑。

// stubErr 走**函数值**间接构造桩错误：SSA 的常量性分析对函数值调用不可证，
// 非 Linux 平台（桩恒返回非 nil）下调用点的 `if err != nil` 不再被判成
// 「恒真」触发 SA4023 伪阳性（darwin 本地 golangci 门禁实测红、CI 在
// ubuntu 不触发——跨平台分析的既知边界，本写法两端一致干净）。
var stubErr = func() error {
	return fmt.Errorf("%w（%s）", ErrMemLimitUnsupported, runtime.GOOS)
}

// createMemGroup 非 Linux 不能兑现 cgroup v2 memory.max。
func createMemGroup(limitBytes int64) (string, int64, error) {
	return "", 0, stubErr()
}

// attachMemGroup 非 Linux 不能兑现 cgroup v2 memory.max。
func attachMemGroup(dir string, pid int) error {
	return fmt.Errorf("%w（%s）", ErrMemLimitUnsupported, runtime.GOOS)
}

// removeMemGroup 非 Linux 无组可清（createMemGroup 恒失败，走不到这里）。
func removeMemGroup(ctx context.Context, dir string) error {
	return fmt.Errorf("%w（%s）", ErrMemLimitUnsupported, runtime.GOOS)
}
