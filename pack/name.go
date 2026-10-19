package pack

import (
	"fmt"
	"strings"

	"github.com/oekit/pkg/errors"
)

// manifestName 是包内清单的固定文件名。
const manifestName = "manifest.json"

// sanitizeName 校验条目名的安全性并返回该名称（防 zip slip 路径穿越）。
//
// 允许的形态：不含路径分隔符、不含 ".."、非绝对路径、无控制字符的普通文件名。
// 清单文件名本身也被拒绝——数据条目不能占用清单名。
func sanitizeName(name string) (string, error) {
	if name == "" {
		return "", errors.New("pack: 文件名为空")
	}
	if strings.TrimSpace(name) != name {
		return "", fmt.Errorf("pack: 非法文件名 %q：不允许首尾空白", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("pack: 非法文件名 %q：不允许包含路径分隔符", name)
	}
	if strings.Contains(name, "..") {
		return "", fmt.Errorf("pack: 非法文件名 %q：不允许包含连续点序列", name)
	}
	if strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("pack: 非法文件名 %q：不允许绝对路径", name)
	}
	// Windows 盘符形式（如 C:data.json）
	if len(name) >= 2 && name[1] == ':' {
		return "", fmt.Errorf("pack: 非法文件名 %q：不允许绝对路径", name)
	}
	if name == "." {
		return "", fmt.Errorf("pack: 非法文件名 %q：不是合法文件名", name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("pack: 非法文件名 %q：不允许控制字符", name)
		}
	}
	if name == manifestName {
		return "", fmt.Errorf("pack: 非法文件名 %q：与清单文件名冲突", name)
	}
	return name, nil
}
