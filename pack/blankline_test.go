package pack

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"testing"
)

// buildPackWithRaw 造一个「内容由调用方给定」的包（可含空行）。
func buildPackWithRaw(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestInspect_BlankLines_CountedAsRecords 回归测试：含**空行**的数据文件，
// 体检的「条数」必须等于**记录数**（非空行数），与读取侧 ReadJSONL 一致。
//
// 缺陷成因：体检侧 digestAndCount 数的是**物理行数**（每个 '\n' 记一次，空行
// 也算）⇒ 含空行的包会被报「条数不符」，而 ReadJSONL 读出来（跳过空行）
// 恰好等于清单里的数 ⇒ 体检与读取自相矛盾。
func TestInspect_BlankLines_CountedAsRecords(t *testing.T) {
	content := "{\"a\":1}\n\n{\"a\":2}\n\n\n{\"a\":3}\n"
	sum := sha256.Sum256([]byte(content))
	manifest := Manifest{
		FormatVersion: CurrentFormatVersion,
		Files:         map[string]string{"data.jsonl": hex.EncodeToString(sum[:])},
		Counts:        map[string]int{"data.jsonl": 3}, // 记录数（不含空行）
	}
	mb, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	raw := buildPackWithRaw(t, manifestName, string(mb))
	raw = appendEntry(t, raw, "data.jsonl", content)

	_, problems, err := Inspect(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	for _, p := range problems {
		if bytes.Contains([]byte(p), []byte("条数不符")) {
			t.Fatalf("含空行的包被误报条数不符：%v", problems)
		}
	}
}

// appendEntry 往已有的 zip 字节流里追加一个条目（保持其它条目不变）。
func appendEntry(t *testing.T, raw []byte, name, content string) []byte {
	t.Helper()
	var out bytes.Buffer
	zr, zrErr := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if zrErr != nil {
		t.Fatal(zrErr)
	}
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		rc, openErr := f.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		w, createErr := zw.Create(f.Name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, err := io.Copy(w, rc); err != nil {
			t.Fatal(err)
		}
		_ = rc.Close()
	}
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// TestDigestAndCount_RecordsVsLines 直接钉住计数口径：记录数（非空行数）。
func TestDigestAndCount_RecordsVsLines(t *testing.T) {
	cases := []struct {
		content string
		want    int
	}{
		{"{\"a\":1}\n{\"a\":2}\n", 2},
		{"{\"a\":1}\n\n{\"a\":2}\n", 2},    // 中间一个空行
		{"{\"a\":1}\n\n\n{\"a\":2}\n", 2},  // 连续空行
		{"\n\n{\"a\":1}\n", 1},             // 开头空行
		{"{\"a\":1}", 1},                   // 末行无换行
		{"\n\n\n", 0},                      // 全是空行
		{"{\"a\":1}\n   \n{\"a\":2}\n", 2}, // 空白行（只有空格）也算空
	}
	for _, c := range cases {
		raw := buildPackWithRaw(t, "data.jsonl", c.content)
		zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			t.Fatal(err)
		}
		_, got, err := digestAndCount(zr.File[0])
		if err != nil {
			t.Fatalf("digestAndCount: %v", err)
		}
		if got != c.want {
			t.Errorf("digestAndCount(%q) 记录数 = %d，期望 %d", c.content, got, c.want)
		}
	}
}

// TestReadJSONL_BlankLinesSkipped 回归护栏：读取侧跳过空行（既有口径）。
func TestReadJSONL_BlankLinesSkipped(t *testing.T) {
	mb, err := json.Marshal(Manifest{
		FormatVersion: CurrentFormatVersion,
		Files:         map[string]string{},
		Counts:        map[string]int{},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := buildPackWithRaw(t, "data.jsonl", "{\"a\":1}\n\n{\"a\":2}\n")
	// 把清单也塞进包里（buildPackWithRaw 只造一个条目，这里重造一个完整的）。
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(manifestName)
	_, _ = w.Write(mb)
	w2, _ := zw.Create("data.jsonl")
	_, _ = w2.Write([]byte("{\"a\":1}\n\n{\"a\":2}\n"))
	_ = zw.Close()

	arc, err := Open(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
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
	if got != 2 {
		t.Fatalf("读到 %d 行，期望 2（空行应跳过）", got)
	}
	_ = raw
}
