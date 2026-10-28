package pack

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"

	"github.com/oekit/pkg/errors"
)

// Archive 是已打开的迁移包。
type Archive struct {
	zr       *zip.Reader
	manifest *Manifest
	entries  map[string]*zip.File
	names    []string
}

// Open 打开迁移包并读取清单。
//
// 清单格式版本必须与 CurrentFormatVersion 一致，否则返回包装 ErrUnsupportedVersion 的错误；
// zip 读不开或清单缺失/损坏时返回包装 ErrCorrupt 的错误。
func Open(r io.ReaderAt, size int64) (*Archive, error) {
	return OpenVersion(r, size, CurrentFormatVersion)
}

// OpenVersion 与 Open 相同，但显式指定调用方期望的清单格式版本。
func OpenVersion(r io.ReaderAt, size int64, expected int) (*Archive, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, newPackError(ErrCorrupt,
			fmt.Sprintf("pack: 迁移包损坏：zip 无法读取（%v）", err), err)
	}
	m, err := readManifest(zr, expected)
	if err != nil {
		return nil, err
	}
	entries, names, _ := indexEntries(zr)
	sort.Strings(names)
	return &Archive{zr: zr, manifest: m, entries: entries, names: names}, nil
}

// Manifest 返回包内清单。
func (a *Archive) Manifest() *Manifest { return a.manifest }

// Names 返回包内全部条目名（含清单），按字典序排列。
func (a *Archive) Names() []string {
	out := make([]string, len(a.names))
	copy(out, a.names)
	return out
}

// Verify 对已打开的包做体检，返回问题清单（与 Inspect 同一套规则）。
//
// 与 Inspect 不同的是：这里包已经打开成功，因此不会出现「包损坏 / 版本不兼容」两类错误，
// 只返回软问题清单（缺文件、摘要不符、条数不符、条数未核对、多余文件、非法文件名）。
func (a *Archive) Verify() []string {
	return verifyEntries(a.zr, a.manifest)
}

// ReadJSONL 按名读回 JSONL 行，逐行回调。
//
// 回调收到的是该行的原始 JSON（json.RawMessage），调用方自行解码；
// 回调返回错误会中断读取并原样带回（errors.Is 仍可判定回调自己的哨兵错误）。
// 空行会被跳过。
func (a *Archive) ReadJSONL(name string, fn func(json.RawMessage) error) error {
	if fn == nil {
		return errors.New("pack: 回调不能为空")
	}
	f, ok := a.entries[name]
	if !ok {
		return fmt.Errorf("%w: %s", ErrFileNotFound, name)
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("pack: 打开 %s 失败: %w", name, err)
	}
	defer rc.Close()

	br := bufio.NewReader(rc)
	lineNo := 0
	for {
		line, err := readLine(br, maxLineSize)
		if len(line) > 0 {
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) > 0 {
				lineNo++
				var raw json.RawMessage
				if uerr := json.Unmarshal(trimmed, &raw); uerr != nil {
					return fmt.Errorf("pack: %s 第 %d 行不是合法 JSON: %w", name, lineNo, uerr)
				}
				if ferr := fn(raw); ferr != nil {
					return fmt.Errorf("pack: %s 第 %d 行处理失败: %w", name, lineNo, ferr)
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("pack: 读取 %s 失败: %w", name, err)
		}
	}
}

// ReadJSONLInto 按名读回 JSONL 行并解码进 dst 指向的切片（追加语义）。
//
//	dst 必须是 *[]T 形式（指向切片的非空指针），每行按元素类型解码后追加。
func (a *Archive) ReadJSONLInto(name string, dst any) error {
	rv := reflect.ValueOf(dst)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errors.New("pack: dst 必须是非空指针（形如 *[]T）")
	}
	elem := rv.Elem()
	if elem.Kind() != reflect.Slice {
		return fmt.Errorf("pack: dst 必须指向切片，收到 %T", dst)
	}
	itemType := elem.Type().Elem()
	return a.ReadJSONL(name, func(raw json.RawMessage) error {
		item := reflect.New(itemType)
		if err := json.Unmarshal(raw, item.Interface()); err != nil {
			return err
		}
		elem.Set(reflect.Append(elem, item.Elem()))
		return nil
	})
}

// maxLineSize 限制 JSONL 的单行长度（8MB）。
//
// bufio.Reader.ReadBytes 会为超长行**无限增长**（bufio 的 4KB 缓冲不是上限）⇒
// 一个「一行几百 MB」的条目就能把内存吃满。
const maxLineSize = 8 << 20

// readLine 读一行（含换行符），超过 limit 字节即报错。
//
// bufio.Reader.ReadBytes 会为超长行无限增长 ⇒ 改用 ReadSlice + 手动拼接，
// 并在超限时立刻停手。
func readLine(br *bufio.Reader, limit int) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := br.ReadSlice('\n')
		buf = append(buf, chunk...)
		if len(buf) > limit {
			return nil, fmt.Errorf("单行超过 %d 字节上限", limit)
		}
		if err == nil {
			return buf, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return buf, err
	}
}
