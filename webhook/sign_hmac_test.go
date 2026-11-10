package webhook

// 发出侧 HMAC 签名器测试（#325/#328）：纯函数 helper 与独立实现逐字节
// 对照、Signer↔Validator round-trip 互逆、推荐约定头（事件/幂等 ID/时间
// 戳）、幂等 ID 形态与唯一性、构造期 fail-closed。

import (
	"crypto"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestSignHMAC_MatchesValidator SignHMAC 产出与「测试自己的独立 hmac 实现 +
// 验签器解码路径」逐字节对照——前缀/编码由同一组 HMACOption 驱动的三种
// 组合都要对上（#325 主张：验证器怎么读、签名器就怎么写）。
func TestSignHMAC_MatchesValidator(t *testing.T) {
	payload := []byte(`{"action":"opened"}`)
	secret := []byte(testSecret)
	raw := signRaw(t, secret, sha256.New, payload)

	cases := []struct {
		name string
		opts []HMACOption
		want string
	}{
		{"裸hex缺省", nil, hex.EncodeToString(raw)},
		{"GitHub前缀hex", []HMACOption{WithSignaturePrefix("sha256=")}, "sha256=" + hex.EncodeToString(raw)},
		{"base64", []HMACOption{WithBase64()}, base64.StdEncoding.EncodeToString(raw)},
		{"前缀+base64", []HMACOption{WithSignaturePrefix("sha256="), WithBase64()}, "sha256=" + base64.StdEncoding.EncodeToString(raw)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SignHMAC(secret, crypto.SHA256, payload, tc.opts...); got != tc.want {
				t.Fatalf("签名值应为 %s, got %s", tc.want, got)
			}
			// 同选项构造的验证器必须接受该签名值（单源 round-trip）
			v := NewHMACValidator(secret, crypto.SHA256, "X-Sig", tc.opts...)
			if err := v.Validate(mkReq(t, map[string]string{"X-Sig": tc.want}, payload), payload); err != nil {
				t.Fatalf("SignHMAC 的产出喂同配验证器应通过, got %v", err)
			}
		})
	}
}

// TestSignTimestampHMAC_GiteeForm 与独立实现的 Gitee 口径对照（ts+"\n"+
// secret 的 HMAC-base64），并喂给 TimestampHMACValidator 通过——message
// 组装单源（giteeMessage）两侧一致。
func TestSignTimestampHMAC_GiteeForm(t *testing.T) {
	const secret = "ts-sign-secret"
	ts := itoa(fixedClock.UnixMilli())
	body := []byte("ignored")

	want := giteeSign(t, secret, ts, body) // 测试独立实现
	if got := SignTimestampHMAC([]byte(secret), crypto.SHA256, ts); got != want {
		t.Fatalf("Gitee 口径签名应为 %s, got %s", want, got)
	}

	// 验签侧同口径必须通过（容忍窗内假时钟）
	v := NewTimestampHMACValidator([]byte(secret), crypto.SHA256, "TS", "SIG",
		WithClock(func() time.Time { return fixedClock }))
	req := mkReq(t, map[string]string{"TS": ts, "SIG": want}, body)
	if err := v.Validate(req, body); err != nil {
		t.Fatalf("SignTimestampHMAC 产出应通过同口径验证器, got %v", err)
	}
}

func TestSignTimestampHMAC_FailClosed(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"空secret", func() { _ = SignTimestampHMAC(nil, crypto.SHA256, "1") }},
		{"零值算法", func() { _ = SignTimestampHMAC([]byte("s"), crypto.Hash(0), "1") }},
		{"白名单外算法", func() { _ = SignTimestampHMAC([]byte("s"), crypto.MD5, "1") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("构造期非法值应 panic（仓规总则①）")
				}
			}()
			tc.fn()
		})
	}
}

// TestHMACSigner_RoundTrip 泛型签名器 ↔ 泛型验证器互逆：同 secret 同选项
// 下 Sign 产出的 header 喂 Validate 必过；换 secret / 篡改 body 必死（发
// 出的签名是真的在防篡改，不是装饰）。
func TestHMACSigner_RoundTrip(t *testing.T) {
	payload := []byte(`{"event":"forwarded","n":1}`)

	cases := []struct {
		name     string
		signOpts []HMACSignerOption
		valOpts  []HMACOption
	}{
		{"缺省hex", nil, nil},
		{"前缀hex", []HMACSignerOption{SignerWithSignaturePrefix("sha256=")}, []HMACOption{WithSignaturePrefix("sha256=")}},
		{"base64", []HMACSignerOption{SignerWithBase64()}, []HMACOption{WithBase64()}},
		{"前缀+base64", []HMACSignerOption{SignerWithSignaturePrefix("v1="), SignerWithBase64()}, []HMACOption{WithSignaturePrefix("v1="), WithBase64()}},
		{"收侧回退链主header命中", []HMACSignerOption{SignerWithSignaturePrefix("sha256=")}, []HMACOption{WithSignaturePrefix("sha256="), WithFallbackHeaders("X-Sig-Old")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewHMACSigner([]byte(testSecret), crypto.SHA256, "X-Sig", tc.signOpts...)
			v := NewHMACValidator([]byte(testSecret), crypto.SHA256, "X-Sig", tc.valOpts...)

			hdr, err := s.Sign(payload)
			if err != nil {
				t.Fatalf("Sign 不应出错, got %v", err)
			}
			req := mkReq(t, map[string]string{"X-Sig": hdr.Get("X-Sig")}, payload)
			if err := v.Validate(req, payload); err != nil {
				t.Fatalf("同配 round-trip 应通过, got %v", err)
			}

			// 错 secret 的签名必死
			bad := NewHMACSigner([]byte("other"), crypto.SHA256, "X-Sig", tc.signOpts...)
			hdrBad, _ := bad.Sign(payload)
			req = mkReq(t, map[string]string{"X-Sig": hdrBad.Get("X-Sig")}, payload)
			if err := v.Validate(req, payload); err == nil {
				t.Fatal("错 secret 签名应被拒")
			}

			// 篡改 body 必死
			tampered := []byte(`{"event":"forwarded","n":2}`)
			req = mkReq(t, map[string]string{"X-Sig": hdr.Get("X-Sig")}, tampered)
			if err := v.Validate(req, tampered); err == nil {
				t.Fatal("篡改 body 后原签名应被拒")
			}
		})
	}
}

// TestHMACSigner_StandardHeaders 推荐约定输出（#328 设计点 2）：事件头 +
// 幂等投递 ID 头（<producer>-<event>-<uuidv4>）+ 时间戳头（unix 秒，时钟
// 注入确定性判定）。
func TestHMACSigner_StandardHeaders(t *testing.T) {
	s := NewHMACSigner([]byte(testSecret), crypto.SHA256, "X-Sig",
		SignerWithEvent("sync_run"),
		SignerWithProducer("git-ferry"),
		SignerWithTimestampHeader(DefaultTimestampHeader),
		SignerWithClock(func() time.Time { return fixedClock }),
	)

	hdr, err := s.Sign([]byte("payload"))
	if err != nil {
		t.Fatalf("Sign 不应出错, got %v", err)
	}
	if got := hdr.Get(DefaultEventHeader); got != "sync_run" {
		t.Fatalf("事件头应为 sync_run, got %q", got)
	}
	if got := hdr.Get(DefaultTimestampHeader); got != itoa(fixedClock.Unix()) {
		t.Fatalf("时间戳头应为 %s（unix 秒）, got %q", itoa(fixedClock.Unix()), got)
	}
	id := hdr.Get(DefaultDeliveryHeader)
	if !strings.HasPrefix(id, "git-ferry-sync_run-") {
		t.Fatalf("投递 ID 应为 <producer>-<event>-<uuid> 形态, got %q", id)
	}
	// uuid 段是 RFC 4122 v4 形态
	uuid := strings.TrimPrefix(id, "git-ferry-sync_run-")
	if !uuidV4Re.MatchString(uuid) {
		t.Fatalf("ID 的 uuid 段应为 v4 形态, got %q", uuid)
	}

	// 两次 Sign 的投递 ID 必不同（幂等 ID 是投递唯一键，重复即消费端误去重）
	hdr2, _ := s.Sign([]byte("payload"))
	if hdr.Get(DefaultDeliveryHeader) == hdr2.Get(DefaultDeliveryHeader) {
		t.Fatal("两次投递的幂等 ID 不应相同")
	}
}

var uuidV4Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// TestNewEventID 推荐形态分段规则：producer+event 全量、缺段退化、纯 uuid。
func TestNewEventID(t *testing.T) {
	full, err := NewEventID("prod", "push")
	if err != nil {
		t.Fatalf("不应出错, got %v", err)
	}
	if !strings.HasPrefix(full, "prod-push-") || !uuidV4Re.MatchString(strings.TrimPrefix(full, "prod-push-")) {
		t.Fatalf("全量形态不符, got %q", full)
	}
	eventOnly, _ := NewEventID("", "push")
	if !strings.HasPrefix(eventOnly, "push-") {
		t.Fatalf("缺 producer 应退化为 <event>-<uuid>, got %q", eventOnly)
	}
	producerOnly, _ := NewEventID("prod", "")
	if !strings.HasPrefix(producerOnly, "prod-") {
		t.Fatalf("缺 event 应退化为 <producer>-<uuid>, got %q", producerOnly)
	}
	bare, _ := NewEventID("", "")
	if !uuidV4Re.MatchString(bare) {
		t.Fatalf("双缺应为纯 uuid, got %q", bare)
	}
	// 两次生成不同
	if full2, _ := NewEventID("prod", "push"); full == full2 {
		t.Fatal("两次生成的 ID 不应相同")
	}
}

// TestHMACSigner_CustomDeliveryID 已有分布式 ID 的服务接入：自定义生成器
// 优先于推荐形态；返回空串在 Sign 时判死（空 ID 使消费端幂等去重失效）。
func TestHMACSigner_CustomDeliveryID(t *testing.T) {
	s := NewHMACSigner([]byte(testSecret), crypto.SHA256, "X-Sig",
		SignerWithEvent("push"), SignerWithDeliveryID(func() string { return "snowflake-123" }))
	hdr, err := s.Sign([]byte("p"))
	if err != nil {
		t.Fatalf("Sign 不应出错, got %v", err)
	}
	if got := hdr.Get(DefaultDeliveryHeader); got != "snowflake-123" {
		t.Fatalf("自定义 ID 应原样输出, got %q", got)
	}

	bad := NewHMACSigner([]byte(testSecret), crypto.SHA256, "X-Sig",
		SignerWithEvent("push"), SignerWithDeliveryID(func() string { return "" }))
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("空投递 ID 应 panic（fail-fast，仓规总则①）")
			}
		}()
		_, _ = bad.Sign([]byte("p"))
	}()
}

func TestHMACSigner_FailClosed(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"空secret", func() { _ = NewHMACSigner(nil, crypto.SHA256, "X-Sig") }},
		{"空secret切片", func() { _ = NewHMACSigner([]byte{}, crypto.SHA256, "X-Sig") }},
		{"零值算法", func() { _ = NewHMACSigner([]byte("s"), crypto.Hash(0), "X-Sig") }},
		{"白名单外算法", func() { _ = NewHMACSigner([]byte("s"), crypto.MD5, "X-Sig") }},
		{"空header", func() { _ = NewHMACSigner([]byte("s"), crypto.SHA256, "") }},
		{"nil选项", func() { _ = NewHMACSigner([]byte("s"), crypto.SHA256, "X-Sig", nil) }},
		{"空前缀", func() { _ = NewHMACSigner([]byte("s"), crypto.SHA256, "X-Sig", SignerWithSignaturePrefix("")) }},
		{"空事件", func() { _ = NewHMACSigner([]byte("s"), crypto.SHA256, "X-Sig", SignerWithEvent("")) }},
		{"空生产者", func() { _ = NewHMACSigner([]byte("s"), crypto.SHA256, "X-Sig", SignerWithProducer("")) }},
		{"nil投递ID生成器", func() { _ = NewHMACSigner([]byte("s"), crypto.SHA256, "X-Sig", SignerWithDeliveryID(nil)) }},
		{"空时间戳header名", func() { _ = NewHMACSigner([]byte("s"), crypto.SHA256, "X-Sig", SignerWithTimestampHeader("")) }},
		{"nil时钟", func() { _ = NewHMACSigner([]byte("s"), crypto.SHA256, "X-Sig", SignerWithClock(nil)) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("构造期非法值应 panic（仓规总则①）")
				}
			}()
			tc.fn()
		})
	}
}
