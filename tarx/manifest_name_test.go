package tarx

// 清单条目名可配置（WithManifestName）的测试。
//
// 为什么要有这个选项：默认名 ManifestName 是固定串，落在接收端目录里可能
// 与用户自己的文件撞名（撞了 ⇒ 清单覆盖用户文件，或用户文件覆盖清单——
// 两种都让自证失去意义）。需要「绝不撞名」的调用方传一个唯一名。

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestManifestCustomName 自定义清单名生效，且流里就是那个名字。
func TestManifestCustomName(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	const name = ".jmsx-manifest-1234567890"

	var buf bytes.Buffer
	if _, err := Write(&buf, root, WithManifest(), WithManifestName(name)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// 流里的清单条目名应是自定义名
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
		if hdr.Name == name {
			found = true
		}
		if hdr.Name == ManifestName {
			t.Fatalf("自定义名时不应再出现默认名 %s", ManifestName)
		}
	}
	if !found {
		t.Fatalf("流里应有自定义名条目 %s", name)
	}

	// 解包后清单落在 root/<自定义名>
	out := t.TempDir()
	if _, err := Extract(bytes.NewReader(buf.Bytes()), out); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, name)); err != nil {
		t.Fatalf("清单应落在 root/%s: %v", name, err)
	}
}

// TestManifestNameInvalid 非法清单名在写入前就被拒（不留半截流）。
func TestManifestNameInvalid(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{".", "..", "sub/dir.txt", "../escape.txt", `..\\escape`} {
		var buf bytes.Buffer
		_, err := Write(&buf, root, WithManifest(), WithManifestName(bad))
		if err == nil {
			t.Fatalf("非法清单名 %q 应被拒", bad)
		}
		if !strings.Contains(err.Error(), "清单条目名非法") {
			t.Fatalf("非法清单名 %q 的错误消息应点名判据，实为 %v", bad, err)
		}
		// 关键：非法名时**不该写出任何内容**（半截流比没有流更坏）
		if buf.Len() != 0 {
			t.Fatalf("非法清单名 %q 时不应写出内容，实为 %d 字节", bad, buf.Len())
		}
	}
}

// TestManifestDefaultNameUnchanged 不传自定义名时行为不变（默认常量）。
func TestManifestDefaultNameUnchanged(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Write(&buf, root, WithManifest()); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if _, err := Extract(bytes.NewReader(buf.Bytes()), out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, ManifestName)); err != nil {
		t.Fatalf("默认名应是 %s: %v", ManifestName, err)
	}
}
