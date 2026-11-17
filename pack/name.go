package pack

import (
	"strings"

	"github.com/oekit/pkg/errors"
	"github.com/oekit/pkg/pathx"
)

// manifestName 是包内清单的固定文件名。
const manifestName = "manifest.json"

// sanitizeName 校验条目名的安全性并返回该名称（防 zip slip 路径穿越）。
//
// 判定分两层（#536c，基线判定单源到 pathx）：
//  1. pathx.IsSafeFileName 基线（#253 闸门同口径）：拒绝路径分隔符（"/" 与
//     "\"）、"."/".." 段、绝对路径、UNC、Windows 盘符；
//  2. pack 特有规则叠加：首尾空白、控制字符、manifestName 保留名，外加两条
//     **比基线更严**的 pack 口径——".." 子串（"a..b.jsonl" 基线放行但 pack
//     拒绝）与任意字符盘符位（"1:foo" 基线放行但 pack 拒绝）。
//
// 各类非法名的报错文案保持 pack 既有口径不变——只让判定复用 pathx，文案
// 由 baselineViolationError 按原检查序对位映射。
func sanitizeName(name string) (string, error) {
	if name == "" {
		return "", errors.New("pack: 文件名为空")
	}
	if strings.TrimSpace(name) != name {
		return "", errors.Newf("pack: 非法文件名 %q：不允许首尾空白", name)
	}
	// pathx 基线判定（#253 闸门单源）：不过基线即非法，按 pack 既有文案报出。
	if !pathx.IsSafeFileName(name) {
		return "", baselineViolationError(name)
	}
	// pack 特有叠加规则（基线不覆盖或比基线更严的口径）。
	if strings.Contains(name, "..") {
		return "", errors.Newf("pack: 非法文件名 %q：不允许包含连续点序列", name)
	}
	// Windows 盘符形式（如 C:data.json）；比基线更严：基线只拒盘符位为字母
	// 的形态，pack 连数字/符号盘符位（"1:foo"）一并拒绝。
	if len(name) >= 2 && name[1] == ':' {
		return "", errors.Newf("pack: 非法文件名 %q：不允许绝对路径", name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", errors.Newf("pack: 非法文件名 %q：不允许控制字符", name)
		}
	}
	if name == manifestName {
		return "", errors.Newf("pack: 非法文件名 %q：与清单文件名冲突", name)
	}
	return name, nil
}

// baselineViolationError 把 pathx 基线判定不过的输入映射回 pack 既有文案。
// IsSafeFileName 只答 ok/false 不分类——这里按原检查序（分隔符 → 连续点 →
// 绝对路径前缀 → 盘符 → "."）对位映射，保证各类非法名文案不变。
func baselineViolationError(name string) error {
	switch {
	case strings.ContainsAny(name, `/\`):
		return errors.Newf("pack: 非法文件名 %q：不允许包含路径分隔符", name)
	case strings.Contains(name, ".."):
		return errors.Newf("pack: 非法文件名 %q：不允许包含连续点序列", name)
	case strings.HasPrefix(name, "/"):
		return errors.Newf("pack: 非法文件名 %q：不允许绝对路径", name)
	case len(name) >= 2 && name[1] == ':':
		return errors.Newf("pack: 非法文件名 %q：不允许绝对路径", name)
	case name == ".":
		return errors.Newf("pack: 非法文件名 %q：不是合法文件名", name)
	}
	// fail-closed：基线不过但上方分类未命中的形态（构造上不可达——基线的
	// 全部拒绝形态均被上方 case 覆盖）；宁可拒绝也不静默放行。
	return errors.Newf("pack: 非法文件名 %q：未通过 pathx 安全基线判定", name)
}
