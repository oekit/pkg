package auditchain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// GenesisHash 创世链锚：首行（无前链可衔接）的 PrevHash 取值——64 个 '0'
// （sha256 hex 长度）。空表追加首行时用它，链走查缺省要求首行衔接它。
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// Separator canonical 字段分隔符（ASCII \x1f Unit Separator）：业务可打印
// 字段里几乎不出现，天然降低拼接歧义。字段内可能出现 \x1f 时由宿主 canonical
// 先消毒——转义规则本身也是 canonical 的一部分，必须写入方/校验方一致。
const Separator = "\x1f"

// CanonicalFunc 行 → canonical 字段序（宿主单源）。字段序由宿主定义——本包
// 不落成任何具体业务行结构专用；写入方与校验方必须共享同一函数值，任何一方
// 私改字段序即全链假断。
type CanonicalFunc[T any] func(row T) []string

// Chain 审计哈希链内核（泛型）：把「字段序定义」钉成一个值，写入方与校验方
// 共享同一 *Chain 实例即共享同一 canonical。零可变状态，可并发使用。
type Chain[T any] struct {
	canonical CanonicalFunc[T]
}

// NewChain 构造链内核。canonical 为 nil 是构造期非法值 → fail-fast 报错
// （没有字段序定义的链没有语义，不能静默兜底）。
func NewChain[T any](canonical CanonicalFunc[T]) (*Chain[T], error) {
	if canonical == nil {
		return nil, errors.New("auditchain: canonical 函数不能为 nil（字段序由宿主单源定义）")
	}
	return &Chain[T]{canonical: canonical}, nil
}

// Canonicalize 行 → canonical 串（\x1f 连接宿主字段序）。写入方（算哈希前）
// 与校验方（重算哈希前）都必须经由本方法产出——canonical 单源纪律的执行点。
func (c *Chain[T]) Canonicalize(row T) string {
	return strings.Join(c.canonical(row), Separator)
}

// HashRow 行 → 链哈希（Canonicalize 后 sha256 hex）。追加侧：选定新行
// PrevHash（链尾哈希或 GenesisHash）后，对本行调用本函数落库。
func (c *Chain[T]) HashRow(row T) string {
	return Hash(c.Canonicalize(row))
}

// Hash canonical 串 → 链哈希（sha256 hex，64 字符小写）。与行类型无关——
// 校验侧对 Record.Canonical 重算哈希也走本函数。
func Hash(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// NormalizeTs 入链时间归一：UTC + 微秒截断。PG timestamptz 精度上限微秒——
// 纳秒入哈希，任何方言读回（微秒精度）重算必假断。写入方入链前必须归一；
// 这是双方言读回一致的前提，不是可选优化。
func NormalizeTs(t time.Time) time.Time {
	return t.UTC().Truncate(time.Microsecond)
}

// Record 链校验记录面：宿主把存储行适配为内核所需的最小事实四元组。
// Canonical 必须由与写入方共享的同一 *Chain.Canonicalize 产出（单源纪律）。
type Record struct {
	// ID 行标识（宿主主键字符串化；断点报告原样透出）。
	ID string
	// Canonical 行 canonical 串（共享 Chain.Canonicalize 的产物）。
	Canonical string
	// PrevHash 落库的前链哈希（首行 = GenesisHash 或锚行哈希）。
	PrevHash string
	// Hash 落库的本行链哈希。
	Hash string
}

// RecordSource 链校验行源（行迭代器接口）：宿主把 DB 分批扫描 / 内存切片 /
// SIEM 流适配成逐行拉取，行序必须按链序（写入序）。大表分批由宿主在 Next
// 内部完成，内核不做一次性载入。
type RecordSource interface {
	// Next 返回下一行；遍历结束返回 io.EOF。非 EOF 错误视为扫描故障，
	// Verify 原样上抛（与「链断」严格区分：前者走查不了，后者是数据事实）。
	Next() (Record, error)
}

// SliceSource 内存切片行源（测试 / 小型链 / SIEM 已收批数据用），按序逐行
// 吐出，结束返回 io.EOF。
func SliceSource(records []Record) RecordSource {
	return &sliceSource{records: records}
}

type sliceSource struct {
	records []Record
	i       int
}

func (s *sliceSource) Next() (Record, error) {
	if s.i >= len(s.records) {
		return Record{}, io.EOF
	}
	rec := s.records[s.i]
	s.i++
	return rec, nil
}

// VerifyResult 链校验结论（首个断点即停——后续行 PrevHash 全部对不上，多报
// 无益）。json 标签供宿主管理面直接透出。
type VerifyResult struct {
	// OK 全部已核行哈希与前后链接均通过。
	OK bool `json:"ok"`
	// Checked 已核对行数（断点即停时 = 断点所在位置）。
	Checked int64 `json:"checked"`
	// FirstBrokenID 首个断点行标识（宿主主键字符串化；OK=true 时为空）。
	FirstBrokenID string `json:"first_broken_id,omitempty"`
	// Reason 断点原因（行哈希不匹配 / 链衔接断裂，带期望与实际值短哈希）。
	Reason string `json:"reason,omitempty"`
}

// VerifyOption 校验走查选项（命名循仓规：XxxOption，禁裸 Option）。
type VerifyOption func(*verifyConfig)

// verifyConfig 走查锚定配置：首行衔接哪个前链、首行是否查前链。
type verifyConfig struct {
	expectPrev     string
	checkFirstLink bool
}

// WithAnchor 从锚行起走查：首行 PrevHash 必须衔接 anchorHash（锚行哈希）。
// 用于从链中段起查——锚行即起点前一行，锚行自身有效性由其所在链段的走查
// 负责。锚行为空串等非法锚没有静默兜底（调用方应显式改用缺省或
// WithUnanchoredHead 表达意图）。
func WithAnchor(anchorHash string) VerifyOption {
	return func(cfg *verifyConfig) {
		cfg.expectPrev = anchorHash
		cfg.checkFirstLink = true
	}
}

// WithUnanchoredHead 无锚头走查：首行只查自洽不查前链。保留期清扫截断链头的
// 部署用——头行截断本身不可由本地链证明（防截断靠外发 SIEM 侧对账），从最早
// 存活行起查时首行只查自洽，其后行仍逐一核对前后链接。
func WithUnanchoredHead() VerifyOption {
	return func(cfg *verifyConfig) {
		cfg.checkFirstLink = false
	}
}

// Verify 逐行重算哈希并核对前后链接，首个断点即停。锚定语义三档：
//   - 缺省（无选项）：首行必须衔接创世锚 GenesisHash——未清扫链头的部署
//     即完整证明；
//   - WithAnchor(h)：首行衔接锚行哈希（链中段起查）；
//   - WithUnanchoredHead()：首行只查自洽（保留清扫截断链头的部署）。
//
// 返回值语义：(res, nil) 表示走查正常完成——链断是数据事实，走 res.OK /
// FirstBrokenID / Reason 报告，不作 error；(res, err) 仅表示行源扫描故障
// （非 io.EOF），res.Checked 带已核行数，链完整性此时未知，调用方不得把
// res.OK 当作通过。
func Verify(src RecordSource, opts ...VerifyOption) (VerifyResult, error) {
	if src == nil {
		return VerifyResult{}, errors.New("auditchain: 行源不能为 nil")
	}
	cfg := verifyConfig{expectPrev: GenesisHash, checkFirstLink: true}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	res := VerifyResult{OK: true}
	for {
		rec, err := src.Next()
		if errors.Is(err, io.EOF) {
			return res, nil
		}
		if err != nil {
			return res, fmt.Errorf("auditchain: 行源扫描失败（已核 %d 行）: %w", res.Checked, err)
		}
		res.Checked++
		if got := Hash(rec.Canonical); got != rec.Hash {
			res.OK = false
			res.FirstBrokenID = rec.ID
			res.Reason = "行哈希不匹配（内容被改或写入损坏）"
			return res, nil
		}
		if cfg.checkFirstLink && rec.PrevHash != cfg.expectPrev {
			res.OK = false
			res.FirstBrokenID = rec.ID
			res.Reason = fmt.Sprintf("链衔接断裂（应指向前行 %s，实为 %s）",
				shortHash(cfg.expectPrev), shortHash(rec.PrevHash))
			return res, nil
		}
		cfg.checkFirstLink = true
		cfg.expectPrev = rec.Hash
	}
}

// shortHash 断点原因里的短哈希（前 12 字符），完整哈希不进人类可读文案。
func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
