package pack

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"maps"
	"sync"
	"time"

	"github.com/oekit/pkg/encoding"
	"github.com/oekit/pkg/errors"
)

// Manifest 描述迁移包的结构与内容。
type Manifest struct {
	// FormatVersion 是清单格式版本，写入方与读取方必须一致（当前为 CurrentFormatVersion）。
	FormatVersion int `json:"format_version"`
	// CreatedAt 是包的创建时间（UTC），取自 Writer 的时刻来源
	// （缺省 time.Now，可用 WithClock 注入）。
	CreatedAt time.Time `json:"created_at"`
	// Counts 记录每个数据文件的行数（文件名为键）。
	Counts map[string]int `json:"counts"`
	// Files 记录每个数据文件的 SHA-256 十六进制摘要（文件名为键）。
	Files map[string]string `json:"files"`
}

// Writer 逐个写入迁移包的数据文件。
//
// Writer 不是并发安全的；一个包由一个 Writer 顺序写出。
type Writer struct {
	mu       sync.Mutex
	zw       *zip.Writer
	version  int
	counts   map[string]int
	files    map[string]string
	now      func() time.Time
	created  time.Time
	closed   bool
	err      error
	manifest *Manifest
}

// Option Writer 可选项。
type Option func(*Writer)

// WithClock 注入清单 created_at 的时刻来源（缺省 time.Now）。
//
// 清单里的 created_at 落在包里（下游拿它判断「何时导出」「两个包哪个新」），
// 写死时刻会让导出逻辑没法做确定性单测、同一份数据反复导出不可复现——
// 注入固定时钟即可得到逐字节可复现的清单。
func WithClock(now func() time.Time) Option {
	return func(w *Writer) { w.now = now }
}

// NewWriter 创建迁移包写入器，底层数据写入 w。
//
// formatVersion 是清单格式版本，应传 CurrentFormatVersion；版本会被写进清单，
// 读取方据此判断能否识别该包。
//
// 可选项 WithClock 注入清单 created_at 的时刻来源（缺省 time.Now）。
func NewWriter(w io.Writer, formatVersion int, opts ...Option) *Writer {
	wr := &Writer{
		zw:      zip.NewWriter(w),
		version: formatVersion,
		counts:  make(map[string]int),
		files:   make(map[string]string),
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(wr)
	}
	if wr.now == nil {
		wr.now = time.Now
	}
	// created_at 的时刻只从注入的时钟取（全包仅此一处），别处不再调 time.Now。
	wr.created = wr.now()
	return wr
}

// AddJSONL 把 rows 逐行编码成 JSONL 写入包内名为 name 的条目，返回写入行数。
//
// 行是逐条编码、逐条写出的（不把整个行集合攒进内存），适合大结果集导出。
// 写入成功后该文件的 SHA-256 摘要与行数会记入清单（Close 时落盘）。
//
// name 必须是安全的普通文件名：含路径分隔符、含 ".."、绝对路径的会被拒绝。
func (w *Writer) AddJSONL(name string, rows any) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, errors.New("pack: Writer 已关闭")
	}
	if w.err != nil {
		return 0, w.err
	}
	if w.version <= 0 {
		w.err = errors.New("pack: 格式版本必须为正整数")
		return 0, w.err
	}
	safe, err := sanitizeName(name)
	if err != nil {
		return 0, err
	}
	if _, dup := w.files[safe]; dup {
		return 0, fmt.Errorf("pack: 文件名 %q 重复", safe)
	}
	iter, err := newRowIter(rows)
	if err != nil {
		return 0, err
	}

	f, err := w.zw.Create(safe)
	if err != nil {
		w.err = fmt.Errorf("pack: 创建条目 %s 失败: %w", safe, err)
		return 0, w.err
	}
	dw := &digestWriter{w: f, h: sha256.New()}

	n := 0
	for iter.Next() {
		line, err := json.Marshal(iter.Value())
		if err != nil {
			w.err = fmt.Errorf("pack: 编码 %s 第 %d 行失败: %w", safe, n+1, err)
			return 0, w.err
		}
		if _, err := dw.Write(line); err != nil {
			w.err = fmt.Errorf("pack: 写入 %s 第 %d 行失败: %w", safe, n+1, err)
			return 0, w.err
		}
		if _, err := dw.Write([]byte{'\n'}); err != nil {
			w.err = fmt.Errorf("pack: 写入 %s 第 %d 行失败: %w", safe, n+1, err)
			return 0, w.err
		}
		n++
	}
	if err := iter.Err(); err != nil {
		w.err = err
		return 0, err
	}

	w.counts[safe] = n
	w.files[safe] = hex.EncodeToString(dw.Sum(nil))
	return n, nil
}

// Close 写入清单并收尾 zip，返回最终清单。
//
// 清单以固定文件名 manifest.json 放在包内（自身不计入清单的文件列表）。
// Close 只能调用一次，重复调用返回错误。
func (w *Writer) Close() (*Manifest, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil, errors.New("pack: Writer 已关闭")
	}
	if w.err != nil {
		return nil, w.err
	}

	m := &Manifest{
		FormatVersion: w.version,
		CreatedAt:     w.created.UTC(),
		Counts:        maps.Clone(w.counts),
		Files:         maps.Clone(w.files),
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		w.err = fmt.Errorf("pack: 序列化清单失败: %w", err)
		return nil, w.err
	}
	data = append(data, '\n')

	f, err := w.zw.Create(manifestName)
	if err != nil {
		w.err = fmt.Errorf("pack: 写入清单失败: %w", err)
		return nil, w.err
	}
	if _, err := f.Write(data); err != nil {
		w.err = fmt.Errorf("pack: 写入清单失败: %w", err)
		return nil, w.err
	}
	if err := w.zw.Close(); err != nil {
		w.err = fmt.Errorf("pack: 关闭 zip 失败: %w", err)
		return nil, w.err
	}

	w.closed = true
	w.manifest = m
	return m, nil
}

// DigestHex 返回内容的 SHA-256 十六进制摘要。
//
// 与 github.com/oekit/pkg/encoding 的 Sha256Hex 同型；包内流式写入/读取时使用增量
// hasher 累积，摘要值与该函数逐字节一致（契约测试 TestDigestMatchesEncoding 锁定）。
func DigestHex(data []byte) string {
	return encoding.Sha256Hex(data)
}

// digestWriter 把写入的数据同时喂给 SHA-256 增量 hasher。
type digestWriter struct {
	w io.Writer
	h hash.Hash
}

// Write 写入数据并同步更新摘要。
func (d *digestWriter) Write(p []byte) (int, error) {
	n, err := d.w.Write(p)
	if n > 0 {
		d.h.Write(p[:n])
	}
	return n, err
}

// Sum 把当前累计内容的 SHA-256 摘要追加到 b 并返回。
func (d *digestWriter) Sum(b []byte) []byte {
	return d.h.Sum(b)
}
