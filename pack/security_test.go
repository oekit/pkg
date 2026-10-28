package pack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// buildZip 造一个 zip：entries 是「名 → 内容」，顺序即写入顺序（可重复名）。
func buildZip(t *testing.T, entries [][2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestInspect_DuplicateEntry_Reported 回归测试：zip 内**同名条目并存**必须被
// 报出来。
//
// 缺陷成因：indexEntries 对同名条目只保留**第一个**，清单里声明的文件只被
// 校验了第一份；第二份（内容可任意）不被任何检查覆盖——按 zip 条目遍历（而
// 不是按清单）的消费方会读到**未校验**的那份。
//
// 此前「清单未记录的文件」这一轮检查按名字判断，重名条目在清单里 ⇒ 被跳过 ⇒
// 完全不报。
func TestInspect_DuplicateEntry_Reported(t *testing.T) {
	// 构造一个自洽的包（有清单），再往里塞一份同名但内容不同的数据文件。
	// 先用正常 API 造一个包，拿到清单内容。
	var src bytes.Buffer
	zw := zip.NewWriter(&src)
	w, _ := zw.Create("data.jsonl")
	_, _ = w.Write([]byte("{\"a\":1}\n"))
	zw.Close()

	// 读出清单，构造一个与 data.jsonl 摘要一致的清单比较复杂——
	// 这里换个更直接的路径：手工造清单，让摘要对不上也不影响「重名」这条结论。
	manifest := Manifest{
		FormatVersion: CurrentFormatVersion,
		Files:         map[string]string{},
		Counts:        map[string]int{},
	}
	mb, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}

	raw := buildZip(t, [][2]string{
		{manifestName, string(mb)},
		{"data.jsonl", "{\"a\":1}\n"},
		{"data.jsonl", "{\"a\":999}\n"}, // 同名第二份，内容不同
	})

	_, problems, err := Inspect(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	found := false
	for _, p := range problems {
		if strings.Contains(p, "重名条目") && strings.Contains(p, "data.jsonl") {
			found = true
		}
	}
	if !found {
		t.Fatalf("重名条目未被报出，problems=%v", problems)
	}
}

// TestInspect_DuplicateManifest_Reported 清单自身重名同样要报。
func TestInspect_DuplicateManifest_Reported(t *testing.T) {
	mb, err := json.Marshal(Manifest{
		FormatVersion: CurrentFormatVersion,
		Files:         map[string]string{},
		Counts:        map[string]int{},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := buildZip(t, [][2]string{
		{manifestName, string(mb)},
		{manifestName, string(mb)},
	})
	_, problems, err := Inspect(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	found := false
	for _, p := range problems {
		if strings.Contains(p, "重名条目") && strings.Contains(p, manifestName) {
			found = true
		}
	}
	if !found {
		t.Fatalf("清单重名未被报出，problems=%v", problems)
	}
}

// TestDigestAndCount_SizeLimit 回归测试：条目**声明**的解压大小超上限时必须
// 直接拒绝，而不是实际去解压（zip bomb）。
//
// 缺陷成因：清单有 maxManifestSize 保护，数据条目没有——Inspect 要把每个
// 声明条目完整解压一遍算摘要与行数 ⇒ 「几 KB 的 zip、条目声明 1TB」的包能让
// 体检无限期烧 CPU。
func TestDigestAndCount_SizeLimit(t *testing.T) {
	// 造一个声明解压大小巨大的条目：zip 的 UncompressedSize64 来自写入时的
	// 数据大小，真实构造 1TB 不现实 —— 直接构造 zip.File 结构体来测预检分支。
	// UncompressedSize64 是 zip.File 内嵌 FileHeader 的字段（提升字段，
	// 结构体字面量里只能经 FileHeader 设置）。
	f := &zip.File{
		FileHeader: zip.FileHeader{Name: "big.jsonl", UncompressedSize64: maxEntrySize + 1},
	}
	_, _, err := digestAndCount(f)
	if err == nil {
		t.Fatal("声明解压大小超限的条目应被拒绝")
	}
	if !strings.Contains(err.Error(), "上限") {
		t.Fatalf("错误信息未说明是上限问题：%v", err)
	}
}

// TestReadJSONL_LineTooLong 回归测试：超长行必须报错，而不是把内存吃满。
//
// 缺陷成因：bufio.Reader.ReadBytes('\n') 为超长行**无限增长**（bufio 的 4KB
// 缓冲不是上限）⇒ 一行几百 MB 的条目就能把内存吃满。
func TestReadJSONL_LineTooLong(t *testing.T) {
	long := strings.Repeat("x", maxLineSize+1)
	raw := buildZip(t, [][2]string{
		{manifestName, `{"format_version":1,"files":{},"counts":{}}`},
		{"data.jsonl", long},
	})
	arc, err := Open(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	err = arc.ReadJSONL("data.jsonl", func(json.RawMessage) error { return nil })
	if err == nil {
		t.Fatal("超长行应报错")
	}
	if !strings.Contains(err.Error(), "上限") {
		t.Fatalf("错误信息未说明是上限问题：%v", err)
	}
}

// TestReadJSONL_NormalLinesUnaffected 正常行照旧能读（回归护栏）。
func TestReadJSONL_NormalLinesUnaffected(t *testing.T) {
	mb, err := json.Marshal(Manifest{
		FormatVersion: CurrentFormatVersion,
		Files:         map[string]string{},
		Counts:        map[string]int{},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := buildZip(t, [][2]string{
		{manifestName, string(mb)},
		{"data.jsonl", "{\"a\":1}\n{\"a\":2}\n\n{\"a\":3}\n"},
	})
	arc, err := Open(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var got int
	err = arc.ReadJSONL("data.jsonl", func(json.RawMessage) error {
		got++
		return nil
	})
	if err != nil {
		t.Fatalf("ReadJSONL: %v", err)
	}
	if got != 3 {
		t.Fatalf("读到 %d 行，期望 3（空行应跳过）", got)
	}
}

// TestReadJSONL_CallbackErrorPreserved 回调错误原样带回（既有口径，回归护栏）。
func TestReadJSONL_CallbackErrorPreserved(t *testing.T) {
	mb, err := json.Marshal(Manifest{
		FormatVersion: CurrentFormatVersion,
		Files:         map[string]string{},
		Counts:        map[string]int{},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := buildZip(t, [][2]string{
		{manifestName, string(mb)},
		{"data.jsonl", "{\"a\":1}\n"},
	})
	arc, err := Open(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("业务失败")
	err = arc.ReadJSONL("data.jsonl", func(json.RawMessage) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("回调错误应原样带回，实得 %v", err)
	}
}
