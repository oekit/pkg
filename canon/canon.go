package canon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// DefaultSchema 缺省 schema 版本串（宿主可经 [Surface.Schema] 覆盖为
// 自己的命名空间，如 "policyver/v1"）。
const DefaultSchema = "canon/v1"

// 哨兵错误：errors.Is 可判定（非法入参归 ErrBadSurface / ErrUnencodable /
// ErrSchemaMismatch，文案带上下文）。
var (
	ErrBadSurface     = errors.New("canon: surface 非法")
	ErrUnencodable    = errors.New("canon: facet 值不可 JSON 化")
	ErrSchemaMismatch = errors.New("canon: 新旧快照 schema 不一致，不可 diff")
)

// digestBytes 规范形指纹宽度：sha256 前 48bit——只用于对比不用于防篡改
// （取舍见包文档）。
var digestLen = 6

// Facet 策略面的一个可独立比对/吊销的切片。Name 在快照内唯一；
// Value 是宿主**已完成三条归一纪律**（排序/去重/缺省折算，见包文档）
// 的结构，须可 JSON 化（map 键序无关、结构体字段序钉死）。
type Facet struct {
	Name  string
	Value any
}

// Surface 一次策略面快照。Schema 空 = [DefaultSchema]；改 canonical
// 结构必须升格 Schema 串（契约见包文档「canonical 稳定性」）。
type Surface struct {
	Schema string
	Facets []Facet
}

// Version 版本指纹：schema 前缀 + 48bit 十六进制摘要。
type Version struct {
	Schema string
	Digest string
}

// String 形如 "policyver/v1:3f9a1c2b4d5e"。
func (v Version) String() string { return v.Schema + ":" + v.Digest }

// canonicalDoc 规范形文档。Facets 用 map——encoding/json 对 map 键字典序
// 输出，facet 声明序与值内 map 键序都不进哈希；外层结构体字段序
// （schema 在前、facets 在后）钉死。
type canonicalDoc struct {
	Schema string                     `json:"schema"`
	Facets map[string]json.RawMessage `json:"facets"`
}

// schemaOf schema 串：空回退 [DefaultSchema]（仓规总则②：零值 = 显式
// 回退默认）。
func schemaOf(s Surface) string {
	if s.Schema == "" {
		return DefaultSchema
	}
	return s.Schema
}

// canonicalBytes 把快照折算成规范形字节。facet 名唯一性在此 fail-fast。
func canonicalBytes(s Surface) ([]byte, error) {
	facets := make(map[string]json.RawMessage, len(s.Facets))
	for _, f := range s.Facets {
		if strings.TrimSpace(f.Name) == "" {
			return nil, fmt.Errorf("%w: facet 名为空", ErrBadSurface)
		}
		if _, dup := facets[f.Name]; dup {
			return nil, fmt.Errorf("%w: facet 重名 %q（同一面请合并为一个 facet）", ErrBadSurface, f.Name)
		}
		raw, err := json.Marshal(f.Value)
		if err != nil {
			return nil, fmt.Errorf("%w: facet %q: %v", ErrUnencodable, f.Name, err)
		}
		facets[f.Name] = raw
	}
	if len(facets) == 0 {
		return nil, fmt.Errorf("%w: 至少一个 facet（空面无版本可言）", ErrBadSurface)
	}
	return json.Marshal(canonicalDoc{Schema: schemaOf(s), Facets: facets})
}

// Fingerprint 计算快照的版本指纹：规范形 JSON → sha256 → 48bit 十六进制。
// 同语义（三条归一纪律后）必同版、可复现。
func Fingerprint(s Surface) (Version, error) {
	b, err := canonicalBytes(s)
	if err != nil {
		return Version{}, err
	}
	sum := sha256.Sum256(b)
	return Version{
		Schema: schemaOf(s),
		Digest: hex.EncodeToString(sum[:digestLen]),
	}, nil
}

// FacetDigest 单个 facet 值的规范形指纹（48bit 十六进制）：按 facet 吊销/
// 比对时可独立持久化的切片指纹，与会话行记的 policy_version 配套。
// 名字不进切片指纹——切片指纹只对「值」负责，名字是装配层的键。
func FacetDigest(f Facet) (string, error) {
	b, err := json.Marshal(f.Value)
	if err != nil {
		return "", fmt.Errorf("%w: facet %q: %v", ErrUnencodable, f.Name, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:digestLen]), nil
}

// ChangeKind facet 变更形态。
type ChangeKind int

const (
	FacetAdded   ChangeKind = iota // 新面新增的 facet
	FacetChanged                   // 两面同名但指纹不同
	FacetRemoved                   // 旧面有、新面没有
)

// String "added"/"changed"/"removed"。
func (k ChangeKind) String() string {
	switch k {
	case FacetAdded:
		return "added"
	case FacetChanged:
		return "changed"
	case FacetRemoved:
		return "removed"
	default:
		return fmt.Sprintf("ChangeKind(%d)", int(k))
	}
}

// Change 一个 facet 的变更记录。
type Change struct {
	Kind ChangeKind
	Name string
	From string // 旧指纹（Removed/Changed 时非空）
	To   string // 新指纹（Added/Changed 时非空）
}

// Diff 按 facet 名逐片比较两个快照，返回变更清单。确定性顺序：Added
// 在前、Changed 居中、Removed 在后，各段按 Name 字典序——「新面多了
// 什么、什么变了、什么没了」。同版 facet 不报。新旧 schema 不一致按
// [ErrSchemaMismatch] 拒绝：跨 schema 的 diff 是无意义比对，升格纪律
// 在此兜底。
func Diff(old, cur Surface) ([]Change, error) {
	so, sc := schemaOf(old), schemaOf(cur)
	if so != sc {
		return nil, fmt.Errorf("%w: %q vs %q", ErrSchemaMismatch, so, sc)
	}
	oldD, err := facetDigests(old)
	if err != nil {
		return nil, err
	}
	curD, err := facetDigests(cur)
	if err != nil {
		return nil, err
	}

	var added, changed, removed []Change
	for _, f := range old.Facets {
		cd, ok := curD[f.Name]
		if !ok {
			removed = append(removed, Change{Kind: FacetRemoved, Name: f.Name, From: oldD[f.Name]})
			continue
		}
		if cd != oldD[f.Name] {
			changed = append(changed, Change{Kind: FacetChanged, Name: f.Name, From: oldD[f.Name], To: cd})
		}
	}
	for _, f := range cur.Facets {
		if _, ok := oldD[f.Name]; !ok {
			added = append(added, Change{Kind: FacetAdded, Name: f.Name, To: curD[f.Name]})
		}
	}
	slices.SortFunc(added, func(a, b Change) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(changed, func(a, b Change) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(removed, func(a, b Change) int { return strings.Compare(a.Name, b.Name) })
	return append(append(added, changed...), removed...), nil
}

// facetDigests 快照内全部 facet 名 → 切片指纹（重名/空名由 canonicalBytes
// 的同口径校验兜底：这里复用同一 fail-fast 集）。
func facetDigests(s Surface) (map[string]string, error) {
	seen := make(map[string]struct{}, len(s.Facets))
	out := make(map[string]string, len(s.Facets))
	for _, f := range s.Facets {
		if strings.TrimSpace(f.Name) == "" {
			return nil, fmt.Errorf("%w: facet 名为空", ErrBadSurface)
		}
		if _, dup := seen[f.Name]; dup {
			return nil, fmt.Errorf("%w: facet 重名 %q（同一面请合并为一个 facet）", ErrBadSurface, f.Name)
		}
		d, err := FacetDigest(f)
		if err != nil {
			return nil, err
		}
		seen[f.Name] = struct{}{}
		out[f.Name] = d
	}
	return out, nil
}

// SortDedup 排序 + 去重（三条归一纪律的前两条）：集合语义字段的唯一
// 入口原语——声明顺序不是语义、同一词写两遍与写一遍是同一集合。返回
// 副本不改写入参；空入参返回 nil。
func SortDedup(words []string) []string {
	if len(words) == 0 {
		return nil
	}
	out := slices.Clone(words)
	slices.Sort(out)
	return slices.Compact(out)
}
