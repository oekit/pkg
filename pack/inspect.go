package pack

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/oekit/pkg/errors"
)

// CurrentFormatVersion 是当前写出的清单格式版本。
const CurrentFormatVersion = 1

var (
	// ErrCorrupt 表示迁移包损坏：zip 读不开、清单缺失/损坏、清单 JSON 解析失败。
	// 用 errors.Is 判定，报错信息可直接展示给用户。
	ErrCorrupt = errors.New("pack: 迁移包损坏")
	// ErrUnsupportedVersion 表示清单的格式版本不被调用方识别。
	// 报错信息里带实际版本号与期望版本号。
	ErrUnsupportedVersion = errors.New("pack: 格式版本不兼容")
	// ErrFileNotFound 表示包内不存在指定文件。
	ErrFileNotFound = errors.New("pack: 包内不存在该文件")
)

// maxManifestSize 限制清单大小，避免畸形包把内存吃满。
const maxManifestSize = 8 << 20

// Inspect 对迁移包做体检。
//
// 一次说清所有问题而不是遇到第一个错误就停：problems 里会同时列出缺失文件、摘要不符、
// 条数不符、多余文件、非法文件名等；err 只在「包损坏」「版本不兼容」两类不可导原因时返回。
func Inspect(r io.ReaderAt, size int64) (*Manifest, []string, error) {
	return InspectVersion(r, size, CurrentFormatVersion)
}

// InspectVersion 与 Inspect 相同，但显式指定调用方期望的清单格式版本。
//
// 版本一致时 err 为 nil（问题都落在 problems 里）；版本不一致时 err 包装 ErrUnsupportedVersion，
// 此时清单仍会返回（便于把实际版本号展示给用户）。
func InspectVersion(r io.ReaderAt, size int64, expected int) (*Manifest, []string, error) {
	var problems []string

	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, problems, newPackError(ErrCorrupt,
			fmt.Sprintf("pack: 迁移包损坏：zip 无法读取（%v）", err), err)
	}
	m, err := readManifest(zr, expected)
	if err != nil {
		problems = append(problems, err.Error())
		return m, problems, err
	}
	problems = append(problems, verifyEntries(zr, m)...)
	return m, problems, nil
}

// packError 承载「类别哨兵 + 面向用户的消息 + 底层原因」。
type packError struct {
	kind  error
	msg   string
	cause error
}

// Error 返回面向用户的消息。
func (e *packError) Error() string { return e.msg }

// Unwrap 同时暴露类别哨兵与底层原因，errors.Is 可判定两者。
func (e *packError) Unwrap() []error {
	if e.cause != nil {
		return []error{e.kind, e.cause}
	}
	return []error{e.kind}
}

// newPackError 构造带类别哨兵的错误。
func newPackError(kind error, msg string, cause error) error {
	return &packError{kind: kind, msg: msg, cause: cause}
}

// readManifest 读取并解析包内清单，同时校验格式版本。
func readManifest(zr *zip.Reader, expected int) (*Manifest, error) {
	entries, _ := indexEntries(zr)
	f, ok := entries[manifestName]
	if !ok {
		return nil, newPackError(ErrCorrupt, "pack: 迁移包损坏：缺少 "+manifestName, nil)
	}
	data, err := readLimited(f, maxManifestSize)
	if err != nil {
		return nil, newPackError(ErrCorrupt,
			fmt.Sprintf("pack: 迁移包损坏：读取 %s 失败（%v）", manifestName, err), err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, newPackError(ErrCorrupt,
			fmt.Sprintf("pack: 迁移包损坏：解析 %s 失败（%v）", manifestName, err), err)
	}
	if m.FormatVersion != expected {
		return &m, newPackError(ErrUnsupportedVersion,
			fmt.Sprintf("pack: 格式版本不兼容：期望 %d，实际 %d", expected, m.FormatVersion), nil)
	}
	return &m, nil
}

// verifyEntries 逐项核对清单与包内容，返回问题清单。
func verifyEntries(zr *zip.Reader, m *Manifest) []string {
	var problems []string
	entries, names := indexEntries(zr)

	// 清单里声明的数据文件：是否在、摘要对不对、条数对不对
	for _, name := range sortedKeys(m.Files) {
		want := m.Files[name]
		f, ok := entries[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("缺少文件：%s", name))
			continue
		}
		got, lines, err := digestAndCount(f)
		if err != nil {
			problems = append(problems, fmt.Sprintf("文件 %s 读取失败：%v", name, err))
			continue
		}
		if !strings.EqualFold(got, want) {
			problems = append(problems, fmt.Sprintf("摘要不符：%s（清单 %s，实际 %s）", name, want, got))
		}
		if c, ok := m.Counts[name]; ok && c != lines {
			problems = append(problems, fmt.Sprintf("条数不符：%s（清单 %d，实际 %d）", name, c, lines))
		}
	}

	// 包内多出来的文件（清单没记）
	for _, name := range names {
		if name == manifestName {
			continue
		}
		if _, ok := m.Files[name]; !ok {
			problems = append(problems, fmt.Sprintf("清单未记录的文件：%s", name))
		}
	}

	// 非法文件名（zip slip）
	for _, name := range names {
		if name == manifestName {
			continue
		}
		if _, err := sanitizeName(name); err != nil {
			problems = append(problems, err.Error())
		}
	}
	return problems
}

// indexEntries 建立条目名索引（同名条目取第一个）并返回全部条目名。
func indexEntries(zr *zip.Reader) (map[string]*zip.File, []string) {
	entries := make(map[string]*zip.File, len(zr.File))
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
		if _, ok := entries[f.Name]; !ok {
			entries[f.Name] = f
		}
	}
	return entries, names
}

// digestAndCount 流式计算条目的 SHA-256 摘要与行数（不把整个文件读进内存）。
func digestAndCount(f *zip.File) (string, int, error) {
	rc, err := f.Open()
	if err != nil {
		return "", 0, err
	}
	defer rc.Close()

	h := sha256.New()
	buf := make([]byte, 32*1024)
	lines := 0
	nonEmpty := false
	last := byte('\n')
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			nonEmpty = true
			last = buf[n-1]
			for _, b := range buf[:n] {
				if b == '\n' {
					lines++
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", 0, err
		}
	}
	if nonEmpty && last != '\n' {
		lines++ // 末行没有换行符
	}
	return hex.EncodeToString(h.Sum(nil)), lines, nil
}

// readLimited 读取条目内容，超过 limit 字节即报错。
func readLimited(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s 超过 %d 字节上限", f.Name, limit)
	}
	return data, nil
}

// sortedKeys 返回 map 的键（字典序），保证问题清单输出稳定。
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
