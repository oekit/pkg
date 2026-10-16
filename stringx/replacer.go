package stringx

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// 本文件提供基于 rune 级 trie 的多模式替换器。
//
// 扫描语义：从左到右逐 rune 扫描，同一起点命中多个 pattern 时取最长
//（命中后整体跳过该词，不再回溯）；未命中的 rune 原样保留。
// Replacer 构造完成后只读，可安全并发使用。

// replacerNode trie 节点。terminal 标记词尾——替换串允许为空串
// （删除语义），因此词尾不能以 repl 是否非空判断。
type replacerNode struct {
	children map[rune]*replacerNode
	repl     string
	terminal bool
}

// Replacer 多模式字符串替换器。
// 与 strings.Replacer 的差异：按 rune 对齐匹配（不撕裂 UTF-8），
// 且同一起点恒取最长匹配。
type Replacer struct {
	root *replacerNode
}

// NewReplacer 创建多模式替换器：patterns[i] 命中的子串替换为 repls[i]。
// 两者长度必须一致；空 pattern 或 nil patterns（不替换任何词）的处理见
// 返回值。同名 pattern 后写覆盖先写。
func NewReplacer(patterns, repls []string) (*Replacer, error) {
	if len(patterns) != len(repls) {
		return nil, fmt.Errorf("stringx: patterns 数 %d 与 repls 数 %d 不一致", len(patterns), len(repls))
	}
	root := &replacerNode{}
	for i, p := range patterns {
		if p == "" {
			return nil, fmt.Errorf("stringx: pattern 不能为空串（索引 %d）", i)
		}
		if !utf8.ValidString(p) {
			return nil, fmt.Errorf("stringx: pattern 含非法 UTF-8 序列（索引 %d）", i)
		}
		node := root
		for _, r := range p {
			if node.children == nil {
				node.children = make(map[rune]*replacerNode)
			}
			next := node.children[r]
			if next == nil {
				next = &replacerNode{}
				node.children[r] = next
			}
			node = next
		}
		node.terminal = true
		node.repl = repls[i]
	}
	return &Replacer{root: root}, nil
}

// NewMask 创建遮罩替换器：把 words 中每个词替换为等 rune 数的 mask 串。
// 用于敏感词遮罩场景，是 NewReplacer 的薄壳。
func NewMask(words []string, mask rune) (*Replacer, error) {
	repls := make([]string, len(words))
	for i, w := range words {
		repls[i] = strings.Repeat(string(mask), utf8.RuneCountInString(w))
	}
	return NewReplacer(words, repls)
}

// Replace 按 trie 最长匹配扫描 text 并替换。
// 按 rune 边界迭代：未命中的 rune 原字节保留（非法 UTF-8 序列不被
// 改写为 U+FFFD），命中的词整体替换。
func (r *Replacer) Replace(text string) string {
	if r == nil || r.root == nil || len(r.root.children) == 0 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); {
		node := r.root
		lastEnd, lastRepl := -1, ""
		for j := i; j < len(text); {
			rn, size := utf8.DecodeRuneInString(text[j:])
			next, ok := node.children[rn]
			if !ok {
				break
			}
			node = next
			j += size
			if node.terminal {
				lastEnd, lastRepl = j, node.repl
			}
		}
		if lastEnd > 0 {
			b.WriteString(lastRepl)
			i = lastEnd
		} else {
			_, size := utf8.DecodeRuneInString(text[i:])
			b.WriteString(text[i : i+size])
			i += size
		}
	}
	return b.String()
}
