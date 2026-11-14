package licensx

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"
)

// 哨兵错误（errors.Is 判定，消费方不要匹配错误文本）。
var (
	// ErrSignatureInvalid 签名校验失败（文件被篡改 / 公钥不匹配 / 签名或公钥
	// 格式非法 / 载荷不是合法 JSON）。fail-loud：不提供任何「验签失败当有效」
	// 的降级路径。
	ErrSignatureInvalid = errors.New("license 签名校验失败")
	// ErrExpired license 已过期（Validate 判定；ExpiresAt nil/零值 = 永久授权）。
	ErrExpired = errors.New("license 已过期")
	// ErrRevoked license 序列号在吊销清单内（清单由宿主从签发侧分发/拉取）。
	ErrRevoked = errors.New("license 已吊销")
)

// License license 文件内容（JSON 信封）：载荷是宿主定义的任意 JSON，签名覆盖
// 除 Signature 外的全部字段——改任何一项都会验签失败。
type License struct {
	// Serial 序列号（签发侧编号；吊销清单的键。可选，但要做吊销就必须有）。
	Serial string `json:"serial,omitempty"`
	// IssuedAt 签发时间。
	IssuedAt time.Time `json:"issued_at,omitempty"`
	// ExpiresAt 到期时间；nil/零值 = 永久授权。
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// Payload 宿主载荷（任意 JSON）。必须固定序序列化：用 json.Marshal 宿主
	// struct 产出（字段序 = 声明序），禁 map——键序随机则同一 license 两次
	// 序列化字节不同，签名随之不确定。签名覆盖本字段的字节（经 encoding/json
	// 紧凑化，键序保持）。
	Payload json.RawMessage `json:"payload"`
	// Signature base64 的 Ed25519 签名（覆盖除本字段外的全部字段）。
	Signature string `json:"signature,omitempty"`
}

// payload 签名覆盖的内容（除 signature 外全部字段）。独立 struct 保证字段
// 顺序确定（encoding/json 按 struct 声明序输出）→ 签与验用同一份字节，不依赖
// map 迭代序。⚠️ 新增参与签名的字段必须同步加到这里——本 struct 就是签名面
// 白名单，只加进 License 而不加进 payload 的字段不会被签名保护。
type payload struct {
	Serial    string          `json:"serial,omitempty"`
	IssuedAt  time.Time       `json:"issued_at,omitempty"`
	ExpiresAt *time.Time      `json:"expires_at,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

// payload 取签名载荷（license → payload；Sign 与 Verify 共用这一处）。
func (l *License) payload() payload {
	return payload{
		Serial:    l.Serial,
		IssuedAt:  l.IssuedAt,
		ExpiresAt: l.ExpiresAt,
		Payload:   l.Payload,
	}
}

// Keygen 生成新的签名密钥对（供首次签发与轮换）。**私钥必须写到仓外路径**
// ——只存运营方签发侧，永不入仓、不进验签侧；公钥怎么交付（编译期常量 vs
// 配置注入）是宿主策略。
func Keygen() (pub ed25519.PublicKey, priv ed25519.PrivateKey, err error) {
	return ed25519.GenerateKey(rand.Reader)
}

// Sign 对给定 license 计算签名并写回 Signature 字段。**私钥在运营方手里**，
// 本函数只应出现在签发侧工具里，不在产品运行期路径上。
func Sign(l *License, priv ed25519.PrivateKey) error {
	if l == nil {
		return errors.New("licensx: 待签 license 为 nil")
	}
	if len(priv) != ed25519.PrivateKeySize {
		return fmt.Errorf("licensx: 私钥长度非法: %d 字节（期望 %d）", len(priv), ed25519.PrivateKeySize)
	}
	b, err := json.Marshal(l.payload())
	if err != nil {
		return fmt.Errorf("licensx: 签名载荷序列化失败: %w", err)
	}
	l.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, b))
	return nil
}

// Verify 用给定公钥验签（纯签名面：不含过期/吊销——那是授权生命周期，
// 见 Validate）。失败返回 ErrSignatureInvalid 包装的错误（fail-loud）。
func (l *License) Verify(pub ed25519.PublicKey) error {
	if l == nil {
		return fmt.Errorf("%w: license 为 nil", ErrSignatureInvalid)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: 公钥长度非法（%d 字节）", ErrSignatureInvalid, len(pub))
	}
	sig, err := base64.StdEncoding.DecodeString(l.Signature)
	if err != nil {
		return fmt.Errorf("%w: 签名不是合法 base64（%v）", ErrSignatureInvalid, err)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: 签名长度非法（%d 字节）", ErrSignatureInvalid, len(sig))
	}
	b, err := json.Marshal(l.payload())
	if err != nil {
		return fmt.Errorf("%w: 签名载荷序列化失败（%v）——Payload 必须是合法 JSON（json.Marshal 宿主 struct 产出）",
			ErrSignatureInvalid, err)
	}
	if !ed25519.Verify(pub, b, sig) {
		return fmt.Errorf("%w: 内容与签名不匹配（文件被改动，或公钥不是签发方那把）", ErrSignatureInvalid)
	}
	return nil
}

// ValidateOption Validate 的选项（命名循仓规：XxxOption，禁裸 Option）。
type ValidateOption func(*validateConfig)

// validateConfig 授权生命周期校验配置。
type validateConfig struct {
	revoked []string
}

// WithRevokedSerials 注入吊销清单（序列号集合；由宿主从签发侧分发/拉取并
// 传入）。nil/空 = 显式无吊销（回退默认，不算非法）。
func WithRevokedSerials(serials []string) ValidateOption {
	return func(cfg *validateConfig) {
		cfg.revoked = serials
	}
}

// Validate 全量校验：验签 + 过期 + 吊销（授权生命周期面）。now 由调用方注入
// （测试确定性；生产传 time.Now()）。失败返回类型化哨兵（ErrSignatureInvalid
// / ErrExpired / ErrRevoked）包装的错误——签名不过先报签名，过期/吊销只在
// 签名可信后判定。
func (l *License) Validate(pub ed25519.PublicKey, now time.Time, opts ...ValidateOption) error {
	if err := l.Verify(pub); err != nil {
		return err
	}
	if l.ExpiresAt != nil && !l.ExpiresAt.IsZero() && now.After(*l.ExpiresAt) {
		return fmt.Errorf("%w: 于 %s 过期（当前 %s）", ErrExpired,
			l.ExpiresAt.Format(time.RFC3339), now.Format(time.RFC3339))
	}
	cfg := validateConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if l.Serial != "" && slices.Contains(cfg.revoked, l.Serial) {
		return fmt.Errorf("%w: 序列号 %s 在吊销清单内", ErrRevoked, l.Serial)
	}
	return nil
}

// Decode 把载荷解码到宿主 claims 结构（Validate 通过后调用——载荷结构是
// 宿主产品决策，本包不解码也不解释业务字段）。
func (l *License) Decode(v any) error {
	if l == nil {
		return errors.New("licensx: license 为 nil")
	}
	if len(l.Payload) == 0 {
		return errors.New("licensx: 载荷为空（无宿主 claims 可解码）")
	}
	return json.Unmarshal(l.Payload, v)
}

// Status license 加载结果（机制结论，不含档位决策——Valid → 宿主档位的
// 映射是产品侧的事）。自解释：非有效形态必带 Reason（降级不静默）。
type Status struct {
	// Loaded 文件存在且 JSON 解析成功（license 骨架可读）。
	Loaded bool `json:"loaded"`
	// Valid 签名 + 未过期 + 未吊销 全过。
	Valid bool `json:"valid"`
	// Serial 序列号（解析出来即透出，无论有效与否——吊销对账用）。
	Serial string `json:"serial,omitempty"`
	// ExpiresAt 到期时间（nil = 永久）。
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// Payload 载荷原始字节（Valid 后宿主自行解码到自己的 claims 结构）。
	Payload json.RawMessage `json:"payload,omitempty"`
	// Reason 未装/无效/已过期/已吊销的原因（完全有效时为空）——不静默。
	Reason string `json:"reason,omitempty"`
}

// Load 加载并校验 license 文件（机制内核；公钥由宿主注入——编译期常量或
// 配置都是宿主策略）。语义：
//   - path 为空或文件不存在 → 非 Valid + 明确 Reason（无授权运行，不阻塞
//     启动——要让运维**看见**授权状态，而不是让人只知道「起不来」）；
//   - 文件存在但解析失败/验签失败/已过期/已吊销 → Loaded 可读 + 非 Valid +
//     明确 Reason（防篡改靠「无效 license 拿不到授权能力」，由宿主把
//     Valid 映射到档位兑现）。
func Load(path string, pub ed25519.PublicKey, now time.Time, opts ...ValidateOption) Status {
	if path == "" {
		return Status{Reason: "未配置 license 文件（无授权运行）"}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Status{Reason: "license 文件不存在（无授权运行）"}
		}
		return Status{Reason: "license 文件读取失败: " + err.Error()}
	}
	var l License
	if err := json.Unmarshal(raw, &l); err != nil {
		return Status{Reason: "license 文件解析失败: " + err.Error()}
	}
	st := Status{Loaded: true, Serial: l.Serial, ExpiresAt: l.ExpiresAt, Payload: l.Payload}
	if err := l.Validate(pub, now, opts...); err != nil {
		st.Reason = err.Error()
		return st
	}
	st.Valid = true
	return st
}
