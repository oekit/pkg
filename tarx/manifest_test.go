package tarx

// manifest 自证测试（#326 验收 3）：WithManifest 端到端核对通过；篡改一
// 个字节 → Verify 失败且指名文件；缺文件/坏清单行/缺清单各有可判错误。

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// manifestTree 小型源树 + WithManifest 写出 + 解包，返回 (解包 root, 流)。
func manifestTree(t *testing.T) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("bravo"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if _, err := Write(&buf, root, WithManifest()); err != nil {
		t.Fatalf("Write 不应出错, got %v", err)
	}
	out := filepath.Join(t.TempDir(), "out")
	if _, err := Extract(bytes.NewReader(buf.Bytes()), out); err != nil {
		t.Fatalf("Extract 不应出错, got %v", err)
	}
	return out, buf.Bytes()
}

// TestManifest_VerifyOK 端到端：写出→解包→核对通过；清单格式与 sha256sum
// 兼容（人工 `sha256sum -c` 可直接消费——行形态断言钉住）。
func TestManifest_VerifyOK(t *testing.T) {
	out, _ := manifestTree(t)

	if err := VerifyManifest(out); err != nil {
		t.Fatalf("未被篡改的树应核对通过, got %v", err)
	}

	data, err := os.ReadFile(filepath.Join(out, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("清单应逐文件一行（2 行）, got %d: %q", len(lines), data)
	}
	for _, line := range lines {
		// <64位hex><空格><空格><name>——sha256sum 文本格式
		if len(line) < 66 || line[64] != ' ' || line[65] != ' ' {
			t.Fatalf("清单行应满足 sha256sum 格式, got %q", line)
		}
	}
}

// TestManifest_TamperByte 篡改一个字节 → Verify 失败且**指名文件**（#326
// 验收 3）。
func TestManifest_TamperByte(t *testing.T) {
	out, _ := manifestTree(t)

	target := filepath.Join(out, "sub", "b.txt")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	data[0] ^= 0x01 // 翻转一个字节
	if werr := os.WriteFile(target, data, 0o644); werr != nil {
		t.Fatal(werr)
	}

	err = VerifyManifest(out)
	if !errors.Is(err, ErrMismatch) {
		t.Fatalf("篡改后应 ErrMismatch, got %v", err)
	}
	if !strings.Contains(err.Error(), "sub/b.txt") {
		t.Fatalf("错误消息应指名文件, got %q", err.Error())
	}
}

// TestManifest_MissingFile 清单列出的文件缺失 → ErrMismatch 指名。
func TestManifest_MissingFile(t *testing.T) {
	out, _ := manifestTree(t)
	if err := os.Remove(filepath.Join(out, "a.txt")); err != nil {
		t.Fatal(err)
	}

	err := VerifyManifest(out)
	if !errors.Is(err, ErrMismatch) {
		t.Fatalf("缺文件应 ErrMismatch, got %v", err)
	}
	if !strings.Contains(err.Error(), "a.txt") {
		t.Fatalf("错误消息应指名缺失文件, got %q", err.Error())
	}
}

// TestManifest_BadLine 清单行被改坏 → ErrMismatch 指名行号。
func TestManifest_BadLine(t *testing.T) {
	out, _ := manifestTree(t)
	mp := filepath.Join(out, ManifestName)
	if err := os.WriteFile(mp, []byte("zz-not-hex  a.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := VerifyManifest(out)
	if !errors.Is(err, ErrMismatch) {
		t.Fatalf("坏清单行应 ErrMismatch, got %v", err)
	}
	if !strings.Contains(err.Error(), "第 1 行") {
		t.Fatalf("错误消息应指名行号, got %q", err.Error())
	}
}

// TestManifest_NoManifest 没有清单的树 → ErrNoManifest（区分「没自证」与
// 「自证失败」）。
func TestManifest_NoManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "only.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := VerifyManifest(root)
	if !errors.Is(err, ErrNoManifest) {
		t.Fatalf("无清单应 ErrNoManifest, got %v", err)
	}
	if errors.Is(err, ErrMismatch) {
		t.Fatal("ErrNoManifest 不应同时是 ErrMismatch（两类不可混报）")
	}
}

// TestManifest_TraversalLine 清单被改出穿越条目 → ErrMismatch（与解包同
// 口径判死，绝不按清单指名走出 root）。
func TestManifest_TraversalLine(t *testing.T) {
	out, _ := manifestTree(t)
	mp := filepath.Join(out, ManifestName)
	zero := strings.Repeat("0", 64)
	if err := os.WriteFile(mp, []byte(zero+"  ../../outside.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := VerifyManifest(out)
	if !errors.Is(err, ErrMismatch) {
		t.Fatalf("穿越清单行应 ErrMismatch, got %v", err)
	}
	if !strings.Contains(err.Error(), "越界") {
		t.Fatalf("错误消息应点名越界条目, got %q", err.Error())
	}
}

// TestManifest_MissingRoot 树整个缺失 → ErrNoManifest（清单文件不存在）。
func TestManifest_MissingRoot(t *testing.T) {
	err := VerifyManifest(filepath.Join(t.TempDir(), "nope"))
	if !errors.Is(err, ErrNoManifest) && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("树缺失应 ErrNoManifest 或透传 NotExist, got %v", err)
	}
}
