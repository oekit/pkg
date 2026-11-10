package tarx

// 防御式解包测试（#326 验收 1）：恶意归档（绝对路径/`..` 穿越/链接条目）
// 全部跳过、Skipped 计数、SkippedNames 点名前三个、root 外零写入。

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// buildTar 内存拼 tar 流（恶意条目构造用）。
type tarSpec struct {
	name     string
	typeflag byte
	body     string
	link     string
}

func buildTar(t *testing.T, entries []tarSpec) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			ModTime:  time.Unix(0, 0),
		}
		switch e.typeflag {
		case tar.TypeReg:
			hdr.Size = int64(len(e.body))
			hdr.Mode = 0o644
		case tar.TypeDir:
			hdr.Mode = 0o755
		case tar.TypeSymlink:
			hdr.Linkname = e.link
		case tar.TypeLink:
			hdr.Linkname = e.link
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.typeflag == tar.TypeReg && len(e.body) > 0 {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestExtract_MaliciousArchive 恶意归档（#326 验收 1）：绝对路径、
// `..` 穿越、硬链接/符号链接条目全部拒收；Skipped 计数、SkippedNames 点
// 名前三个；合法条目照常落地；**root 外零写入**。
func TestExtract_MaliciousArchive(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "extract")
	data := buildTar(t, []tarSpec{
		{name: "/etc/passwd", typeflag: tar.TypeReg, body: "pwned"},           // 绝对路径
		{name: "../../escape.txt", typeflag: tar.TypeReg, body: "pwned"},      // 越两级穿越
		{name: "sub/../../escape2.txt", typeflag: tar.TypeReg, body: "pwned"}, // 中段穿越
		{name: "ok.txt", typeflag: tar.TypeReg, body: "safe-content"},
		{name: "link-to-passwd", typeflag: tar.TypeSymlink, link: "/etc/passwd"}, // 链接条目
		{name: "hardlink-escape", typeflag: tar.TypeLink, link: "../../escape3.txt"},
		{name: "okdir", typeflag: tar.TypeDir},
		{name: "okdir/nested.txt", typeflag: tar.TypeReg, body: "also-safe"},
	})

	st, err := Extract(bytes.NewReader(data), root)
	if err != nil {
		t.Fatalf("Extract 不应报错（恶意条目是跳过不是失败）, got %v", err)
	}
	// Skipped 计数 + 点名前三个（按遇序）：绝对路径、两级穿越、中段穿越、
	// symlink、硬链接 = 5 个拒收
	if st.Skipped != 5 {
		t.Fatalf("5 个恶意条目应全部跳过, got Skipped=%d (%v)", st.Skipped, st.SkippedNames)
	}
	wantNames := []string{"/etc/passwd", "../../escape.txt", "sub/../../escape2.txt"}
	if len(st.SkippedNames) != 3 {
		t.Fatalf("SkippedNames 应点名前三个, got %v", st.SkippedNames)
	}
	for i, w := range wantNames {
		if st.SkippedNames[i] != w {
			t.Fatalf("SkippedNames[%d] 应为 %q, got %q", i, w, st.SkippedNames[i])
		}
	}
	// 合法条目落地
	if st.Files != 2 || st.Dirs != 1 {
		t.Fatalf("合法条目应落地 Files=2 Dirs=1, got %+v", st)
	}
	for _, rel := range []string{"ok.txt", "okdir/nested.txt"} {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || string(b) == "pwned" {
			t.Fatalf("合法条目 %s 应原样落地, got %q err=%v", rel, b, err)
		}
	}
	// root 外零写入：parent（root 的上一层）只应有 extract 一个目录
	ents, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name() != "extract" {
		for _, e := range ents {
			t.Logf("parent 下出现: %s", e.Name())
		}
		t.Fatalf("root 外出现写入（Zip Slip 得手）")
	}
}

// TestExtract_LeadingDotEntries 主流 tar 工具的 `./` 前缀形态：`./x` 落
// root 内、`./` 目录条目视作 root 本身不计数。
func TestExtract_LeadingDotEntries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	data := buildTar(t, []tarSpec{
		{name: "./", typeflag: tar.TypeDir},
		{name: "./ok.txt", typeflag: tar.TypeReg, body: "fine"},
	})
	st, err := Extract(bytes.NewReader(data), root)
	if err != nil {
		t.Fatalf("Extract 不应出错, got %v", err)
	}
	if st.Files != 1 || st.Dirs != 0 {
		t.Fatalf("`./` 前缀应正常归一, got %+v", st)
	}
	if b, err := os.ReadFile(filepath.Join(root, "ok.txt")); err != nil || string(b) != "fine" {
		t.Fatalf("./ok.txt 应落地, got %q err=%v", b, err)
	}
}

// TestExtract_TruncatedStream 截断流：tar 读错误上抛（Stats 反映死前落地量）。
func TestExtract_TruncatedStream(t *testing.T) {
	full := buildTar(t, []tarSpec{
		{name: "a.txt", typeflag: tar.TypeReg, body: strings_repeat(4096)},
		{name: "b.txt", typeflag: tar.TypeReg, body: "second"},
	})
	st, err := Extract(bytes.NewReader(full[:2048]), filepath.Join(t.TempDir(), "root"))
	if err == nil {
		t.Fatal("截断流应上抛错误")
	}
	_ = st // 死前落地量不作硬性断言（tar 内部缓冲边界决定）
}

func strings_repeat(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return string(b)
}
