//go:build !linux

package procx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMemLimitUnsupportedPlatform 钉住非 Linux 的降级口径：如实声明、
// fail closed——不假装支持，更不静默不限（pkg#359）。
func TestMemLimitUnsupportedPlatform(t *testing.T) {
	// 问询面先答「不能兑现」。
	if err := MemoryLimitCapable(); !errors.Is(err, ErrMemLimitUnsupported) {
		t.Errorf("MemoryLimitCapable() = %v, want 包装 ErrMemLimitUnsupported", err)
	}
	// 创建面拒绝。
	if _, err := NewMemGroup(32 << 20); !errors.Is(err, ErrMemLimitUnsupported) {
		t.Errorf("NewMemGroup() err = %v, want 包装 ErrMemLimitUnsupported", err)
	}
	// 设置面拒绝。
	if err := attachMemGroup("", 1); !errors.Is(err, ErrMemLimitUnsupported) {
		t.Errorf("attachMemGroup() err = %v, want 包装 ErrMemLimitUnsupported", err)
	}

	// Run 面：拒绝执行，且子进程一个字节都不许跑。
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	marker := filepath.Join(t.TempDir(), "ran")
	args, env := helperArgs("touch", "PROCX_MEMLIMIT_MARKER="+marker)
	res, err := Run(ctx, os.Args[0], args, WithMemoryLimit(32<<20), WithEnv(env))
	if !errors.Is(err, ErrMemLimitUnsupported) {
		t.Errorf("Run(WithMemoryLimit) err = %v, want 包装 ErrMemLimitUnsupported", err)
	}
	if res != nil {
		t.Errorf("fail-closed 应返回 nil Result，got %+v", res)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Errorf("子进程跑过了（%v）——非 Linux 必须拒绝执行而不是静默不限", statErr)
	}
}
