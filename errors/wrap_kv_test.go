package errors

import (
	"strings"
	"testing"
)

// #292：Wrap 的 kv 此前只有 Error() 文本一个出口——交给 slog/logx 时整串
// kv 退化成 error 字段里的一段文本，不可检索、不可聚合、不可按 key 过滤。
// 本文件钉住结构化出口：Kv（最外层）与 KvFromChain（沿链全量，外层在前）。

func TestKv_OutermostLayer(t *testing.T) {
	base := New("boom")
	err := Wrap(base, "query user", "user_id", 42, "table", "users")

	kv := Kv(err)
	if len(kv) != 4 || kv[0] != "user_id" || kv[1] != 42 || kv[2] != "table" || kv[3] != "users" {
		t.Fatalf("Kv 应原样返回最外层 kv 对, got %#v", kv)
	}

	// 无 kv 的 Wrap：nil（不是空切片，调用方 len 判零即可）
	if got := Kv(Wrap(base, "no context")); got != nil {
		t.Fatalf("无 kv 的 Wrap 应返回 nil, got %#v", got)
	}
	// 非 Wrap 构造的错误：nil
	if got := Kv(base); got != nil {
		t.Fatalf("非 Wrap 错误应返回 nil, got %#v", got)
	}
	if got := Kv(nil); got != nil {
		t.Fatalf("Kv(nil) 应返回 nil, got %#v", got)
	}
}

func TestKvFromChain_AllLayersOuterFirst(t *testing.T) {
	base := New("boom")
	inner := Wrap(base, "hash", "name", "a.txt")             // 内层 kv
	outer := Wrap(inner, "sync dir", "dir", "/srv", "op", 7) // 外层 kv

	kv := KvFromChain(outer)
	wantLen := 6 // 外层 2 对在前，内层 1 对在后
	if len(kv) != wantLen {
		t.Fatalf("KvFromChain 应收集两层共 %d 个元素, got %d: %#v", wantLen, len(kv), kv)
	}
	if kv[0] != "dir" || kv[1] != "/srv" || kv[2] != "op" || kv[3] != 7 ||
		kv[4] != "name" || kv[5] != "a.txt" {
		t.Fatalf("顺序应为外层在前内层在后, got %#v", kv)
	}

	// Errorf 单 %w 夹在中间：链穿过 formattedError 继续 收集
	ef := Errorf("wrap once: %w", inner)
	if got := KvFromChain(Wrap(ef, "top", "k", "v")); len(got) != 4 || got[0] != "k" {
		t.Fatalf("链上的 formattedError 应被穿透, got %#v", got)
	}
}

func TestKvFromChain_MultiErrorBranchDepthFirst(t *testing.T) {
	left := Wrap(New("l"), "lctx", "side", "left")
	right := Wrap(New("r"), "rctx", "side", "right")
	multi := Errorf("%w and %w", left, right) // 多 %w：Unwrap() []error
	outer := Wrap(multi, "fanout", "stage", 1)

	kv := KvFromChain(outer)
	// 外层 1 对 + 左分支 1 对 + 右分支 1 对，深度先序
	if len(kv) != 6 || kv[0] != "stage" || kv[2] != "side" || kv[3] != "left" || kv[4] != "side" || kv[5] != "right" {
		t.Fatalf("多分支链应深度先序收集全量 kv, got %#v", kv)
	}
}

func TestKvFromChain_NoKvReturnsNil(t *testing.T) {
	if got := KvFromChain(New("plain")); got != nil {
		t.Fatalf("无 kv 链应返回 nil, got %#v", got)
	}
	if got := KvFromChain(nil); got != nil {
		t.Fatalf("KvFromChain(nil) 应返回 nil, got %#v", got)
	}
}

// KVCarrier 是导出接口：errors.As 可识别携带 kv 的错误（第三方包装体实现
// 同一接口即可参与提取）。
func TestKVCarrier_AsTarget(t *testing.T) {
	err := Wrap(New("boom"), "query user", "user_id", 42)
	var c KVCarrier
	if !As(err, &c) {
		t.Fatalf("errors.As 应能识别 KVCarrier")
	}
	if kv := c.Kv(); len(kv) != 2 || kv[0] != "user_id" {
		t.Fatalf("KVCarrier.Kv = %#v", kv)
	}
	if !strings.Contains(err.Error(), "user_id=42") {
		t.Fatalf("Error() 文本出口保持不变, got %q", err.Error())
	}
}
