package errors

import (
	stderrors "errors"
	"fmt"
	"testing"
)

func TestNewf(t *testing.T) {
	err := Newf("port %d out of range", 70000)
	if err.Error() != "port 70000 out of range" {
		t.Fatalf("Newf message = %q", err.Error())
	}
	if Unwrap(err) != nil {
		t.Fatal("Newf 无链语义：Unwrap 应为 nil")
	}
	if Newf("static").Error() != "static" {
		t.Fatal("Newf 无参形态应与 New 等价")
	}
}

func TestWrapf(t *testing.T) {
	sentinel := stderrors.New("disk full")
	err := Wrapf(sentinel, "write %s at offset %d", "/data/a.log", 4096)
	if !Is(err, sentinel) {
		t.Fatal("Wrapf 应保 Is 链（err 为尾参 %w）")
	}
	if Unwrap(err) != sentinel {
		t.Fatalf("Unwrap 应直达 sentinel, got %v", Unwrap(err))
	}
	want := "write /data/a.log at offset 4096: disk full"
	if err.Error() != want {
		t.Fatalf("Wrapf message = %q, want %q", err.Error(), want)
	}
}

// Wrapf 尾参追加不得突变调用方 args 切片（append 别名防御）。
func TestWrapfDoesNotMutateArgs(t *testing.T) {
	args := []any{"a", 1}
	_ = Wrapf(fmt.Errorf("cause"), "op %v %v", args...)
	if len(args) != 2 || args[0] != "a" || args[1] != 1 {
		t.Fatalf("调用方 args 被突变: %v", args)
	}
}

func TestWrapfNilErr(t *testing.T) {
	if Wrapf(nil, "never %s", "wrapped") != nil {
		t.Fatal("Wrapf(nil, ...) 应返回 nil（同 Wrap(nil) 口径）")
	}
}

// 与 Wrap(kv) 互操作：Wrapf 外层再 Wrap kv，KvFromChain 应只收集 kv 层。
func TestWrapfComposeWithKvWrap(t *testing.T) {
	base := Newf("boom")
	layer1 := Wrapf(base, "step %d", 2)
	layer2 := Wrap(layer1, "handler", "stage", "ingest")
	if !Is(layer2, base) {
		t.Fatal("三层组合 Is 链断裂")
	}
	if kv := Kv(layer2); len(kv) != 2 || kv[0] != "stage" {
		t.Fatalf("最外层 Kv = %v", kv)
	}
}
