package pack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/oekit/pkg/encoding"
	"github.com/oekit/pkg/errors"
)

// userRow 是往返测试用的行结构。
type userRow struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// TestRoundTripJSONL 契约：同形状的数据导出再导入，逐条相等。
func TestRoundTripJSONL(t *testing.T) {
	cases := []struct {
		name  string
		rows  any
		lines []string
	}{
		{
			name:  "map 切片",
			rows:  []map[string]any{{"id": 1, "name": "a"}, {"id": 2, "name": "b"}},
			lines: []string{`{"id":1,"name":"a"}`, `{"id":2,"name":"b"}`},
		},
		{
			name:  "any 切片",
			rows:  []any{map[string]any{"id": 1}, "文本", 42, nil, true},
			lines: []string{`{"id":1}`, `"文本"`, `42`, `null`, `true`},
		},
		{
			name:  "结构体切片",
			rows:  []userRow{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}},
			lines: []string{`{"id":1,"name":"a"}`, `{"id":2,"name":"b"}`},
		},
		{
			name:  "标量切片",
			rows:  []int{1, 2, 3},
			lines: []string{`1`, `2`, `3`},
		},
		{
			name:  "数组",
			rows:  [1]userRow{{ID: 7, Name: "g"}},
			lines: []string{`{"id":7,"name":"g"}`},
		},
		{
			name:  "空切片",
			rows:  []userRow{},
			lines: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, m := buildPack(t, CurrentFormatVersion, map[string]any{"rows.jsonl": tc.rows})

			arc, err := Open(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if arc.Manifest() == nil {
				t.Fatal("Manifest 不应为 nil")
			}

			var got []json.RawMessage
			if err := arc.ReadJSONL("rows.jsonl", func(row json.RawMessage) error {
				got = append(got, append(json.RawMessage(nil), row...))
				return nil
			}); err != nil {
				t.Fatalf("ReadJSONL: %v", err)
			}

			if len(got) != len(tc.lines) {
				t.Fatalf("读回行数 %d，期望 %d", len(got), len(tc.lines))
			}
			for i := range tc.lines {
				if !jsonEqual(t, got[i], []byte(tc.lines[i])) {
					t.Errorf("第 %d 行 %s，期望 %s", i+1, got[i], tc.lines[i])
				}
			}
			if m.Counts["rows.jsonl"] != len(tc.lines) {
				t.Errorf("清单条数 %d，期望 %d", m.Counts["rows.jsonl"], len(tc.lines))
			}
			if _, ok := m.Files["rows.jsonl"]; !ok {
				t.Error("清单应记录 rows.jsonl 的摘要")
			}
		})
	}
}

// TestRoundTripReadJSONLInto 契约：导入端按目标结构解码，数据逐条相等。
func TestRoundTripReadJSONLInto(t *testing.T) {
	want := []userRow{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}}
	data, _ := buildPack(t, CurrentFormatVersion, map[string]any{"users.jsonl": want})

	arc, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var got []userRow
	if err := arc.ReadJSONLInto("users.jsonl", &got); err != nil {
		t.Fatalf("ReadJSONLInto: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("导入结果 %v，期望 %v", got, want)
	}

	// dst 不合法时报错
	if err := arc.ReadJSONLInto("users.jsonl", got); err == nil {
		t.Error("dst 非指针应报错")
	}
	var notSlice int
	if err := arc.ReadJSONLInto("users.jsonl", &notSlice); err == nil {
		t.Error("dst 非切片应报错")
	}
}

// TestInspectVersionMismatch 契约：版本不匹配 → errors.Is(err, ErrUnsupportedVersion)。
func TestInspectVersionMismatch(t *testing.T) {
	data, _ := buildPack(t, 7, map[string]any{"rows.jsonl": []int{1}})

	_, problems, err := Inspect(bytes.NewReader(data), int64(len(data)))
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("期望 ErrUnsupportedVersion，实际 %v", err)
	}
	if errors.Is(err, ErrCorrupt) {
		t.Error("版本不匹配不应被判为包损坏")
	}
	msg := err.Error()
	if !strings.Contains(msg, "7") || !strings.Contains(msg, "1") {
		t.Errorf("错误信息应带实际版本与期望版本：%v", err)
	}
	if !hasProblem(problems, "版本") {
		t.Errorf("问题清单应含版本不匹配：%v", problems)
	}

	// 期望版本一致时不报错
	if _, _, err := InspectVersion(bytes.NewReader(data), int64(len(data)), 7); err != nil {
		t.Errorf("期望版本一致时不应报错：%v", err)
	}
	// Open 同样区分版本
	if _, err := Open(bytes.NewReader(data), int64(len(data))); !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("Open 期望 ErrUnsupportedVersion，实际 %v", err)
	}
}

// TestInspectCorrupt 契约：zip 损坏 / 清单缺失 → errors.Is(err, ErrCorrupt)。
func TestInspectCorrupt(t *testing.T) {
	t.Run("不是 zip", func(t *testing.T) {
		bad := []byte("this is definitely not a zip file")
		_, _, err := Inspect(bytes.NewReader(bad), int64(len(bad)))
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("期望 ErrCorrupt，实际 %v", err)
		}
	})

	t.Run("缺少清单", func(t *testing.T) {
		data := buildRawZip(t, map[string][]byte{"rows.jsonl": []byte("{}\n")})
		_, _, err := Inspect(bytes.NewReader(data), int64(len(data)))
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("期望 ErrCorrupt，实际 %v", err)
		}
		if !strings.Contains(err.Error(), manifestName) {
			t.Errorf("错误信息应点名清单文件：%v", err)
		}
	})

	t.Run("清单 JSON 损坏", func(t *testing.T) {
		data := buildRawZip(t, map[string][]byte{manifestName: []byte("{not json")})
		_, _, err := Inspect(bytes.NewReader(data), int64(len(data)))
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("期望 ErrCorrupt，实际 %v", err)
		}
	})

	t.Run("包被截断", func(t *testing.T) {
		data, _ := buildPack(t, CurrentFormatVersion, map[string]any{"rows.jsonl": []int{1}})
		cut := data[:len(data)/2]
		_, _, err := Inspect(bytes.NewReader(cut), int64(len(cut)))
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("期望 ErrCorrupt，实际 %v", err)
		}
	})

	t.Run("Open 同样报损坏", func(t *testing.T) {
		bad := []byte("garbage")
		if _, err := Open(bytes.NewReader(bad), int64(len(bad))); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("期望 ErrCorrupt，实际 %v", err)
		}
	})
}

// TestInspectListsAllProblems 契约：缺文件、摘要不符、条数不符要一次性列全，不是遇到第一个就停。
func TestInspectListsAllProblems(t *testing.T) {
	data, m := buildPack(t, CurrentFormatVersion, map[string]any{
		"a.jsonl": []map[string]any{{"id": 1}},
		"b.jsonl": []map[string]any{{"id": 2}},
	})

	entries := readEntries(t, data)
	delete(entries, "a.jsonl")                    // 缺文件
	entries["b.jsonl"] = []byte("{\"id\":999}\n") // 摘要不符
	var tampered Manifest
	if err := json.Unmarshal(entries[manifestName], &tampered); err != nil {
		t.Fatalf("解析清单: %v", err)
	}
	tampered.Counts["b.jsonl"] = 42 // 条数不符
	mb, err := json.Marshal(tampered)
	if err != nil {
		t.Fatalf("序列化清单: %v", err)
	}
	entries[manifestName] = mb

	bad := buildRawZip(t, entries)
	gotManifest, problems, err := Inspect(bytes.NewReader(bad), int64(len(bad)))
	if err != nil {
		t.Fatalf("软问题不应返回错误：%v", err)
	}
	if gotManifest == nil {
		t.Fatal("清单不应为 nil")
	}
	if len(problems) < 3 {
		t.Fatalf("问题清单应至少 3 条，实际 %d：%v", len(problems), problems)
	}
	if !hasProblem(problems, "缺少文件：a.jsonl") {
		t.Errorf("问题清单应含缺失文件：%v", problems)
	}
	if !hasProblem(problems, "摘要不符：b.jsonl") {
		t.Errorf("问题清单应含摘要不符：%v", problems)
	}
	if !hasProblem(problems, "条数不符：b.jsonl") {
		t.Errorf("问题清单应含条数不符：%v", problems)
	}

	// 原始包本身体检全绿
	_, okProblems, err := Inspect(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(okProblems) != 0 {
		t.Errorf("正常包不应有问题，实际 %v", okProblems)
	}
	_ = m
}

// TestSanitizeNames 契约：含 ".." / 绝对路径 / 路径分隔符的名字被拒绝。
func TestSanitizeNames(t *testing.T) {
	bad := []string{
		"",
		"../evil.jsonl",
		"..",
		"a..b.jsonl",
		"/etc/passwd",
		"sub/dir.jsonl",
		`sub\dir.jsonl`,
		`C:\data.jsonl`,
		"C:data.jsonl",
		" leading.jsonl",
		"trailing.jsonl ",
		"ctrl\x01.jsonl",
		manifestName,
	}

	var buf bytes.Buffer
	w := NewWriter(&buf, CurrentFormatVersion)
	for _, name := range bad {
		if _, err := w.AddJSONL(name, []int{1}); err == nil {
			t.Errorf("文件名 %q 应被拒绝", name)
		}
	}
	// 合法名仍可写
	if _, err := w.AddJSONL("ok.jsonl", []int{1}); err != nil {
		t.Errorf("合法文件名应可写：%v", err)
	}
	if _, err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestInspectRejectsUnsafeEntryNames 契约：读到含穿越路径的条目名时报进问题清单。
func TestInspectRejectsUnsafeEntryNames(t *testing.T) {
	good, _ := buildPack(t, CurrentFormatVersion, map[string]any{"ok.jsonl": []int{1}})
	entries := readEntries(t, good)
	entries["../evil.jsonl"] = []byte("{\"id\":1}\n")

	bad := buildRawZip(t, entries)
	_, problems, err := Inspect(bytes.NewReader(bad), int64(len(bad)))
	if err != nil {
		t.Fatalf("非法条目名属于软问题，不应返回错误：%v", err)
	}
	if !hasProblem(problems, "非法文件名") {
		t.Errorf("问题清单应含非法文件名：%v", problems)
	}
	if !hasProblem(problems, "../evil.jsonl") {
		t.Errorf("问题清单应点名问题条目：%v", problems)
	}
}

// TestStreamingLargeDataset 契约：逐行编码、流式写出——写完即落到底层 writer，
// 内容与独立拼出的期望字节完全一致，摘要与 pkg/encoding 一致。
func TestStreamingLargeDataset(t *testing.T) {
	const n = 20000

	rows := make([]map[string]any, 0, n)
	var want bytes.Buffer
	for i := 0; i < n; i++ {
		row := map[string]any{"id": i, "name": fmt.Sprintf("user-%d", i)}
		rows = append(rows, row)
		line, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		want.Write(line)
		want.WriteByte('\n')
	}

	cw := &countingWriter{}
	w := NewWriter(cw, CurrentFormatVersion)
	got, err := w.AddJSONL("big.jsonl", rows)
	if err != nil {
		t.Fatalf("AddJSONL: %v", err)
	}
	if got != n {
		t.Fatalf("写入行数 %d，期望 %d", got, n)
	}
	// 数据在 Close 之前就已落到底层 writer（说明是流式写出，不在内存里攒到最后）
	if cw.n() == 0 {
		t.Error("AddJSONL 返回后底层 writer 应已收到数据（流式写出）")
	}
	m, err := w.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if m.Counts["big.jsonl"] != n {
		t.Errorf("清单条数 %d，期望 %d", m.Counts["big.jsonl"], n)
	}
	if m.Files["big.jsonl"] != encoding.Sha256Hex(want.Bytes()) {
		t.Error("清单摘要应与 encoding.Sha256Hex 一致")
	}
	// 包内条目解压后与独立拼出的内容逐字节一致
	zr, err := zip.NewReader(bytes.NewReader(cw.buf.Bytes()), int64(cw.buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	var entry *zip.File
	for _, f := range zr.File {
		if f.Name == "big.jsonl" {
			entry = f
		}
	}
	if entry == nil {
		t.Fatal("包内缺少 big.jsonl")
	}
	if uint64(want.Len()) != entry.UncompressedSize64 {
		t.Errorf("解压后大小 %d，期望 %d", entry.UncompressedSize64, want.Len())
	}
	gotBytes := readEntries(t, cw.buf.Bytes())["big.jsonl"]
	if !bytes.Equal(gotBytes, want.Bytes()) {
		t.Error("包内内容与独立拼出的期望内容不一致")
	}
}

// TestDigestMatchesEncoding 契约：包内摘要与 github.com/oekit/pkg/encoding 的摘要同值。
func TestDigestMatchesEncoding(t *testing.T) {
	rows := []map[string]any{{"id": 1}, {"id": 2}}
	data, m := buildPack(t, CurrentFormatVersion, map[string]any{"rows.jsonl": rows})

	entries := readEntries(t, data)
	if m.Files["rows.jsonl"] != encoding.Sha256Hex(entries["rows.jsonl"]) {
		t.Fatal("清单摘要应与 encoding.Sha256Hex(文件内容) 一致")
	}
	if DigestHex([]byte("abc")) != encoding.Sha256Hex([]byte("abc")) {
		t.Fatal("DigestHex 应与 encoding.Sha256Hex 一致")
	}
}

// TestReadMissingFile 契约：读不存在的文件返回可判定错误。
func TestReadMissingFile(t *testing.T) {
	data, _ := buildPack(t, CurrentFormatVersion, map[string]any{"rows.jsonl": []int{1}})
	arc, err := Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	err = arc.ReadJSONL("nope.jsonl", func(json.RawMessage) error { return nil })
	if !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("期望 ErrFileNotFound，实际 %v", err)
	}
}

// TestArchiveVerify 契约：Archive.Verify 与 Inspect 给出同样的问题清单。
func TestArchiveVerify(t *testing.T) {
	data, _ := buildPack(t, CurrentFormatVersion, map[string]any{
		"a.jsonl": []map[string]any{{"id": 1}},
		"b.jsonl": []map[string]any{{"id": 2}},
	})
	entries := readEntries(t, data)
	delete(entries, "a.jsonl")
	entries["b.jsonl"] = []byte("{\"id\":999}\n")
	bad := buildRawZip(t, entries)

	arc, err := Open(bytes.NewReader(bad), int64(len(bad)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	problems := arc.Verify()
	if !hasProblem(problems, "缺少文件：a.jsonl") {
		t.Errorf("Verify 应报缺失文件：%v", problems)
	}
	if !hasProblem(problems, "摘要不符：b.jsonl") {
		t.Errorf("Verify 应报摘要不符：%v", problems)
	}
}

// TestInspectCountsKeyVariants 契约（pkg#32）：条数核对覆盖三种 counts 键口径——
// 文件名（精确）、基名（去扩展名，宽松）、完全对不上的名字；counts 缺键时同样
// 必须报出「条数未核对」。
//
// 回归背景：旧实现只在 counts 的键恰好是文件名时才核对条数；键对不上就
// **静默跳过**这条检查、还不给任何提示——体检报「通过」，实际从没核对过。
func TestInspectCountsKeyVariants(t *testing.T) {
	const fileName = "raw.jsonl"
	rows := []map[string]any{{"id": 1}, {"id": 2}, {"id": 3}} // 真实 3 行

	tests := []struct {
		name string
		// counts 是清单里的 counts 条目（键口径由生产者决定）。
		counts map[string]int
		// want 是期望出现的问题片段（至少命中一个）。
		want []string
		// notWant 是**不得**出现的问题片段（不得给出假结论）。
		notWant []string
	}{
		{
			name:    "键=文件名",
			counts:  map[string]int{fileName: 2},
			want:    []string{"条数不符：raw.jsonl"},
			notWant: []string{"条数未核对", "未匹配任何数据文件"},
		},
		{
			name:    "键=基名（去扩展名）",
			counts:  map[string]int{"raw": 2},
			want:    []string{"条数不符：raw.jsonl"},
			notWant: []string{"条数未核对", "未匹配任何数据文件"},
		},
		{
			name:    "键=完全对不上的名字",
			counts:  map[string]int{"mystery": 2},
			want:    []string{"条数未核对", "未匹配任何数据文件"},
			notWant: []string{"条数不符"},
		},
		{
			name:    "counts 缺该键",
			counts:  map[string]int{},
			want:    []string{"条数未核对"},
			notWant: []string{"条数不符"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildPackWithCounts(t, fileName, rows, tt.counts)
			_, problems, err := Inspect(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Inspect: %v", err)
			}
			for _, w := range tt.want {
				if !hasProblem(problems, w) {
					t.Errorf("问题清单应含 %q：%v", w, problems)
				}
			}
			for _, nw := range tt.notWant {
				if hasProblem(problems, nw) {
					t.Errorf("问题清单不应含 %q：%v", nw, problems)
				}
			}
		})
	}
}

// TestInspectCountsKeyVariantsCountOK 基名口径下条数**相符**时不得误报。
func TestInspectCountsKeyVariantsCountOK(t *testing.T) {
	data := buildPackWithCounts(t, "raw.jsonl",
		[]map[string]any{{"id": 1}, {"id": 2}, {"id": 3}},
		map[string]int{"raw": 3})
	_, problems, err := Inspect(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(problems) != 0 {
		t.Errorf("条数相符的包不应有问题，实际 %v", problems)
	}
}

// TestWriterClockInjection 契约（pkg#33）：清单 created_at 的时刻来源可注入——
// 注入固定时钟 → created_at 精确等于注入时刻；不注入 → 当前时间；
// 同一份数据 + 同一固定时钟导出两次 → 两份清单逐字节相等（可复现）。
func TestWriterClockInjection(t *testing.T) {
	fixed := time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)

	// 注入固定时钟 → created_at 精确等于注入时刻（不是「大致是现在」）。
	var buf bytes.Buffer
	w := NewWriter(&buf, CurrentFormatVersion, WithClock(func() time.Time { return fixed }))
	if _, err := w.AddJSONL("rows.jsonl", []map[string]any{{"id": 1}, {"id": 2}}); err != nil {
		t.Fatalf("AddJSONL: %v", err)
	}
	m, err := w.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !m.CreatedAt.Equal(fixed) {
		t.Errorf("created_at = %v, want %v", m.CreatedAt, fixed)
	}
	if got := m.CreatedAt.UTC().Format(time.RFC3339Nano); got != "2026-10-20T03:00:00Z" {
		t.Errorf("created_at 格式化 = %q, want %q", got, "2026-10-20T03:00:00Z")
	}

	// 不注入 → 仍是当前时间（不报错）。
	var buf2 bytes.Buffer
	w2 := NewWriter(&buf2, CurrentFormatVersion) // 旧的两参写法仍可用（向后兼容）
	before := time.Now().UTC().Add(-time.Second)
	if _, addErr := w2.AddJSONL("rows.jsonl", []int{1}); addErr != nil {
		t.Fatalf("AddJSONL: %v", addErr)
	}
	m2, err := w2.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if m2.CreatedAt.Before(before) || m2.CreatedAt.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("未注入时钟时 created_at 应是当前时间，实际 %v", m2.CreatedAt)
	}

	// 注入 nil → 退化为 time.Now，不 panic。
	var buf3 bytes.Buffer
	w3 := NewWriter(&buf3, CurrentFormatVersion, WithClock(nil))
	if _, err := w3.AddJSONL("rows.jsonl", []int{1}); err != nil {
		t.Fatalf("AddJSONL: %v", err)
	}
	if _, err := w3.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 同一份数据 + 同一固定时钟 → 两份清单逐字节相等（可复现）。
	build := func() []byte {
		var b bytes.Buffer
		w := NewWriter(&b, CurrentFormatVersion, WithClock(func() time.Time { return fixed }))
		if _, err := w.AddJSONL("rows.jsonl", []map[string]any{{"id": 1}, {"id": 2}}); err != nil {
			t.Fatalf("AddJSONL: %v", err)
		}
		if _, err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		return b.Bytes()
	}
	p1, p2 := build(), build()
	mb1 := readEntries(t, p1)[manifestName]
	mb2 := readEntries(t, p2)[manifestName]
	if !bytes.Equal(mb1, mb2) {
		t.Errorf("同一固定时钟下两份清单应逐字节相等:\n--- 1 ---\n%s\n--- 2 ---\n%s", mb1, mb2)
	}
}

// --- 测试辅助 ---

// buildPackWithCounts 造一个包，并把清单的 counts 换成给定的键值对
// （用来模拟「键=类别名/基名」等不同生产口径）。
func buildPackWithCounts(t *testing.T, name string, rows any, counts map[string]int) []byte {
	t.Helper()
	data, _ := buildPack(t, CurrentFormatVersion, map[string]any{name: rows})
	entries := readEntries(t, data)
	var m Manifest
	if err := json.Unmarshal(entries[manifestName], &m); err != nil {
		t.Fatalf("解析清单: %v", err)
	}
	m.Counts = counts
	mb, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("序列化清单: %v", err)
	}
	entries[manifestName] = mb
	return buildRawZip(t, entries)
}

// buildPack 造一个迁移包，返回包字节与清单。
func buildPack(t *testing.T, version int, files map[string]any) ([]byte, *Manifest) {
	t.Helper()
	var buf bytes.Buffer
	w := NewWriter(&buf, version)
	for _, name := range sortedKeys(files) {
		if _, err := w.AddJSONL(name, files[name]); err != nil {
			t.Fatalf("AddJSONL(%s): %v", name, err)
		}
	}
	m, err := w.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes(), m
}

// buildRawZip 直接造一个 zip（不经过 Writer），用于构造坏包。
func buildRawZip(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range sortedKeys(entries) {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
		if _, err := f.Write(entries[name]); err != nil {
			t.Fatalf("Write(%s): %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	return buf.Bytes()
}

// readEntries 读出包内全部条目内容。
func readEntries(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	out := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("Open(%s): %v", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("ReadAll(%s): %v", f.Name, err)
		}
		out[f.Name] = b
	}
	return out
}

// countingWriter 记录已写出的字节数与内容。
type countingWriter struct {
	buf bytes.Buffer
}

// Write 写入并记录内容。
func (c *countingWriter) Write(p []byte) (int, error) {
	return c.buf.Write(p)
}

// n 返回已写出的字节数。
func (c *countingWriter) n() int {
	return c.buf.Len()
}

// hasProblem 判断问题清单里是否有包含 sub 的条目。
func hasProblem(problems []string, sub string) bool {
	for _, p := range problems {
		if strings.Contains(p, sub) {
			return true
		}
	}
	return false
}

// jsonEqual 比较两段 JSON 的语义是否相等。
func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("解析 %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("解析 %s: %v", b, err)
	}
	return reflect.DeepEqual(av, bv)
}
