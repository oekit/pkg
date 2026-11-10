package tarx

// manifest 自证（#326）：「传输完成」与「传输被证明」是两回事——流尾
// 逐文件 sha256 清单让接收端能证明收到的树就是发出的树。格式取 sha256sum
// 文本格式（`<hex>  <name>`）：远端有 ssh 就能人工 `sha256sum -c`，程序化
// 核对用 VerifyManifest（ErrMismatch 指名文件）。

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ManifestName WithManifest 夹带的清单条目名（解包后位于 root/<ManifestName>；
// 核对完是否删除由调用方定——一般 `os.Remove` 一行）。
const ManifestName = "oekit-tarx-manifest.txt"

// manifest 核对错误哨兵（errors.Is 判定；语义见各注释）。
var (
	// ErrNoManifest 目录里没有清单（root/<ManifestName> 不存在）——
	// 没用 WithManifest 的流解出来的树自然没有，调用方据此区分「没自证」
	// 与「自证失败」。
	ErrNoManifest = errors.New("tarx: manifest 不存在")
	// ErrMismatch 清单核对失败：文件缺失、内容被篡改、清单行格式非法
	// ——错误消息点名具体文件/行号。
	ErrMismatch = errors.New("tarx: manifest 核对失败")
)

// VerifyManifest 对解包后的树核对清单（读 root/<ManifestName>，逐文件
// sha256 比对）。
//
// 判定口径（与 sha256sum -c 一致）：
//
//   - 清单列出的文件缺失或摘要不符 → ErrMismatch（消息指名文件）；
//   - 清单行格式非法（前 64 字节非 hex 等）→ ErrMismatch（消息指名行号）；
//   - 树里多出的文件不核对（sha256sum -c 同语义——清单只承诺「列出的
//     都在且对」；「树比清单大」的检测是 Extract Stats 的事）；
//   - 清单文件本身缺失 → ErrNoManifest。
func VerifyManifest(root string) error {
	manifestPath := filepath.Join(root, ManifestName)
	data, err := os.ReadFile(manifestPath)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNoManifest, ManifestName)
	}
	if err != nil {
		return err
	}

	for i, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue // 末尾换行容忍
		}
		want, name, ok := parseManifestLine(line)
		if !ok {
			return fmt.Errorf("%w: %s 第 %d 行格式非法: %q", ErrMismatch, ManifestName, i+1, line)
		}
		target, ok := safeJoin(root, name)
		if !ok {
			// 清单被改出穿越条目：与解包同口径判死——绝不按清单指名走出
			// root（落点约束先行，os.Open 的名字不再是自由污点）
			return fmt.Errorf("%w: %s: 条目名越界 %q", ErrMismatch, ManifestName, name)
		}
		f, err := os.Open(target)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("%w: %s: 清单列出的文件缺失", ErrMismatch, name)
			}
			return err
		}
		sum := sha256.New()
		_, copyErr := io.Copy(sum, f)
		closeErr := f.Close()
		if copyErr != nil {
			return fmt.Errorf("tarx: %s: 读取中止: %w", name, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("tarx: %s: 关闭失败: %w", name, closeErr)
		}
		if got := hex.EncodeToString(sum.Sum(nil)); got != want {
			return fmt.Errorf("%w: %s: 期望 sha256 %s，实得 %s", ErrMismatch, name, want, got)
		}
	}
	return nil
}

// parseManifestLine 解析 sha256sum 文本格式一行：64 位 hex + 两个字符分隔
// （空格=文本模式 / 星号=二进制模式）+ 条目名。返回 (期望hex, 名, 合法)。
func parseManifestLine(line string) (want, name string, ok bool) {
	if len(line) < 66 || line[64] != ' ' || (line[65] != ' ' && line[65] != '*') {
		return "", "", false
	}
	want, name = line[:64], line[66:]
	if _, err := hex.DecodeString(want); err != nil {
		return "", "", false
	}
	return want, name, true
}
