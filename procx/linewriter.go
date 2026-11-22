package procx

import (
	"bytes"
	"fmt"
	"os"
	"runtime/debug"
	"sync/atomic"
)

// maxLineLen lineWriter 单行缓冲上限：超限丢弃该行不再回调。partial 若无上限，
// 单行无换行洪泛会绕过 maxChildStdout 限容（claude stream-json 每个事件就是一行，
// 一个含超大 diff 的 result 事件即可冲出数百 MB 峰值）。
const maxLineLen = 1 << 20 // 1MB

// lineWriter 按行拆分 stdout，回调后仍写入 buf 保留全文。
// 单行超过 maxLineLen 时丢弃该行（overflow 置位后不回调），扫到该行行尾后
// **从行尾之后继续按行处理**，buf 照常限容写入保留全文供兜底。
type lineWriter struct {
	buf      *cappedBuffer
	onLine   func(string)
	partial  []byte
	overflow bool
}

func (w *lineWriter) Write(p []byte) (int, error) {
	n := len(p)
	_, _ = w.buf.Write(p)
	for len(p) > 0 {
		if w.overflow {
			// 仍在超限行内：不累积，只找它的行尾。行尾之后同一 chunk 里
			// 的字节必须继续按行处理——用 LastIndexByte 会一路跳到**最后一个**
			// 换行，把超限行与本 chunk 末尾之间的完整行整段吞掉且不回调
			// （agent#30：单行 >1MB 后同 chunk 后续事件行全部丢失，消费方
			// 只看到事件流断片，无任何信号）。
			i := bytes.IndexByte(p, '\n')
			if i < 0 {
				return n, nil
			}
			w.overflow = false
			w.partial = w.partial[:0]
			p = p[i+1:]
			continue
		}
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			if len(w.partial)+len(p) > maxLineLen {
				w.overflow = true // 整行丢弃（含已累积部分），防 partial 无限放大
				w.partial = w.partial[:0]
				return n, nil
			}
			w.partial = append(w.partial, p...)
			return n, nil
		}
		if len(w.partial)+i > maxLineLen {
			// 该行自身就是超限行（行尾落在本 chunk 内）：与跨 chunk 的超限行
			// 同等处置——整行丢弃，跳过它的行尾。
			w.partial = w.partial[:0]
			p = p[i+1:]
			continue
		}
		line := string(append(w.partial, p[:i]...))
		w.partial = w.partial[:0]
		p = p[i+1:]
		if line != "" {
			w.onLine(line)
		}
	}
	return n, nil
}

// guardOnLine 收编 OnLine 回调的外部 panic（runtime#70）。
//
// 为什么必须库层收编：回调运行在 os/exec **自己的内部拷贝 goroutine** 上
// （writerDescriptor 的裸 io.Copy，无 recover），回调 panic 顺 Write → io.Copy
// 逐层上抛、被 exec 以进程级 fatal error 处理——调用方在自己 goroutine 上
// defer recover 拦不住另一条 goroutine，爆炸半径是整个宿主进程，与矩阵
// 「外部回调异常只降级、不上炸」的不变量（worker 池隔离 claim/run/heartbeat、
// LeaderElector.runCallback、notifyx 经 async.GoSafe 投递）矛盾。
//
// 收编手法：同步 recover + 落 stderr（本仓零依赖底座没有 logger/PanicHandler
// 面——oekit/concurrency 反向依赖本仓不能引，stderr 是零依赖前提下的观测
// 出口；宿主要结构化收编可在 OnLine 包装层自己做）。首个 panic 后**本次
// 执行**不再回调——缺陷已暴露，继续回调只会重复炸同一处；下次 RunDetailed
// 另起新守卫。stdout 全文仍照常采集兜底：外部回调缺陷降级为「丢这一次的
// 行回调」，而不是丢整个进程。
func guardOnLine(onLine func(string)) func(string) {
	var broken atomic.Bool
	return func(line string) {
		if broken.Load() {
			return
		}
		if runLineCb(onLine, line) {
			broken.Store(true)
		}
	}
}

// runLineCb 单次回调执行的 recover 垫片：true = 回调 panic（守卫据此熔断）。
func runLineCb(onLine func(string), line string) (broke bool) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "procx: OnLine 回调 panic 已收编（本行丢弃；本次执行后续行不再回调）: %v\n%s", r, debug.Stack())
			broke = true
		}
	}()
	onLine(line)
	return false
}
