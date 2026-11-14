package licensx

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// hostClaims 宿主载荷示例（customer/features 是宿主产品决策，不入包）。
type hostClaims struct {
	Customer string   `json:"customer"`
	Features []string `json:"features,omitempty"`
}

// testKeyPair 测试专用密钥对。**仅测试用**——生产私钥在运营方手里，永不入仓；
// 验签公钥由宿主编译进二进制后注入本包 API。
func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := Keygen()
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func baseTime() time.Time { return time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC) }

// signedLicense 签发一份载荷完整的 license（签发-验签闭环的公共前置）。
func signedLicense(t *testing.T, priv ed25519.PrivateKey) *License {
	t.Helper()
	payload, err := json.Marshal(hostClaims{Customer: "acme", Features: []string{"sso", "siem"}})
	if err != nil {
		t.Fatal(err)
	}
	l := &License{
		Serial:   "2026-001",
		IssuedAt: baseTime(),
		Payload:  payload,
	}
	if err := Sign(l, priv); err != nil {
		t.Fatal(err)
	}
	return l
}

// TestKeygen 密钥对形态：公钥 32B / 私钥 64B（Ed25519 标准尺寸）。
func TestKeygen(t *testing.T) {
	pub, priv := testKeyPair(t)
	if len(pub) != ed25519.PublicKeySize {
		t.Fatalf("公钥长度 = %d, want %d", len(pub), ed25519.PublicKeySize)
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Fatalf("私钥长度 = %d, want %d", len(priv), ed25519.PrivateKeySize)
	}
}

// TestSignVerifyLoop 签发-验签闭环：签名写回、验签通过、JSON 文件形态
// round-trip 后仍验签通过；Ed25519 确定性——同一 license 两次签发签名一致。
func TestSignVerifyLoop(t *testing.T) {
	pub, priv := testKeyPair(t)
	l := signedLicense(t, priv)
	if l.Signature == "" {
		t.Fatal("签名未写回")
	}
	if err := l.Verify(pub); err != nil {
		t.Fatalf("验签应通过: %v", err)
	}

	// JSON 文件形态 round-trip（Load 读回的就是这份字节）。
	b, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	var back License
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if err := back.Verify(pub); err != nil {
		t.Fatalf("文件 round-trip 后验签应通过: %v", err)
	}

	// 签名确定性（同钥同内容 → 同签名）：固定序载荷下签发可复现。
	again := *l
	again.Signature = ""
	if err := Sign(&again, priv); err != nil {
		t.Fatal(err)
	}
	if again.Signature != l.Signature {
		t.Fatal("同一 license 两次签发的签名应一致（Ed25519 确定性）")
	}
}

// TestVerifyTamper 篡改载荷/信封任一字段：验签必败且挂 ErrSignatureInvalid
// 哨兵（判据是 errors.Is，不是错误文本）——防篡改是本机制的核心，不是一个字段。
func TestVerifyTamper(t *testing.T) {
	pub, priv := testKeyPair(t)
	l := signedLicense(t, priv)

	tamperedPayload, err := json.Marshal(hostClaims{Customer: "evil", Features: []string{"sso", "siem"}})
	if err != nil {
		t.Fatal(err)
	}
	exp := baseTime().Add(24 * time.Hour)
	mutations := map[string]func(*License){
		"serial":     func(x *License) { x.Serial = "2026-002" },
		"issued_at":  func(x *License) { x.IssuedAt = baseTime().Add(time.Hour) },
		"expires_at": func(x *License) { x.ExpiresAt = &exp },
		"payload":    func(x *License) { x.Payload = tamperedPayload },
		"signature": func(x *License) {
			// 首字符翻转（确定性：不依赖签名里恰好含哪个字符）。
			if strings.HasPrefix(x.Signature, "A") {
				x.Signature = "B" + x.Signature[1:]
			} else {
				x.Signature = "A" + x.Signature[1:]
			}
		},
	}
	for name, mut := range mutations {
		t.Run("篡改 "+name, func(t *testing.T) {
			cp := *l
			mut(&cp)
			err := cp.Verify(pub)
			if err == nil {
				t.Fatal("篡改后应验签失败")
			}
			if !errors.Is(err, ErrSignatureInvalid) {
				t.Fatalf("应挂 ErrSignatureInvalid 哨兵，得: %v", err)
			}
		})
	}
}

// TestVerifyWrongKey 换公钥（非签发方那把）→ 验签必败；正确公钥应通过。
func TestVerifyWrongKey(t *testing.T) {
	pub, priv := testKeyPair(t)
	l := signedLicense(t, priv)
	if err := l.Verify(pub); err != nil {
		t.Fatalf("签发方公钥应验签通过: %v", err)
	}
	otherPub, _ := testKeyPair(t)
	if err := l.Verify(otherPub); err == nil {
		t.Fatal("非签发方公钥应验签失败")
	} else if !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("应挂 ErrSignatureInvalid 哨兵，得: %v", err)
	}
	if err := l.Verify(nil); err == nil || !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("非法长度公钥应挂哨兵，得: %v", err)
	}
	_ = priv
}

// TestVerifyBadSignatureFormat 签名格式非法（非 base64 / 长度不对 / 空签名）
// → 验签必败（fail-loud，不静默当无效后放行）。
func TestVerifyBadSignatureFormat(t *testing.T) {
	pub, priv := testKeyPair(t)
	l := signedLicense(t, priv)
	cases := map[string]string{
		"非 base64": "not-base64!!!",
		"空签名":      "",
		"长度不对":     "AAAA",
	}
	for name, sig := range cases {
		t.Run(name, func(t *testing.T) {
			cp := *l
			cp.Signature = sig
			if err := cp.Verify(pub); err == nil || !errors.Is(err, ErrSignatureInvalid) {
				t.Fatalf("非法签名应挂 ErrSignatureInvalid，得: %v", err)
			}
		})
	}
}

// TestVerifyInvalidPayloadJSON 载荷不是合法 JSON → 验签必败且挂
// ErrSignatureInvalid（被改到连序列化都过不了的文件，更谈不上签名有效）。
func TestVerifyInvalidPayloadJSON(t *testing.T) {
	pub, priv := testKeyPair(t)
	l := signedLicense(t, priv)
	cp := *l
	cp.Payload = json.RawMessage("{not json")
	if err := cp.Verify(pub); err == nil || !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("非法 JSON 载荷应挂 ErrSignatureInvalid，得: %v", err)
	}
}

// TestPayloadFixedOrder 载荷字节逐字进签：同一 license 的载荷若键序不同
// （map 序列化产物），签名对不上——固定序序列化（struct marshal）是纪律。
func TestPayloadFixedOrder(t *testing.T) {
	pub, priv := testKeyPair(t)
	l := signedLicense(t, priv)

	// 语义相同但键序不同的载荷字节（map 序列化产物无顺序保证）。
	reordered := json.RawMessage(`{"features":["sso","siem"],"customer":"acme"}`)
	cp := *l
	cp.Payload = reordered
	if err := cp.Verify(pub); err == nil || !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("键序不同的载荷字节验签应失败（签名覆盖原始字节），得: %v", err)
	}
	_ = priv
}

// TestValidateExpiry 过期判定：过期必败挂 ErrExpired；到期时刻本身仍有效
// （After 严格判据）；nil/零值 = 永久授权。
func TestValidateExpiry(t *testing.T) {
	pub, priv := testKeyPair(t)

	mk := func(exp *time.Time) *License {
		l := &License{Serial: "2026-001", IssuedAt: baseTime(), ExpiresAt: exp}
		l.Payload, _ = json.Marshal(hostClaims{Customer: "acme"})
		if err := Sign(l, priv); err != nil {
			t.Fatal(err)
		}
		return l
	}

	t.Run("已过期 → ErrExpired", func(t *testing.T) {
		exp := baseTime().Add(-time.Hour)
		if err := mk(&exp).Validate(pub, baseTime()); err == nil || !errors.Is(err, ErrExpired) {
			t.Fatalf("应挂 ErrExpired 哨兵，得: %v", err)
		}
	})
	t.Run("到期时刻本身仍有效（After 严格判据）", func(t *testing.T) {
		exp := baseTime()
		if err := mk(&exp).Validate(pub, exp); err != nil {
			t.Fatalf("到期时刻应仍有效: %v", err)
		}
	})
	t.Run("过期后 1 微秒失效", func(t *testing.T) {
		exp := baseTime()
		err := mk(&exp).Validate(pub, exp.Add(time.Microsecond))
		if err == nil || !errors.Is(err, ErrExpired) {
			t.Fatalf("过期后应失效: %v", err)
		}
	})
	t.Run("nil ExpiresAt = 永久", func(t *testing.T) {
		if err := mk(nil).Validate(pub, baseTime().Add(10*365*24*time.Hour)); err != nil {
			t.Fatalf("永久授权不应过期: %v", err)
		}
	})
	t.Run("零值 ExpiresAt = 永久", func(t *testing.T) {
		zero := time.Time{}
		if err := mk(&zero).Validate(pub, baseTime().Add(10*365*24*time.Hour)); err != nil {
			t.Fatalf("零值到期应视为永久: %v", err)
		}
	})
}

// TestValidateRevoked 吊销判定：序列号命中清单必败挂 ErrRevoked；未命中 /
// 空清单 / nil 清单（显式无吊销）均有效；无序列号的 license 不参与吊销。
func TestValidateRevoked(t *testing.T) {
	pub, priv := testKeyPair(t)

	mk := func(serial string) *License {
		l := &License{Serial: serial, IssuedAt: baseTime()}
		l.Payload, _ = json.Marshal(hostClaims{Customer: "acme"})
		if err := Sign(l, priv); err != nil {
			t.Fatal(err)
		}
		return l
	}

	t.Run("命中吊销清单 → ErrRevoked", func(t *testing.T) {
		err := mk("2026-001").Validate(pub, baseTime(), WithRevokedSerials([]string{"2026-001", "2026-002"}))
		if err == nil || !errors.Is(err, ErrRevoked) {
			t.Fatalf("应挂 ErrRevoked 哨兵，得: %v", err)
		}
	})
	t.Run("未命中 → 有效", func(t *testing.T) {
		if err := mk("2026-009").Validate(pub, baseTime(), WithRevokedSerials([]string{"2026-001"})); err != nil {
			t.Fatalf("未命中吊销清单应有效: %v", err)
		}
	})
	t.Run("nil 清单 = 显式无吊销", func(t *testing.T) {
		if err := mk("2026-001").Validate(pub, baseTime(), WithRevokedSerials(nil)); err != nil {
			t.Fatalf("nil 清单应有效: %v", err)
		}
	})
	t.Run("无序列号不参与吊销（清单含空串也不误伤）", func(t *testing.T) {
		if err := mk("").Validate(pub, baseTime(), WithRevokedSerials([]string{""})); err != nil {
			t.Fatalf("空序列号不应被吊销清单误伤: %v", err)
		}
	})
}

// TestValidateSignatureGate 签名不过先报签名：篡改 + 吊销同时成立时报
// ErrSignatureInvalid（过期/吊销只在签名可信后判定）。
func TestValidateSignatureGate(t *testing.T) {
	pub, priv := testKeyPair(t)
	l := signedLicense(t, priv)
	l.Serial = "evil-serial" // 篡改
	err := l.Validate(pub, baseTime(), WithRevokedSerials([]string{"evil-serial"}))
	if err == nil || !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("签名门应先行，得: %v", err)
	}
}

// TestLoadBranches Load 分支：无路径/不存在/解析失败/验签失败/过期/吊销各形态
// 非 Valid 且必带 Reason（降级不静默）；有效形态字段全透出。
func TestLoadBranches(t *testing.T) {
	pub, priv := testKeyPair(t)
	dir := t.TempDir()

	write := func(name string, l *License) string {
		p := filepath.Join(dir, name)
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// good 签发一份到期时间为 expiresAt 的有效 license（到期时间必须在
	// 签名**前**设定——签名覆盖除 signature 外全部字段）。
	good := func(expiresAt *time.Time) *License {
		payload, err := json.Marshal(hostClaims{Customer: "acme", Features: []string{"sso"}})
		if err != nil {
			t.Fatal(err)
		}
		l := &License{Serial: "2026-001", IssuedAt: baseTime(), ExpiresAt: expiresAt, Payload: payload}
		if err := Sign(l, priv); err != nil {
			t.Fatal(err)
		}
		return l
	}

	future := baseTime().Add(24 * time.Hour)
	now := baseTime().Add(time.Hour)

	t.Run("空路径 → 非 Valid + 原因", func(t *testing.T) {
		st := Load("", pub, now)
		if st.Valid || st.Loaded {
			t.Fatalf("st = %+v, want 非 Valid 且未加载", st)
		}
		if st.Reason == "" {
			t.Fatal("必须给出原因（不静默）")
		}
	})
	t.Run("文件不存在 → 非 Valid + 原因", func(t *testing.T) {
		st := Load(filepath.Join(dir, "nope.json"), pub, now)
		if st.Valid || st.Loaded {
			t.Fatalf("st = %+v, want 非 Valid 且未加载", st)
		}
		if st.Reason == "" {
			t.Fatal("必须给出原因（不静默）")
		}
	})
	t.Run("解析失败 → 非 Valid + 原因", func(t *testing.T) {
		p := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		st := Load(p, pub, now)
		if st.Valid || st.Loaded {
			t.Fatalf("st = %+v, want 非 Valid 且未加载", st)
		}
		if st.Reason == "" {
			t.Fatal("必须给出原因（不静默）")
		}
	})
	t.Run("验签失败（篡改）→ 非 Valid + 原因（防篡改：拿不到授权能力）", func(t *testing.T) {
		l := good(&future)
		l.Serial = "tampered"
		st := Load(write("tampered.json", l), pub, now)
		if st.Valid || !st.Loaded {
			t.Fatalf("st = %+v, want 已加载但非 Valid（篡改不得生效）", st)
		}
		if st.Reason == "" {
			t.Fatal("必须给出原因（不静默）")
		}
	})
	t.Run("已过期 → 非 Valid + 原因", func(t *testing.T) {
		past := baseTime().Add(-time.Hour)
		st := Load(write("expired.json", good(&past)), pub, now)
		if st.Valid {
			t.Fatalf("st = %+v, want 非 Valid（过期不得生效）", st)
		}
		if !strings.Contains(st.Reason, "过期") {
			t.Fatalf("reason = %q, 应指明过期", st.Reason)
		}
	})
	t.Run("已吊销 → 非 Valid + 原因", func(t *testing.T) {
		st := Load(write("good.json", good(&future)), pub, now, WithRevokedSerials([]string{"2026-001"}))
		if st.Valid {
			t.Fatalf("st = %+v, want 非 Valid（吊销不得生效）", st)
		}
		if !strings.Contains(st.Reason, "吊销") {
			t.Fatalf("reason = %q, 应指明吊销", st.Reason)
		}
	})
	t.Run("有效 → Valid 且字段全透出", func(t *testing.T) {
		st := Load(write("good.json", good(&future)), pub, now)
		if !st.Valid || !st.Loaded {
			t.Fatalf("st = %+v, want 有效", st)
		}
		if st.Reason != "" {
			t.Fatalf("完全有效不应有 reason: %q", st.Reason)
		}
		if st.Serial != "2026-001" || st.ExpiresAt == nil {
			t.Fatalf("st = %+v, 字段未透出", st)
		}
		var claims hostClaims
		if err := json.Unmarshal(st.Payload, &claims); err != nil || claims.Customer != "acme" {
			t.Fatalf("载荷未透出: %v %+v", err, claims)
		}
	})
	t.Run("永久授权 → Valid", func(t *testing.T) {
		if st := Load(write("forever.json", good(nil)), pub, now); !st.Valid {
			t.Fatalf("st = %+v, want 有效（永久授权）", st)
		}
	})
}

// TestDecode 载荷解码：按宿主 claims 结构解码；空载荷/nil license fail-loud。
func TestDecode(t *testing.T) {
	_, priv := testKeyPair(t)
	l := signedLicense(t, priv)

	var claims hostClaims
	if err := l.Decode(&claims); err != nil {
		t.Fatalf("解码应成功: %v", err)
	}
	if claims.Customer != "acme" || len(claims.Features) != 2 {
		t.Fatalf("claims = %+v, 字段不对", claims)
	}

	empty := &License{}
	if err := empty.Decode(&claims); err == nil {
		t.Fatal("空载荷应报错（fail-loud）")
	}
	var nilLicense *License
	if err := nilLicense.Decode(&claims); err == nil {
		t.Fatal("nil license 应报错（fail-loud）")
	}
}

// TestNilLicenseFailLoud nil license 与非法私钥 fail-loud（不 panic、不静默）。
func TestNilLicenseFailLoud(t *testing.T) {
	pub, _ := testKeyPair(t)
	var nilLicense *License
	if err := nilLicense.Verify(pub); err == nil || !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("nil license 验签应挂哨兵，得: %v", err)
	}
	if err := Sign(nil, nil); err == nil {
		t.Fatal("nil license 签发应报错")
	}
	l := &License{}
	if err := Sign(l, nil); err == nil {
		t.Fatal("非法私钥应报错")
	}
}
