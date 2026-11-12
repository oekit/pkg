package logkv

import "testing"

// #412 契约：ErrKey 是 logx.ErrKey 的上游镜像，值必须逐字等于 "err"。
// 不一致会把两矩阵（oekit/yi-nology）的日志字段劈成两个，Loki/ES 的聚合
// 查询双双失明——改这里必须同步 logx.ErrKey（单向约定见包文档）。
func TestErrKeyMirrorValue(t *testing.T) {
	if ErrKey != "err" {
		t.Fatalf("logkv.ErrKey = %q, want %q（与 logx.ErrKey 的镜像契约被破坏）", ErrKey, "err")
	}
}
