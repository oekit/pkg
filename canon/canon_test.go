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

// TestFingerprintBytesHostLayout 契约：宿主自产规范形字节（历史布局）经
// FingerprintBytes 复算，与历史 12-hex 指纹逐字一致。合成本例的布局刻意
// 与 canon 的 canonicalDoc 不同（facets 是数组、带额外字段、键序也不同），
// 证明本入口只哈希、不重排、不重新规范化。
func TestFingerprintBytesHostLayout(t *testing.T) {
	host := []byte(`{"scope":"tenant-a","v":3,"facets":[{"k":"alpha","d":"1a2b3c4d5e6f"},{"k":"beta","d":"abcdef123456"}]}`)
	const wantDigest = "bfa439c8f3d1" // sha256(host) 前 48bit：历史实现同值

	v, err := FingerprintBytes("hostver/v3", host)
	if err != nil {
		t.Fatalf("FingerprintBytes: %v", err)
	}
	if v.Schema != "hostver/v3" || v.Digest != wantDigest {
		t.Fatalf("FingerprintBytes = %s，应为 hostver/v3:%s", v, wantDigest)
	}
	if got, want := v.String(), "hostver/v3:"+wantDigest; got != want {
		t.Fatalf("String() = %q，应为 %q", got, want)
	}

	// schema 只是版本串前缀，不参与哈希：换命名空间/升格标签不改摘要。
	// （与 Fingerprint 不同——后者把 schema 写进规范形再哈希。）
	v2, err := FingerprintBytes("hostver/v4", host)
	if err != nil {
		t.Fatalf("FingerprintBytes(schema v4): %v", err)
	}
	if v2.Digest != wantDigest {
		t.Fatalf("schema 不应进哈希：%s vs %s", v2, v)
	}

	// 空 schema 回退 DefaultSchema；同一字节重复计算稳定。
	v3, err := FingerprintBytes("", host)
	if err != nil {
		t.Fatalf("FingerprintBytes(空 schema): %v", err)
	}
	if v3.Schema != DefaultSchema {
		t.Fatalf("缺省 schema = %q，应为 %q", v3.Schema, DefaultSchema)
	}
	v4, _ := FingerprintBytes("hostver/v3", host)
	if v4 != v {
		t.Fatalf("同一字节应稳定同版：%s vs %s", v4, v)
	}

	// 字节变则指纹变（不规范化，但哈希必须敏感）。
	changed := []byte(`{"scope":"tenant-a","v":3,"facets":[{"k":"alpha","d":"1a2b3c4d5e6f"},{"k":"beta","d":"abcdef123457"}]}`)
	v5, _ := FingerprintBytes("hostver/v3", changed)
	if v5.Digest == wantDigest {
		t.Fatal("规范形字节变化必须换指纹")
	}
}

// TestFingerprintBytesEmptyRejected 空规范形字节 fail-fast：空面无版本可言
// （与 [Fingerprint] 拒绝空面同口径），且 errors.Is 可判定。
func TestFingerprintBytesEmptyRejected(t *testing.T) {
	for _, b := range [][]byte{nil, {}} {
		if _, err := FingerprintBytes("hostver/v1", b); !errors.Is(err, ErrBadSurface) {
			t.Fatalf("空字节应 ErrBadSurface，got %v", err)
		}
	}
}

// TestFingerprintSharesBytesKernel 契约：Fingerprint 与 FingerprintBytes
// 走同一条哈希/版本格式化管线——同一份规范形字节得到同一 Version。
func TestFingerprintSharesBytesKernel(t *testing.T) {
	s := Surface{Schema: "hostver/v2", Facets: []Facet{{Name: "alpha", Value: []string{"a", "b"}}}}
	b, err := canonicalBytes(s)
	if err != nil {
		t.Fatalf("canonicalBytes: %v", err)
	}
	v, err := Fingerprint(s)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	vb, err := FingerprintBytes(s.Schema, b)
	if err != nil {
		t.Fatalf("FingerprintBytes: %v", err)
	}
	if v != vb {
		t.Fatalf("Fingerprint 与 FingerprintBytes 应同值：%s vs %s", v, vb)
	}
}

// TestDiffDigests 契约：直接吃两代 facet 指纹 map，输出与 [Diff] 同口径的
// Added/Changed/Removed；顺序确定（Added→Changed→Removed，各段按名排序），
// 同指纹不报，nil/空 map 合法。
func TestDiffDigests(t *testing.T) {
	old := map[string]string{
		"alpha": "111111111111",
		"beta":  "222222222222", // unchanged
		"gamma": "333333333333", // changed
	}
	cur := map[string]string{
		"epsilon": "555555555555",
		"delta":   "444444444444",
		"gamma":   "999999999999",
		"beta":    "222222222222",
	}
	want := []Change{
		{Kind: FacetAdded, Name: "delta", To: "444444444444"},
		{Kind: FacetAdded, Name: "epsilon", To: "555555555555"},
		{Kind: FacetChanged, Name: "gamma", From: "333333333333", To: "999999999999"},
		{Kind: FacetRemoved, Name: "alpha", From: "111111111111"},
	}
	// 多轮重复：map 迭代序随机，顺序必须由函数保证而非碰巧。
	for i := 0; i < 50; i++ {
		if got := DiffDigests(old, cur); !reflect.DeepEqual(got, want) {
			t.Fatalf("第 %d 轮 DiffDigests = %+v，应为 %+v", i+1, got, want)
		}
	}

	if got := DiffDigests(nil, nil); len(got) != 0 {
		t.Fatalf("nil/nil 应无变更，got %+v", got)
	}
	if got := DiffDigests(old, old); len(got) != 0 {
		t.Fatalf("同指纹应无变更，got %+v", got)
	}
	addedOnly := DiffDigests(nil, map[string]string{"alpha": "1"})
	if len(addedOnly) != 1 || addedOnly[0].Kind != FacetAdded || addedOnly[0].From != "" {
		t.Fatalf("nil → 非空应只报 added，got %+v", addedOnly)
	}
	removedOnly := DiffDigests(map[string]string{"alpha": "1"}, nil)
	if len(removedOnly) != 1 || removedOnly[0].Kind != FacetRemoved || removedOnly[0].To != "" {
		t.Fatalf("非空 → nil 应只报 removed，got %+v", removedOnly)
	}
}

// TestDiffDigestsMatchesDiff 契约：Diff 与 DiffDigests 同口径——前者是
// 「快照 → 指纹 map → 后者」的薄包装，输出必须逐项一致。
func TestDiffDigestsMatchesDiff(t *testing.T) {
	old := Surface{Schema: "hostver/v1", Facets: []Facet{
		{Name: "alpha", Value: []string{"a"}},
		{Name: "beta", Value: map[string]string{"k": "v"}},
		{Name: "gamma", Value: 3},
	}}
	cur := Surface{Schema: "hostver/v1", Facets: []Facet{
		{Name: "beta", Value: map[string]string{"k": "v2"}},
		{Name: "delta", Value: []string{"d"}},
		{Name: "epsilon", Value: true},
	}}
	oldD, err := facetDigests(old)
	if err != nil {
		t.Fatalf("facetDigests(old): %v", err)
	}
	curD, err := facetDigests(cur)
	if err != nil {
		t.Fatalf("facetDigests(cur): %v", err)
	}
	viaSurface, err := Diff(old, cur)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	viaDigests := DiffDigests(oldD, curD)
	if !reflect.DeepEqual(viaSurface, viaDigests) {
		t.Fatalf("Diff 与 DiffDigests 不一致：%+v vs %+v", viaSurface, viaDigests)
	}
}
