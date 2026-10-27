package pack

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
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

// maxEntrySize 限制单个数据条目**解压后**的字节数（防 zip bomb）。
//
// 清单有 maxManifestSize 保护，但**数据条目没有**——Inspect 要把每个声明的
// 条目完整解压一遍来算摘要与行数 ⇒ 一个「几 KB 的 zip、条目声明 1TB」的包
// 能让体检无限期地烧 CPU（ReadJSONL 里还会把整行读进内存）。
//
// 1GiB 对迁移包是宽裕的上限（正常数据包远小于此），同时把经典 zip bomb 挡在
// 门外。声明的 UncompressedSize64 可以撒谎，所以除预检外还必须在实际解压时
// 用计数器硬卡。
const maxEntrySize = 1 << 30

// Inspect 对迁移包做体检。
//
// 一次说清所有问题而不是遇到第一个错误就停：problems 里会同时列出缺失文件、摘要不符、
// 条数不符、条数未核对、多余文件、非法文件名等；err 只在「包损坏」「版本不兼容」两类不可导原因时返回。
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
	entries, _, _ := indexEntries(zr)
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
//
// 条数核对覆盖两种键口径：先按**文件名精确匹配** counts，再按**「文件名去
// 扩展名」的基名宽松匹配**（覆盖「键=类别名/基名」的生产者口径，如键 "raw"
// 对应文件 "raw.jsonl"）。两种口径都匹配不上、或 counts 里的键匹配不上任何
// 数据文件时，**都明确报出「条数未核对」**——绝不静默跳过一条检查却让
// 体检报「通过」。
func verifyEntries(zr *zip.Reader, m *Manifest) []string {
	var problems []string
	entries, names, dupes := indexEntries(zr)

	// matched 记录已被某个数据文件认领的 counts 键（用于收尾报「键未匹配」）。
	matched := make(map[string]bool, len(m.Counts))

	// 清单里声明的数据文件：是否在、摘要对不对、条数对不对
	for _, name := range sortedKeys(m.Files) {
		want := m.Files[name]
		f, ok := entries[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("缺少文件：%s", name))
			// 文件不在包内，摘要与条数都无从核对；counts 的键仍算被本文件认领，
			// 免得收尾时再报一条「键未匹配任何数据文件」的重复结论。
			if key, _, hasCount := matchCountsKey(m.Counts, name); hasCount {
				matched[key] = true
			}
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
		key, c, ok := matchCountsKey(m.Counts, name)
		if !ok {
			// 没有可对应的 counts 条目 → 条数没核对过，明确报出，不静默放过。
			problems = append(problems, fmt.Sprintf("条数未核对：%s（清单 counts 无对应条目）", name))
			continue
		}
		matched[key] = true
		if c != lines {
			problems = append(problems, fmt.Sprintf("条数不符：%s（清单 %d，实际 %d）", name, c, lines))
		}
	}

	// 清单 counts 里匹配不上任何数据文件的键 → 该键的条数从没被核对过
	for _, key := range sortedKeys(m.Counts) {
		if !matched[key] {
			problems = append(problems,
				fmt.Sprintf("清单 counts 的键 %s 未匹配任何数据文件（该键的条数未核对）", key))
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

	// 重名条目：清单只校验了其中一份，其余份不被任何检查覆盖。
	for _, name := range dupes {
		problems = append(problems,
			fmt.Sprintf("重名条目：%s（zip 内同名条目并存，清单只校验了其中一份）", name))
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

// matchCountsKey 返回适用于数据文件 name 的 counts 键与条数。
//
// 先按**文件名精确匹配**；精确匹配不上时按**「文件名去扩展名」的基名宽松
// 匹配**——覆盖「键=类别名/基名」的生产者口径（如键 "raw" 对应文件
// "raw.jsonl"）。两种都匹配不上时返回 ok=false（调用方必须报出「条数未核对」，
// 不许静默跳过这条检查）。
func matchCountsKey(counts map[string]int, name string) (string, int, bool) {
	if c, ok := counts[name]; ok {
		return name, c, true
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if base != "" && base != name {
		if c, ok := counts[base]; ok {
			return base, c, true
		}
	}
	return "", 0, false
}

// indexEntries 建立条目名索引（同名条目取第一个）并返回全部条目名与**重名条目名**。
//
// 重名条目必须报出来：zip 允许同名条目并存，而索引只保留第一个 ⇒ 清单里
// 声明的文件只被校验了**第一份**，第二份（内容可任意、摘要可对不上）不被任何
// 检查覆盖——按 zip 条目遍历（而不是按清单）的消费方会读到**未校验**的那份。
func indexEntries(zr *zip.Reader) (map[string]*zip.File, []string, []string) {
	entries := make(map[string]*zip.File, len(zr.File))
	names := make([]string, 0, len(zr.File))
	var dupes []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if _, ok := entries[f.Name]; !ok {
			entries[f.Name] = f
		} else {
			dupes = append(dupes, f.Name)
		}
	}
	return entries, names, dupes
}

// digestAndCount 流式计算条目的 SHA-256 摘要与行数（不把整个文件读进内存）。
func digestAndCount(f *zip.File) (string, int, error) {
	// 预检：条目**声明**的解压大小超限 ⇒ 直接拒绝，省掉实际解压。
	if f.UncompressedSize64 > maxEntrySize {
		return "", 0, fmt.Errorf("条目声明解压后 %d 字节，超过 %d 字节上限",
			f.UncompressedSize64, maxEntrySize)
	}
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
	total := int64(0)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			// 硬上限：声明大小可以撒谎，实际解压量用计数器卡住。
			total += int64(n)
			if total > maxEntrySize {
				return "", 0, fmt.Errorf("条目解压后超过 %d 字节上限", maxEntrySize)
			}
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
