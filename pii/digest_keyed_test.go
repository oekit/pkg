package pii

// #293 判据先于实现：hash 档的摘要必须**密钥化**（域分离 HMAC），缺密钥 fail-closed。
//
// 缺陷（修复前）：`encoding.Sha256(v+"|"+kind)[:16]`——盐是公开常量、无密钥、
// 截断 64 bit。手机号域约 9×10⁹，离线字典枚举即可还原；与 policy.go 对外承诺的
// 「不可逆」正好相反。本文件把验收标准逐条钉死：
//
//	同值同摘要、异值异摘要（回归护栏）；
//	注入密钥后换密钥 ⇒ 摘要变化；有限步字典枚举不得还原原值（真值就在候选集里）；
//	摘要不等于无盐 sha256 前缀；
//	去掉密钥注入（或密钥校验）⇒ 对应用例转红。

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/oekit/pkg/errors"
)

// testDigestKey / testDigestKey2：16 字节，正好压在「最短可用」下限上，
// 顺带钉住密钥长度门槛不是 32/64 之类的更高值。
const (
	testDigestKey  = "0123456789abcdef"
	testDigestKey2 = "fedcba9876543210"
)

// unsaltedPrefix 旧实现（无盐截断 sha256）的复算基准——用例自算，不依赖包内实现，
// 这样「摘要不得等于它」才是独立于实现的判据。
func unsaltedPrefix(v string, kind Kind) string {
	sum := sha256.Sum256([]byte(v + "|" + string(kind)))
	return hex.EncodeToString(sum[:])[:16]
}

// TestHashWithoutKeyFailsClosed 缺密钥时 hash 档必须**拒绝出摘要**：
// 整段替换 + 要求阻断（与未登记面名同档），绝不静默退回无盐摘要。
//
// 反向验证：把缺密钥分支改回「照常出无盐摘要」⇒ 本用例转红。
func TestHashWithoutKeyFailsClosed(t *testing.T) {
	restore(t)
	if err := setDigestKeyForTest(t, ""); err != nil {
		t.Fatal(err)
	}
	if err := setStrategyForTest(t, StrategyHash); err != nil {
		t.Fatal(err)
	}

	out, blocked := Apply(SurfaceInput, "tel "+synMobile)
	if !blocked {
		t.Fatal("缺密钥的 hash 档必须 fail-closed：应回报阻断信号")
	}
	if out != "tel "+redactedPlaceholder {
		t.Fatalf("缺密钥时不得产出摘要，应整段替换，实得 %q", out)
	}
	if strings.Contains(out, unsaltedPrefix(synMobile, KindMobile)) {
		t.Fatalf("缺密钥时绝不能退回无盐摘要形态：%q", out)
	}

	// 值路径同理：整棵载荷都拿不到摘要。
	v, b := ApplyValue(SurfaceInput, map[string]string{"k": synMobile})
	if !b {
		t.Fatal("值路径缺密钥同样应回报阻断")
	}
	if got := v.(map[string]string)["k"]; got != redactedPlaceholder {
		t.Fatalf("值路径缺密钥不得产出摘要，实得 %q", got)
	}
}

// TestHashKeyedCorrelatable 注入密钥后：可关联性保住（同值同摘要、异值异摘要）、
// 换密钥摘要必变、且不等于无盐 sha256 前缀。
//
// 反向验证：让实现忽略注入的密钥（退回无盐摘要）⇒ 「换密钥摘要必变」与
// 「不等于无盐前缀」两条断言当场转红。
func TestHashKeyedCorrelatable(t *testing.T) {
	restore(t)
	if err := setDigestKeyForTest(t, testDigestKey); err != nil {
		t.Fatal(err)
	}
	if err := setStrategyForTest(t, StrategyHash); err != nil {
		t.Fatal(err)
	}

	h1, blocked := Apply(SurfaceOutput, synMobile)
	if blocked {
		t.Fatal("注入密钥后 hash 档不应阻断")
	}
	if h2, _ := Apply(SurfaceOutput, synMobile); h1 != h2 {
		t.Fatalf("同值同摘要（可关联性）必须保住: %q vs %q", h1, h2)
	}
	if len(h1) != 16 {
		t.Fatalf("摘要应保持 16 位 hex（64 bit 截断），实得 %q", h1)
	}
	if other, _ := Apply(SurfaceOutput, synID); other == h1 {
		t.Fatal("异值应产出异摘要")
	}

	// 换密钥 ⇒ 摘要变化（密钥确实参与了摘要，不是摆设）。
	if err := setDigestKeyForTest(t, testDigestKey2); err != nil {
		t.Fatal(err)
	}
	h3, _ := Apply(SurfaceOutput, synMobile)
	if h3 == h1 {
		t.Fatal("换密钥后摘要必须变化——密钥没有参与摘要")
	}
	if h3 == unsaltedPrefix(synMobile, KindMobile) {
		t.Fatalf("摘要不得等于无盐 sha256 前缀（缺陷形态）：%q", h3)
	}
}

// TestHashResistsDomainEnumeration 有限步字典枚举不得还原原值。
//
// 攻击者模型：拿到摘要、知道域分离口径（kind 串公开），**不知道密钥**。
// 按旧缺陷口径对 1380000**** 全域 10⁴ 候选复算无盐 sha256 前缀——
// 候选集**包含真值**（synMobile = 13800001234）。旧实现下第一个候选区间内
// 必然命中；密钥化后整个枚举一次都不该命中。
func TestHashResistsDomainEnumeration(t *testing.T) {
	restore(t)
	if err := setDigestKeyForTest(t, testDigestKey); err != nil {
		t.Fatal(err)
	}
	if err := setStrategyForTest(t, StrategyHash); err != nil {
		t.Fatal(err)
	}

	digest, _ := Apply(SurfaceOutput, synMobile)
	if len(digest) != 16 {
		t.Fatalf("基准摘要形态异常: %q", digest)
	}
	for i := 0; i < 10000; i++ {
		cand := fmt.Sprintf("1380000%04d", i)
		if digest == unsaltedPrefix(cand, KindMobile) {
			t.Fatalf("无盐字典枚举命中 %s——摘要退回了可离线穷举的缺陷形态", cand)
		}
	}
}

// TestSetDigestKeyGuards 密钥注入面自身的护栏：过短密钥拒绝且不生效
// （1 字节密钥可被离线爆破，等于没锁）；空串显式清除。
func TestSetDigestKeyGuards(t *testing.T) {
	restore(t)

	if err := setDigestKeyForTest(t, "short"); !errors.Is(err, ErrDigestKeyTooShort) {
		t.Fatalf("过短密钥应报 ErrDigestKeyTooShort，实得 %v", err)
	}
	if DigestKeyed() {
		t.Fatal("被拒绝的密钥不得生效")
	}
	if err := setDigestKeyForTest(t, testDigestKey); err != nil {
		t.Fatal(err)
	}
	if !DigestKeyed() {
		t.Fatal("注入后 DigestKeyed 应为真")
	}
	if err := setDigestKeyForTest(t, ""); err != nil {
		t.Fatal(err)
	}
	if DigestKeyed() {
		t.Fatal("空串应显式清除密钥")
	}
}
