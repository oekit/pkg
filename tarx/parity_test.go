package tarx

// 奇偶测试与基础口径测试（#326 验收 2）：Scan 与 Write 对同一棵树的
// Stats 逐字段相等——预扫与流式写入器「各走各的分类」的口径漂移被钉死；
// Write↔Extract round-trip 内容逐字节一致。

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// makeTree 构建确定性的测试树：多层目录、不同大小文件（含 0 字节）、
// 空目录；unix 下再加 symlink 与 unix socket（流会丢下的非常规条目）。
// 返回文件数/目录数/字节数的期望值。
func makeTree(t *testing.T) (root string, want Stats) {
	t.Helper()
	root = t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("file1.txt", "0123456789")                          // 10B
	write("empty.txt", "")                                    // 0B
	write("nested/sub/file2.bin", strings.Repeat("x", 512))   // 512B
	write("nested/deep/file3.log", strings.Repeat("y", 2048)) // 2KiB
	if err := os.MkdirAll(filepath.Join(root, "emptydir"), 0o755); err != nil {
		t.Fatal(err)
	}

	want.Files = 4
	want.Dirs = 4 // nested, nested/sub, nested/deep, emptydir
	want.Bytes = 10 + 0 + 512 + 2048

	if runtime.GOOS != "windows" {
		if err := os.Symlink("file1.txt", filepath.Join(root, "link.txt")); err != nil {
			t.Fatal(err)
		}
		// unix socket：net.Listen 建出的就是 socket 文件（非常规条目）
		l, err := net.Listen("unix", filepath.Join(root, "sock"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		want.Skipped = 2
		want.SkippedNames = []string{"link.txt", "sock"}
	}
	return root, want
}

// assertStatsEqual Stats 逐字段比较（含 SkippedNames 序）。
func assertStatsEqual(t *testing.T, label string, got, want Stats) {
	t.Helper()
	if got.Files != want.Files || got.Dirs != want.Dirs || got.Bytes != want.Bytes ||
		got.Skipped != want.Skipped || !equalStrings(got.SkippedNames, want.SkippedNames) {
		t.Fatalf("%s: Stats 不符\n got %+v\nwant %+v", label, got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestScanWriteParity 奇偶测试（#326 验收 2）：同一棵树，Scan 预扫与
// Write 流式写出的分类逐字段相等——演练数字就是对它要预演的那次运行的
// 数字。unix 附加非常规条目形态（symlink/socket 两边同口径跳过）。
func TestScanWriteParity(t *testing.T) {
	root, _ := makeTree(t)

	scanStats, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan 不应出错, got %v", err)
	}
	var buf bytes.Buffer
	writeStats, err := Write(&buf, root)
	if err != nil {
		t.Fatalf("Write 不应出错, got %v", err)
	}
	assertStatsEqual(t, "Scan vs Write 奇偶", scanStats, writeStats)

	// manifest 选项不改变源树口径（manifest 条目是协议开销，不计入）
	var buf2 bytes.Buffer
	mStats, err := Write(&buf2, root, WithManifest())
	if err != nil {
		t.Fatalf("Write(WithManifest) 不应出错, got %v", err)
	}
	assertStatsEqual(t, "Scan vs Write(WithManifest) 奇偶", scanStats, mStats)
}

// TestWriteExtractRoundTrip 写出→解包 round-trip：落地树与源树内容逐字节
// 一致，落地口径 Stats 与 Scan 相同（文件/目录/字节），Skipped=0。
func TestWriteExtractRoundTrip(t *testing.T) {
	root, want := makeTree(t)

	var buf bytes.Buffer
	st, err := Write(&buf, root)
	if err != nil {
		t.Fatalf("Write 不应出错, got %v", err)
	}
	assertStatsEqual(t, "Write", st, want)

	out := filepath.Join(t.TempDir(), "out")
	gotStats, err := Extract(bytes.NewReader(buf.Bytes()), out)
	if err != nil {
		t.Fatalf("Extract 不应出错, got %v", err)
	}
	wantExtract := want
	wantExtract.Skipped = 0
	wantExtract.SkippedNames = nil
	assertStatsEqual(t, "Extract", gotStats, wantExtract)

	// 内容逐字节一致（对称遍历两棵树）
	requireSameTree(t, root, out)
}

// requireSameTree 两棵树内容逐字节一致（文件集合与内容全等）。
func requireSameTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		q := filepath.Join(dst, rel)
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if fi.IsDir() {
			got, err := os.Stat(q)
			if err != nil {
				return err
			}
			if !got.IsDir() {
				t.Fatalf("%s 应是目录", rel)
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil // 非常规条目本就不传输
		}
		srcData, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dstData, err := os.ReadFile(q)
		if err != nil {
			t.Fatalf("落地树缺 %s: %v", rel, err)
		}
		if !bytes.Equal(srcData, dstData) {
			t.Fatalf("%s 内容不一致", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("树比对失败: %v", err)
	}
}

// TestScan_EmptyTree 空树是合法输入：零 Stats、空 tar。
func TestScan_EmptyTree(t *testing.T) {
	root := t.TempDir()

	st, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan 不应出错, got %v", err)
	}
	assertStatsEqual(t, "Scan 空树", st, Stats{})

	var buf bytes.Buffer
	st, err = Write(&buf, root)
	if err != nil {
		t.Fatalf("Write 不应出错, got %v", err)
	}
	assertStatsEqual(t, "Write 空树", st, Stats{})

	st, err = Extract(bytes.NewReader(buf.Bytes()), filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatalf("Extract 空流不应出错, got %v", err)
	}
	assertStatsEqual(t, "Extract 空流", st, Stats{})
}

// TestScan_MissingRoot root 不存在：行走错误原样上抛。
func TestScan_MissingRoot(t *testing.T) {
	_, err := Scan(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("root 不存在应上抛错误")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("应透传 fs.ErrNotExist, got %v", err)
	}
}

// TestWrite_NilOption nil 选项是构造期配置错误（仓规总则①）。
func TestWrite_NilOption(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("nil WriteOption 应 panic（fail-fast，仓规总则①）")
		}
	}()
	_, _ = Write(io.Discard, t.TempDir(), nil)
}

// TestWrite_WriteError 流写出失败：错误上抛、流不完整（不写结束块——
// 接收端 Extract 以 tar 读错误告终，绝不静默交付半棵树）。
func TestWrite_WriteError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "big.bin"), make([]byte, 1<<20), 0o644); err != nil {
		t.Fatal(err)
	}

	ew := &errWriter{limit: 1024}
	st, err := Write(ew, root)
	if err == nil {
		t.Fatal("写出失败应上抛错误")
	}
	_ = st

	// 同一流喂 Extract：必须报错（不完整流的接收端可见，而非假成功）
	_, xerr := Extract(bytes.NewReader(ew.buf), t.TempDir())
	if xerr == nil {
		t.Fatal("截断流喂 Extract 应报错（不静默交付半棵树）")
	}
}

// errWriter 写满 limit 字节后报错（保存已写字节供后续断言）。
type errWriter struct {
	limit int
	buf   []byte
}

func (w *errWriter) Write(p []byte) (int, error) {
	if len(w.buf)+len(p) > w.limit {
		n := w.limit - len(w.buf)
		if n > 0 {
			w.buf = append(w.buf, p[:n]...)
		}
		return n, io.ErrShortWrite
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}
