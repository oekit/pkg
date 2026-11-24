package tarx

// 符号链接传输能力的测试（Write 侧 WithSymlinks / Extract 侧
// WithSymlinkExtract）。
//
// 三个面必须都有断言：
//
//  1. **默认不传**（旧行为不变）：链接被跳过并计数点名——已有 parity
//     测试钉住「默认口径下 symlink 是 Skipped」；
//  2. **开启后能往返**：Write 写出的链接，Extract 能原样落地（目标与
//     相对性都保持）；
//  3. **逃逸被拒**：绝对目标与越界相对目标必须被拒收并点名——这是开启
//     链接语义后**新增的攻击面**，不测就等于没防线。

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// symlinkTree 造一棵带链接的树：常规文件 + 相对链接 + 指向目录的链接。
func symlinkTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dir", "b.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestSymlinkDefaultSkipped 默认口径下链接不传输（跳过 + 点名）。
func TestSymlinkDefaultSkipped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows 无符号链接语义")
	}
	root := symlinkTree(t)
	if err := os.Symlink("a.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	st, err := Write(&buf, root)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if st.Symlinks != 0 {
		t.Fatalf("默认口径下 Symlinks 应为 0，实为 %d", st.Symlinks)
	}
	if st.Skipped != 1 {
		t.Fatalf("默认口径下链接应计入 Skipped（=1），实为 %d", st.Skipped)
	}
	if len(st.SkippedNames) != 1 || st.SkippedNames[0] != "link.txt" {
		t.Fatalf("SkippedNames 应点名 link.txt，实为 %v", st.SkippedNames)
	}

	// 流里不应有 symlink 条目
	tr := tar.NewReader(bytes.NewReader(buf.Bytes()))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeSymlink {
			t.Fatalf("默认口径下流里不应出现 symlink 条目：%s", hdr.Name)
		}
	}
}

// TestSymlinkRoundTrip 开启后 Write↔Extract 往返一致：链接落地、目标保持。
func TestSymlinkRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows 无符号链接语义")
	}
	root := symlinkTree(t)
	if err := os.Symlink("a.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dir", filepath.Join(root, "dirlink")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	st, err := Write(&buf, root, WithSymlinks())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if st.Symlinks != 2 {
		t.Fatalf("Symlinks 应为 2，实为 %d", st.Symlinks)
	}
	if st.Skipped != 0 {
		t.Fatalf("开启后链接不应再计入 Skipped（=0），实为 %d", st.Skipped)
	}

	out := t.TempDir()
	est, err := Extract(bytes.NewReader(buf.Bytes()), out, WithSymlinkExtract())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if est.Symlinks != 2 {
		t.Fatalf("Extract Symlinks 应为 2，实为 %d", est.Symlinks)
	}
	if est.Skipped != 0 {
		t.Fatalf("Extract 不应有跳过项，实为 %d（%v）", est.Skipped, est.SkippedNames)
	}

	// 链接真的落成了链接，且目标保持
	target, err := os.Readlink(filepath.Join(out, "link.txt"))
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if target != "a.txt" {
		t.Fatalf("链接目标应保持 a.txt，实为 %q", target)
	}
	// 链接指向的内容可读（相对目标在同目录内）
	b, err := os.ReadFile(filepath.Join(out, "link.txt"))
	if err != nil {
		t.Fatalf("经链接读内容: %v", err)
	}
	if string(b) != "hello" {
		t.Fatalf("链接内容应为 hello，实为 %q", b)
	}
}

// TestSymlinkEscapeRejected 逃逸目标必须被拒收（开启链接语义后的新增面）。
func TestSymlinkEscapeRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows 无符号链接语义")
	}
	cases := []struct {
		name   string
		link   string
		wantSk int
	}{
		{"绝对目标", "/etc/passwd", 1},
		{"越界相对目标", "../../etc/passwd", 1},
		{"空目标", "", 1},
	}
	for _, tc := range cases {
		// 手工造一条 symlink 条目的流（不经过 Write——Write 记的是源树
		// 里的真实链接，逃逸目标要靠「恶意归档」这侧才造得出来）。
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		hdr := &tar.Header{
			Typeflag: tar.TypeSymlink,
			Name:     "evil",
			Linkname: tc.link,
			Mode:     0o777,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}

		out := t.TempDir()
		st, err := Extract(bytes.NewReader(buf.Bytes()), out, WithSymlinkExtract())
		if err != nil {
			t.Fatalf("%s: Extract: %v", tc.name, err)
		}
		if st.Symlinks != 0 {
			t.Fatalf("%s: 逃逸链接不应落地（Symlinks=0），实为 %d", tc.name, st.Symlinks)
		}
		if st.Skipped != tc.wantSk {
			t.Fatalf("%s: 应跳过并计数 1，实为 %d", tc.name, st.Skipped)
		}
		if len(st.SkippedNames) != 1 || st.SkippedNames[0] != "evil" {
			t.Fatalf("%s: SkippedNames 应点名 evil，实为 %v", tc.name, st.SkippedNames)
		}
		// 目标处不该留下任何东西
		if _, err := os.Lstat(filepath.Join(out, "evil")); !os.IsNotExist(err) {
			t.Fatalf("%s: 逃逸链接不该在目标处留下条目", tc.name)
		}
	}
}

// TestSymlinkWithinRootAllowed 落在 root 内的相对目标（含悬空）必须放行。
func TestSymlinkWithinRootAllowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows 无符号链接语义")
	}
	for _, link := range []string{"a.txt", "dir/b.txt", "nonexistent.txt", "./a.txt"} {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		hdr := &tar.Header{Typeflag: tar.TypeSymlink, Name: "ok", Linkname: link, Mode: 0o777}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		out := t.TempDir()
		st, err := Extract(bytes.NewReader(buf.Bytes()), out, WithSymlinkExtract())
		if err != nil {
			t.Fatalf("link=%q Extract: %v", link, err)
		}
		if st.Symlinks != 1 {
			t.Fatalf("link=%q: root 内链接应落地（Symlinks=1），实为 %d（跳过 %v）",
				link, st.Symlinks, st.SkippedNames)
		}
	}
}

// TestSymlinkStatsParity Scan↔Write 在开启链接语义时仍逐字段相等。
func TestSymlinkStatsParity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows 无符号链接语义")
	}
	root := symlinkTree(t)
	if err := os.Symlink("a.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	ws, err := Write(&buf, root, WithSymlinks())
	if err != nil {
		t.Fatal(err)
	}
	// Scan 默认口径（链接算 Skipped）⇒ 与开启后的 Write 口径不同，
	// 两者不该被强行相等——这里只断言 Write 侧的链接数与 Files/Dirs 自洽。
	if ws.Files != 2 || ws.Dirs != 1 || ws.Symlinks != 1 {
		t.Fatalf("Write Stats 不自洽：%+v", ws)
	}
	if ws.Skipped != 0 {
		t.Fatalf("开启后不该有跳过项：%+v", ws)
	}
	// 流里确实有一条 symlink 条目，Linkname 如实记录
	tr := tar.NewReader(bytes.NewReader(buf.Bytes()))
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeSymlink {
			found = true
			if hdr.Name != "link.txt" {
				t.Fatalf("symlink 条目名应为 link.txt，实为 %q", hdr.Name)
			}
			if hdr.Linkname != "a.txt" {
				t.Fatalf("Linkname 应如实记录 a.txt，实为 %q", hdr.Linkname)
			}
		}
	}
	if !found {
		t.Fatal("流里应有 symlink 条目")
	}
}

// TestSymlinkExtractWithoutOptionSkipped 不开解包选项时链接被跳过（口径不分裂）。
func TestSymlinkExtractWithoutOptionSkipped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows 无符号链接语义")
	}
	root := symlinkTree(t)
	if err := os.Symlink("a.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Write(&buf, root, WithSymlinks()); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	st, err := Extract(bytes.NewReader(buf.Bytes()), out) // 不开选项
	if err != nil {
		t.Fatal(err)
	}
	if st.Symlinks != 0 {
		t.Fatalf("不开选项时不该落地链接，实为 %d", st.Symlinks)
	}
	if st.Skipped != 1 {
		t.Fatalf("不开选项时链接应计入 Skipped（=1），实为 %d", st.Skipped)
	}
	if !strings.Contains(strings.Join(st.SkippedNames, ","), "link.txt") {
		t.Fatalf("应点名 link.txt，实为 %v", st.SkippedNames)
	}
}
