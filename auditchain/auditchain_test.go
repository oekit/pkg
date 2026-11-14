package auditchain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// auditRow 测试行结构（模拟宿主业务行：自增 ID + 归一时间 + 业务字段 + 前链）。
// canonical 字段序由本测试「宿主」定义——与 bianque repo_auditlog.go 同构。
type auditRow struct {
	ID       uint64
	Ts       time.Time
	Actor    string
	Action   string
	Detail   string
	PrevHash string
	Hash     string
}

// testChain 测试用链内核（宿主字段序单源：ID/Ts/Actor/Action/Detail/PrevHash）。
func testChain(t *testing.T) *Chain[auditRow] {
	t.Helper()
	chain, err := NewChain(func(row auditRow) []string {
		return []string{
			strconv.FormatUint(row.ID, 10),
			strconv.FormatInt(row.Ts.UnixNano(), 10),
			row.Actor, row.Action, row.Detail, row.PrevHash,
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return chain
}

// appendRows 模拟宿主追加流：逐行选前链（链尾或创世锚）→ 归一时间 → 算哈希落库。
func appendRows(chain *Chain[auditRow], base uint64, rows ...auditRow) []auditRow {
	lastHash := ""
	out := make([]auditRow, 0, len(rows))
	for _, row := range rows {
		row.ID = base + uint64(len(out)) + 1
		row.Ts = NormalizeTs(row.Ts)
		if lastHash == "" {
			row.PrevHash = GenesisHash
		} else {
			row.PrevHash = lastHash
		}
		row.Hash = chain.HashRow(row)
		out = append(out, row)
		lastHash = row.Hash
	}
	return out
}

// records 存储行 → 走查记录（校验方必须经同一 chain.Canonicalize 产出——单源纪律）。
func records(chain *Chain[auditRow], rows []auditRow) []Record {
	out := make([]Record, 0, len(rows))
	for _, row := range rows {
		out = append(out, Record{
			ID:        strconv.FormatUint(row.ID, 10),
			Canonical: chain.Canonicalize(row),
			PrevHash:  row.PrevHash,
			Hash:      row.Hash,
		})
	}
	return out
}

func baseTime() time.Time {
	return time.Date(2026, 10, 22, 8, 30, 0, 123456789, time.FixedZone("CST", 8*3600))
}

func sampleRows() []auditRow {
	return []auditRow{
		{Ts: baseTime(), Actor: "alice", Action: "team.create", Detail: `{"name":"ep"}`},
		{Ts: baseTime().Add(time.Second), Actor: "alice", Action: "member.add", Detail: `{"uid":42}`},
		{Ts: baseTime().Add(2 * time.Second), Actor: "bob", Action: "team.rename", Detail: `{"name":"ep2"}`},
		{Ts: baseTime().Add(3 * time.Second), Actor: "bob", Action: "member.remove", Detail: `{"uid":7}`},
		{Ts: baseTime().Add(4 * time.Second), Actor: "carol", Action: "key.rotate", Detail: `{}`},
	}
}

// TestAppendVerifyLoop 追加-校验闭环：合法链全绿，逐行哈希与前后链接全过。
func TestAppendVerifyLoop(t *testing.T) {
	chain := testChain(t)
	rows := appendRows(chain, 0, sampleRows()...)
	res, err := Verify(SliceSource(records(chain, rows)))
	if err != nil {
		t.Fatalf("走查不应有扫描故障: %v", err)
	}
	if !res.OK {
		t.Fatalf("合法链应全绿: %+v", res)
	}
	if res.Checked != int64(len(rows)) {
		t.Fatalf("Checked = %d, want %d", res.Checked, len(rows))
	}
	if res.FirstBrokenID != "" || res.Reason != "" {
		t.Fatalf("全绿链不应有断点: %+v", res)
	}
}

// TestVerifyBreaksOnTamper 篡改任一业务字段：verify 必断于被改行（首个断点
// 即停——Checked 停在断点，后续行不再核）。
func TestVerifyBreaksOnTamper(t *testing.T) {
	chain := testChain(t)
	cases := []struct {
		name   string
		mutate func(*auditRow)
	}{
		{"actor", func(r *auditRow) { r.Actor = "mallory" }},
		{"action", func(r *auditRow) { r.Action = "auth.bypass" }},
		{"detail", func(r *auditRow) { r.Detail = `{"amount":99999}` }},
		{"ts", func(r *auditRow) { r.Ts = r.Ts.Add(time.Minute) }},
		{"prev_hash", func(r *auditRow) { r.PrevHash = strings.Repeat("f", 64) }},
	}
	for _, tc := range cases {
		t.Run("篡改 "+tc.name, func(t *testing.T) {
			rows := appendRows(chain, 0, sampleRows()...)
			recs := records(chain, rows)
			const at = 2 // 第三行（下标 2）
			mutated := rows[at]
			tc.mutate(&mutated)
			recs[at].Canonical = chain.Canonicalize(mutated) // 走查读到被改后的行内容，落库哈希不变

			res, err := Verify(SliceSource(recs))
			if err != nil {
				t.Fatalf("篡改是链断不是扫描故障: %v", err)
			}
			if res.OK {
				t.Fatal("篡改后 verify 必断")
			}
			if res.FirstBrokenID != strconv.FormatUint(rows[at].ID, 10) {
				t.Fatalf("断点 = %s, want 被改行 %d", res.FirstBrokenID, rows[at].ID)
			}
			if res.Checked != at+1 {
				t.Fatalf("Checked = %d, want 首个断点即停 %d", res.Checked, at+1)
			}
			if !strings.Contains(res.Reason, "行哈希不匹配") {
				t.Fatalf("reason = %q, 应指明行哈希不匹配", res.Reason)
			}
		})
	}
}

// TestVerifyBreaksOnDeletedRow 删行：中间行被删，后行 PrevHash 指向已删行
// 哈希 → 后行链衔接断裂（断点在删除点的下一行）。
func TestVerifyBreaksOnDeletedRow(t *testing.T) {
	chain := testChain(t)
	rows := appendRows(chain, 0, sampleRows()...)
	const deleted = 2 // 删第三行
	recs := records(chain, append(rows[:deleted:deleted], rows[deleted+1:]...))

	res, err := Verify(SliceSource(recs))
	if err != nil {
		t.Fatalf("删行是链断不是扫描故障: %v", err)
	}
	if res.OK {
		t.Fatal("删行后 verify 必断")
	}
	want := strconv.FormatUint(rows[deleted+1].ID, 10)
	if res.FirstBrokenID != want {
		t.Fatalf("断点 = %s, want %s（删除点的下一行）", res.FirstBrokenID, want)
	}
	if !strings.Contains(res.Reason, "链衔接断裂") {
		t.Fatalf("reason = %q, 应指明链衔接断裂", res.Reason)
	}
}

// TestVerifyBreaksOnReorder 重排行：链序被打乱，挪动行的前链对不上它的新前驱。
func TestVerifyBreaksOnReorder(t *testing.T) {
	chain := testChain(t)
	rows := appendRows(chain, 0, sampleRows()...)
	recs := records(chain, rows)
	recs[1], recs[2] = recs[2], recs[1] // 交换第二、三行的走查顺序

	res, err := Verify(SliceSource(recs))
	if err != nil {
		t.Fatalf("重排是链断不是扫描故障: %v", err)
	}
	if res.OK {
		t.Fatal("重排后 verify 必断")
	}
	if res.FirstBrokenID != recs[1].ID {
		t.Fatalf("断点 = %s, want 被挪动行 %s", res.FirstBrokenID, recs[1].ID)
	}
	if res.Checked != 2 {
		t.Fatalf("Checked = %d, want 首个断点即停 2", res.Checked)
	}
}

// TestGenesisAnchorForm 创世锚形态：64 个 '0'（sha256 hex 长度）；首行哈希
// 自洽但未衔接创世锚 → 链衔接断裂（断点=首行）；衔接创世锚则过。
func TestGenesisAnchorForm(t *testing.T) {
	if len(GenesisHash) != 64 {
		t.Fatalf("创世锚长度 = %d, want sha256 hex 64", len(GenesisHash))
	}
	for _, c := range GenesisHash {
		if c != '0' {
			t.Fatalf("创世锚应全 '0'，出现 %q", c)
		}
	}

	chain := testChain(t)
	rows := appendRows(chain, 0, sampleRows()...)
	// 首行改前链并重算自身哈希：行自洽但不是从创世锚起链。
	rows[0].PrevHash = strings.Repeat("a", 64)
	rows[0].Hash = chain.HashRow(rows[0])

	res, err := Verify(SliceSource(records(chain, rows)))
	if err != nil {
		t.Fatalf("走查不应有扫描故障: %v", err)
	}
	if res.OK {
		t.Fatal("未衔接创世锚的首行必须断（缺省锚定语义）")
	}
	if res.FirstBrokenID != strconv.FormatUint(rows[0].ID, 10) {
		t.Fatalf("断点 = %s, want 首行 %d", res.FirstBrokenID, rows[0].ID)
	}
	if !strings.Contains(res.Reason, "链衔接断裂") {
		t.Fatalf("reason = %q, 应指明链衔接断裂", res.Reason)
	}
}

// TestWithAnchorMidChain 锚定走查（链中段）：从锚行哈希衔接起查全绿；锚错则断。
func TestWithAnchorMidChain(t *testing.T) {
	chain := testChain(t)
	rows := appendRows(chain, 0, sampleRows()...)
	const from = 3 // 从第四行起查，锚 = 第三行落库哈希
	recs := records(chain, rows[from:])

	res, err := Verify(SliceSource(recs), WithAnchor(rows[from-1].Hash))
	if err != nil {
		t.Fatalf("走查不应有扫描故障: %v", err)
	}
	if !res.OK || res.Checked != int64(len(recs)) {
		t.Fatalf("衔接锚行的中段走查应全绿: %+v", res)
	}

	// 锚错（拿第二行哈希当第三行的）→ 首行链衔接断裂。
	res, err = Verify(SliceSource(recs), WithAnchor(rows[from-2].Hash))
	if err != nil {
		t.Fatalf("走查不应有扫描故障: %v", err)
	}
	if res.OK || res.FirstBrokenID != recs[0].ID || !strings.Contains(res.Reason, "链衔接断裂") {
		t.Fatalf("错锚应断于首行: %+v", res)
	}
}

// TestWithUnanchoredHead 保留清扫截断链头的部署：首行 PrevHash 指向已删行，
// 缺省锚定断于首行；WithUnanchoredHead 首行只查自洽、其后行仍核对前链。
func TestWithUnanchoredHead(t *testing.T) {
	chain := testChain(t)
	rows := appendRows(chain, 0, sampleRows()...)
	swept := rows[1:] // 链头一行被保留期清扫，余行 PrevHash 原样指向已删行
	recs := records(chain, swept)

	res, err := Verify(SliceSource(recs))
	if err != nil {
		t.Fatalf("走查不应有扫描故障: %v", err)
	}
	if res.OK || res.FirstBrokenID != strconv.FormatUint(swept[0].ID, 10) {
		t.Fatalf("缺省锚定下截断链头应断于首行: %+v", res)
	}

	res, err = Verify(SliceSource(recs), WithUnanchoredHead())
	if err != nil {
		t.Fatalf("走查不应有扫描故障: %v", err)
	}
	if !res.OK || res.Checked != int64(len(swept)) {
		t.Fatalf("无锚头走查应全绿（首行只查自洽）: %+v", res)
	}

	// 无锚头档不豁免后续链接：再删中间一行仍必断。
	const deleted = 1
	recs2 := records(chain, append(swept[:deleted:deleted], swept[deleted+1:]...))
	res, err = Verify(SliceSource(recs2), WithUnanchoredHead())
	if err != nil {
		t.Fatalf("走查不应有扫描故障: %v", err)
	}
	if res.OK || res.FirstBrokenID != strconv.FormatUint(swept[deleted+1].ID, 10) {
		t.Fatalf("无锚头档下删行仍应断于删除点下一行: %+v", res)
	}
}

// TestNormalizeTs 时间归一：UTC + 微秒截断；归一后与「方言读回」（微秒精度
// 重建）哈希一致；不归一（纳秒入链）则读回重算必假断——这正是归一存在的原因。
func TestNormalizeTs(t *testing.T) {
	in := baseTime()
	got := NormalizeTs(in)
	if got.Location() != time.UTC {
		t.Fatalf("归一后应转 UTC，得 %v", got.Location())
	}
	if got.Nanosecond()%1000 != 0 {
		t.Fatalf("归一后应微秒截断，纳秒余数 %d", got.Nanosecond()%1000)
	}
	if got.UnixMicro() != in.UnixMicro() {
		t.Fatalf("微秒以内的值应保留: got %d, want %d", got.UnixMicro(), in.UnixMicro())
	}

	chain := testChain(t)
	row := auditRow{ID: 1, Actor: "alice", Action: "op", Detail: "d", PrevHash: GenesisHash}

	// 归一入链 vs 方言读回（微秒精度、任意时区重建）：哈希必须一致。
	row.Ts = NormalizeTs(in)
	wantHash := chain.HashRow(row)
	readBack := auditRow{ID: 1, Actor: "alice", Action: "op", Detail: "d", PrevHash: GenesisHash,
		Ts: time.UnixMicro(row.Ts.UnixMicro()).In(time.FixedZone("EST", -5*3600))}
	if gotHash := chain.HashRow(readBack); gotHash != wantHash {
		t.Fatalf("归一入链与微秒精度读回哈希应一致:\n got %s\nwant %s", gotHash, wantHash)
	}

	// 不归一（纳秒入链）vs 读回：必假断——归一不是可选优化。
	row.Ts = in // 带纳秒
	if gotHash := chain.HashRow(readBack); gotHash == chain.HashRow(row) {
		t.Fatal("纳秒入链在微秒精度读回下应假断，却得到相同哈希")
	}
}

// TestSeparatorDiscipline 分隔符纪律：canonical 用 \x1f 连接；同字段集不同
// 切分不撞串；字段内含 \x1f 可构造等值 canonical——消毒责任在宿主（文档纪律
// 的可执行示例）。
func TestSeparatorDiscipline(t *testing.T) {
	chain, err := NewChain(func(fields []string) []string { return fields })
	if err != nil {
		t.Fatal(err)
	}
	got := chain.Canonicalize([]string{"ab", "c"})
	if !strings.Contains(got, Separator) {
		t.Fatalf("canonical 应含 \\x1f 分隔符: %q", got)
	}
	if chain.Canonicalize([]string{"a", "bc"}) == got {
		t.Fatal("不同字段切分不应撞同一 canonical")
	}
	// 宿主未消毒的反例：字段内含 \x1f 与多字段等值——这就是 canonical 必须
	// 双方共享 + 宿主消毒的原因（内核不做猜测性转义）。
	if chain.Canonicalize([]string{"a" + Separator + "b"}) != chain.Canonicalize([]string{"a", "b"}) {
		t.Fatal("含分隔符字段应演示出等值风险（消毒责任在宿主）")
	}
}

// TestNewChainNilCanonical 构造期 fail-fast：nil canonical 直接报错。
func TestNewChainNilCanonical(t *testing.T) {
	if _, err := NewChain[auditRow](nil); err == nil {
		t.Fatal("nil canonical 应构造失败")
	}
}

// TestVerifyEmptyAndNilSource 空链（空表）走查全绿 Checked=0；nil 行源
// fail-loud 报错不 panic。
func TestVerifyEmptyAndNilSource(t *testing.T) {
	res, err := Verify(SliceSource(nil))
	if err != nil || !res.OK || res.Checked != 0 {
		t.Fatalf("空链应全绿: res=%+v err=%v", res, err)
	}
	if _, err := Verify(nil); err == nil {
		t.Fatal("nil 行源应报错")
	}
}

// errorSource 固定行数后报扫描故障的行源（区分「链断」与「走查不了」）。
type errorSource struct {
	records []Record
	err     error
	i       int
}

func (s *errorSource) Next() (Record, error) {
	if s.i >= len(s.records) {
		return Record{}, s.err
	}
	rec := s.records[s.i]
	s.i++
	return rec, nil
}

// TestVerifySourceError 行源扫描故障原样上抛（带已核行数上下文），不与链断
// 混淆；此时 OK 不可作为通过判据。
func TestVerifySourceError(t *testing.T) {
	chain := testChain(t)
	rows := appendRows(chain, 0, sampleRows()...)
	boom := errors.New("db gone")
	recs := records(chain, rows[:2])

	res, err := Verify(&errorSource{records: recs, err: boom})
	if err == nil {
		t.Fatal("扫描故障应上抛")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("扫描故障应可 errors.Is 追根: %v", err)
	}
	if res.Checked != 2 {
		t.Fatalf("Checked = %d, want 已核 2 行", res.Checked)
	}
	if !strings.Contains(err.Error(), "已核 2 行") {
		t.Fatalf("错误应带已核行数上下文: %v", err)
	}
	if fmt.Sprint(res.OK) != "true" {
		t.Fatalf("OK 语义是「已核行全过」，扫描故障不翻绿也不判红: %+v", res)
	}
}
