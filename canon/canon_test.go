package canon

import (
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// roleAdminLike 两个同字段不同声明序的结构体：字段序钉死契约的探针。
type roleAdminA struct {
	Mode    string
	Attempt int
	Words   []string
}

type roleAdminB struct {
	Attempt int
	Words   []string
	Mode    string
}

// TestCanonicalKeyOrderIndependent 契约：map 键序无关 + facet 声明序无关
// ——键/清单的插入与排列顺序不是语义，不换版本。
func TestCanonicalKeyOrderIndependent(t *testing.T) {
	a := Surface{
		Facets: []Facet{
			{Name: "role:admin", Value: map[string]string{"mode": "strict", "scope": "repo"}},
			{Name: "role:dev", Value: []string{"read", "write"}},
		},
	}
	b := Surface{
		Facets: []Facet{
			{Name: "role:dev", Value: []string{"read", "write"}},                              // facet 顺序颠倒
			{Name: "role:admin", Value: map[string]string{"scope": "repo", "mode": "strict"}}, // map 键插入序颠倒
		},
	}
	va, err := Fingerprint(a)
	if err != nil {
		t.Fatalf("Fingerprint(a): %v", err)
	}
	vb, err := Fingerprint(b)
	if err != nil {
		t.Fatalf("Fingerprint(b): %v", err)
	}
	if va != vb {
		t.Fatalf("键序/声明序不应换版本：%s vs %s", va, vb)
	}

	// 集合语义字段：宿主 SortDedup 喂入后，重复声明与排列差异同版。
	c := Surface{Facets: []Facet{{Name: "words", Value: SortDedup([]string{"write", "read", "write"})}}}
	d := Surface{Facets: []Facet{{Name: "words", Value: SortDedup([]string{"read", "write"})}}}
	vc, _ := Fingerprint(c)
	vd, _ := Fingerprint(d)
	if vc != vd {
		t.Fatalf("排序去重后应同版：%s vs %s", vc, vd)
	}

	// 纪律边界：未归一的原始切片序是不同规范形——包不越权猜测集合语义，
	// 归一必须在喂入前完成（宿主职责）。
	e := Surface{Facets: []Facet{{Name: "words", Value: []string{"write", "read"}}}}
	ve, _ := Fingerprint(e)
	if ve == vd {
		t.Fatal("未排序的原始切片应产生不同版本（归一是宿主职责，包不猜测）")
	}
}

// TestCanonicalFieldOrderPinned 契约：结构体字段序钉死——字段序是规范形
// 的一部分；改 canonical 结构必须升格 Schema，否则哈希漂移即被发现。
func TestCanonicalFieldOrderPinned(t *testing.T) {
	vA := Facet{Name: "role", Value: roleAdminA{Mode: "strict", Attempt: 3, Words: []string{"a", "b"}}}
	vB := Facet{Name: "role", Value: roleAdminB{Attempt: 3, Words: []string{"a", "b"}, Mode: "strict"}}
	dA, err := FacetDigest(vA)
	if err != nil {
		t.Fatalf("FacetDigest(A): %v", err)
	}
	dB, err := FacetDigest(vB)
	if err != nil {
		t.Fatalf("FacetDigest(B): %v", err)
	}
	if dA == dB {
		t.Fatalf("字段序不同应产生不同指纹（字段序钉死）：%s", dA)
	}

	// 同一结构类型重复取指纹：稳定可复现。
	dA2, _ := FacetDigest(vA)
	if dA != dA2 {
		t.Fatalf("同结构重复指纹应一致：%s vs %s", dA, dA2)
	}
}

// TestVersionReproducible 契约：版本哈希可复现、形态为
// "<schema>:<12位hex>"、schema 前缀随宿主命名空间/升格而变。
func TestVersionReproducible(t *testing.T) {
	s := Surface{Facets: []Facet{{Name: "a", Value: map[string]any{"k": "v"}}}}
	v1, err := Fingerprint(s)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	v2, err := Fingerprint(s)
	if err != nil {
		t.Fatalf("Fingerprint(2): %v", err)
	}
	if v1 != v2 {
		t.Fatalf("同面两次指纹应一致：%s vs %s", v1, v2)
	}
	if v1.Schema != DefaultSchema {
		t.Fatalf("缺省 schema = %q，应为 %q", v1.Schema, DefaultSchema)
	}
	if len(v1.Digest) != 12 {
		t.Fatalf("48bit 摘要应为 12 个 hex 字符，got %q", v1.Digest)
	}
	if _, err := hex.DecodeString(v1.Digest); err != nil {
		t.Fatalf("摘要应为合法 hex: %v", err)
	}
	if got, want := v1.String(), DefaultSchema+":"+v1.Digest; got != want {
		t.Fatalf("String() = %q，应为 %q", got, want)
	}

	// 宿主命名空间与 schema 升格改变版本串前缀（也改变哈希——schema 进规范形）。
	s.Schema = "policyver/v1"
	v3, _ := Fingerprint(s)
	if v3.Schema != "policyver/v1" || v3.String() != "policyver/v1:"+v3.Digest {
		t.Fatalf("宿主 schema 前缀未生效：%s", v3)
	}
	s.Schema = "policyver/v2"
	v4, _ := Fingerprint(s)
	if v4.Digest == v3.Digest {
		t.Fatal("schema 升格必须换哈希（schema 进规范形）")
	}
}

// TestFacetDigestIndependentOfComposition 切片指纹只对值负责：同一 facet
// 在不同快照组合里指纹一致（按 facet 吊销可独立持久化）。
func TestFacetDigestIndependentOfComposition(t *testing.T) {
	f := Facet{Name: "role:admin", Value: []string{"read", "write"}}
	d1, err := FacetDigest(f)
	if err != nil {
		t.Fatalf("FacetDigest: %v", err)
	}
	s := Surface{Facets: []Facet{f, {Name: "other", Value: "x"}}}
	v, err := Fingerprint(s)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	_ = v
	d2, err := FacetDigest(s.Facets[0])
	if err != nil {
		t.Fatalf("FacetDigest: %v", err)
	}
	if d1 != d2 {
		t.Fatalf("切片指纹不应随组合漂移：%s vs %s", d1, d2)
	}
}

// TestFacetDiffShapes 契约：diff 形态——Added/Changed/Removed 分段、
// 各段按 Name 字典序、同版不报；确定性顺序 Added→Changed→Removed。
func TestFacetDiffShapes(t *testing.T) {
	old := Surface{Facets: []Facet{
		{Name: "admission", Value: map[string]string{"mode": "strict"}},
		{Name: "role:admin", Value: []string{"read", "write"}},
		{Name: "role:gone", Value: []string{"x"}},
	}}
	cur := Surface{Facets: []Facet{
		{Name: "admission", Value: map[string]string{"mode": "permissive"}}, // changed
		{Name: "role:admin", Value: []string{"read", "write"}},              // unchanged：不报
		{Name: "role:new", Value: []string{"y"}},                            // added
		{Name: "role:zeta", Value: []string{"z"}},                           // added（字典序在后）
	}}

	changes, err := Diff(old, cur)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	want := []Change{
		{Kind: FacetAdded, Name: "role:new", To: changes[0].To},
		{Kind: FacetAdded, Name: "role:zeta", To: changes[1].To},
		{Kind: FacetChanged, Name: "admission", From: changes[2].From, To: changes[2].To},
		{Kind: FacetRemoved, Name: "role:gone", From: changes[3].From},
	}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("diff = %+v，应为 %+v", changes, want)
	}
	for _, c := range changes {
		switch c.Kind {
		case FacetAdded:
			if c.From != "" || c.To == "" {
				t.Fatalf("added 形态应只带 To: %+v", c)
			}
		case FacetChanged:
			if c.From == "" || c.To == "" || c.From == c.To {
				t.Fatalf("changed 形态应带 From/To 且相异: %+v", c)
			}
		case FacetRemoved:
			if c.From == "" || c.To != "" {
				t.Fatalf("removed 形态应只带 From: %+v", c)
			}
		}
	}

	// 同面 diff 为空。
	same, err := Diff(old, old)
	if err != nil {
		t.Fatalf("Diff(same): %v", err)
	}
	if len(same) != 0 {
		t.Fatalf("同面 diff 应为空，got %+v", same)
	}
	if FacetAdded.String() != "added" ||
		FacetChanged.String() != "changed" ||
		FacetRemoved.String() != "removed" {
		t.Fatal("ChangeKind.String 契约漂移")
	}
}

// TestDiffSchemaMismatch 兜底升格纪律：schema 不一致直接拒绝 diff。
func TestDiffSchemaMismatch(t *testing.T) {
	old := Surface{Schema: "policyver/v1", Facets: []Facet{{Name: "a", Value: 1}}}
	cur := Surface{Schema: "policyver/v2", Facets: []Facet{{Name: "a", Value: 1}}}
	_, err := Diff(old, cur)
	if !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("schema 不一致应 ErrSchemaMismatch，got %v", err)
	}
}

// TestSortDedup 排序+去重原语：返回副本不改写入参、空入参 nil。
func TestSortDedup(t *testing.T) {
	in := []string{"write", "read", "write", "admin"}
	out := SortDedup(in)
	want := []string{"admin", "read", "write"}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("SortDedup = %v，应为 %v", out, want)
	}
	if !reflect.DeepEqual(in, []string{"write", "read", "write", "admin"}) {
		t.Fatalf("入参被改写: %v", in)
	}
	if got := SortDedup(nil); got != nil {
		t.Fatalf("空入参应 nil，got %v", got)
	}
	if got := SortDedup([]string{}); got != nil {
		t.Fatalf("空切片应 nil，got %v", got)
	}
}

// TestBadSurfaceErrors 非法快照 fail-fast 且 errors.Is 可判定。
func TestBadSurfaceErrors(t *testing.T) {
	cases := []struct {
		name string
		s    Surface
		want error
	}{
		{"空面", Surface{}, ErrBadSurface},
		{"空名", Surface{Facets: []Facet{{Name: "", Value: 1}}}, ErrBadSurface},
		{"重名", Surface{Facets: []Facet{{Name: "a", Value: 1}, {Name: "a", Value: 2}}}, ErrBadSurface},
		{"不可编码", Surface{Facets: []Facet{{Name: "a", Value: make(chan int)}}}, ErrUnencodable},
		{"函数不可编码", Surface{Facets: []Facet{{Name: "a", Value: func() {}}}}, ErrUnencodable},
	}
	for _, c := range cases {
		_, err := Fingerprint(c.s)
		if !errors.Is(err, c.want) {
			t.Fatalf("%s: 应 %v，got %v", c.name, c.want, err)
		}
	}
	// Diff 路径同样 fail-fast。
	_, err := Diff(cases[2].s, Surface{Facets: []Facet{{Name: "a", Value: 1}}})
	if !errors.Is(err, ErrBadSurface) {
		t.Fatalf("Diff 重名快照应 ErrBadSurface，got %v", err)
	}
	if _, err := FacetDigest(Facet{Name: "bad", Value: make(chan int)}); !errors.Is(err, ErrUnencodable) {
		t.Fatalf("FacetDigest 不可编码应 ErrUnencodable，got %v", err)
	}
}

// TestChangeKindStringUnknown 未知形态有兜底文案。
func TestChangeKindStringUnknown(t *testing.T) {
	if got := ChangeKind(99).String(); got != fmt.Sprintf("ChangeKind(%d)", 99) {
		t.Fatalf("未知形态 = %q", got)
	}
}
