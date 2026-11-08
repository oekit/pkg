package webhook

// HMAC 验签器家族测试（#270）：多算法、hex/base64 双编码、前缀剥离、
// header 回退链、算法正确性（RFC 4231 向量锚点）、构造期 fail-closed。

import (
	"bytes"
	"crypto"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"hash"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testSecret = "webhook-secret-2026"

// signRaw 用测试自己的 hmac 实现（独立于被测实现）计算期望签名。
func signRaw(t *testing.T, secret []byte, newHash func() hash.Hash, body []byte) []byte {
	t.Helper()
	mac := hmac.New(newHash, secret)
	mac.Write(body)
	return mac.Sum(nil)
}

func mkReq(t *testing.T, headers map[string]string, body []byte) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

// TestHMAC_RFC4231Vector RFC 4231 Test Case 2 锚点：key="Jefe"（4 字节），
// data="what do ya want for nothing?"，HMAC-SHA256 =
// 5bdcc146bf60754e6a0464fe4c6dcb28129e30cba7a51e82f39cf8cda9ba0f9e。
// 钉住「算法正确」而不只是「实现自洽」。
func TestHMAC_RFC4231Vector(t *testing.T) {
	key := []byte("Jefe")
	data := []byte("what do ya want for nothing?")
	want := "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"

	v := NewHMACValidator(key, crypto.SHA256, "X-Sig")
	if got := hex.EncodeToString(v.sum(data)); got != want {
		t.Fatalf("HMAC-SHA256 应匹配 RFC 4231 向量, got %s want %s", got, want)
	}
}

func TestHMACValidator_Table(t *testing.T) {
	payload := []byte(`{"action":"opened","n":1}`)
	sha256hex := hex.EncodeToString(signRaw(t, []byte(testSecret), sha256.New, payload))

	cases := []struct {
		name    string
		v       *HMACValidator
		headers map[string]string
		body    []byte // nil = payload
		wantErr error
	}{
		{
			name:    "GitHub双形态-sha256前缀剥离",
			v:       NewHMACValidator([]byte(testSecret), crypto.SHA256, "X-Sig", WithSignaturePrefix("sha256=")),
			headers: map[string]string{"X-Sig": "sha256=" + sha256hex},
		},
		{
			name:    "GitHub双形态-裸hex",
			v:       NewHMACValidator([]byte(testSecret), crypto.SHA256, "X-Sig", WithSignaturePrefix("sha256=")),
			headers: map[string]string{"X-Sig": sha256hex},
		},
		{
			name:    "前缀大小写不敏感",
			v:       NewHMACValidator([]byte(testSecret), crypto.SHA256, "X-Sig", WithSignaturePrefix("sha256=")),
			headers: map[string]string{"X-Sig": "SHA256=" + sha256hex},
		},
		{
			name:    "base64编码",
			v:       NewHMACValidator([]byte(testSecret), crypto.SHA256, "X-Sig", WithBase64()),
			headers: map[string]string{"X-Sig": base64.StdEncoding.EncodeToString(signRaw(t, []byte(testSecret), sha256.New, payload))},
		},
		{
			name:    "签名正确通过",
			v:       NewHMACValidator([]byte(testSecret), crypto.SHA256, "X-Sig"),
			headers: map[string]string{"X-Sig": sha256hex},
		},
		{
			name:    "签名不符-ErrBadSignature",
			v:       NewHMACValidator([]byte(testSecret), crypto.SHA256, "X-Sig"),
			headers: map[string]string{"X-Sig": hex.EncodeToString(signRaw(t, []byte("other"), sha256.New, payload))},
			wantErr: ErrBadSignature,
		},
		{
			name:    "body被篡改-ErrBadSignature",
			v:       NewHMACValidator([]byte(testSecret), crypto.SHA256, "X-Sig"),
			headers: map[string]string{"X-Sig": sha256hex}, // 签名对应原 body，请求体不同
			body:    []byte(`{"action":"opened","n":2}`),
			wantErr: ErrBadSignature,
		},
		{
			name:    "header缺席-ErrMissingSignature",
			v:       NewHMACValidator([]byte(testSecret), crypto.SHA256, "X-Sig"),
			headers: map[string]string{},
			wantErr: ErrMissingSignature,
		},
		{
			name:    "解码失败-ErrBadSignature",
			v:       NewHMACValidator([]byte(testSecret), crypto.SHA256, "X-Sig"),
			headers: map[string]string{"X-Sig": "zzzz-not-hex"},
			wantErr: ErrBadSignature,
		},
		{
			name:    "错前缀不剥离-裸解码失败",
			v:       NewHMACValidator([]byte(testSecret), crypto.SHA256, "X-Sig", WithSignaturePrefix("sha256=")),
			headers: map[string]string{"X-Sig": "sha1=" + sha256hex},
			wantErr: ErrBadSignature,
		},
		{
			name:    "header回退链-主header缺席用回退",
			v:       NewHMACValidator([]byte(testSecret), crypto.SHA256, "X-New", WithFallbackHeaders("X-Old")),
			headers: map[string]string{"X-Old": sha256hex},
		},
		{
			name:    "header回退链-主header优先",
			v:       NewHMACValidator([]byte(testSecret), crypto.SHA256, "X-New", WithFallbackHeaders("X-Old")),
			headers: map[string]string{"X-New": sha256hex, "X-Old": "deadbeef"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			if body == nil {
				body = payload
			}
			err := tc.v.Validate(mkReq(t, tc.headers, body), body)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("应通过, got %v", err)
				}
				return
			}
			if !errorsIs(err, tc.wantErr) {
				t.Fatalf("应 %v, got %v", tc.wantErr, err)
			}
		})
	}
}

// errorsIs 本测试文件的 errors.Is（不引标准库包装，直接走 Unwrap 链）。
func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// TestHMACValidator_MultiAlgo sha256/384/512 全族（sha1 存量兼容）各来
// 一条正确签名；sha256 签名喂给其他算法必须被拒。
func TestHMACValidator_MultiAlgo(t *testing.T) {
	payload := []byte("payload")
	cases := []struct {
		algo    crypto.Hash
		newHash func() hash.Hash
	}{
		{crypto.SHA256, sha256.New},
		{crypto.SHA384, sha512.New384},
		{crypto.SHA512, sha512.New},
		{crypto.SHA1, sha1.New},
	}
	sha256sig := hex.EncodeToString(signRaw(t, []byte(testSecret), sha256.New, payload))
	for _, tc := range cases {
		t.Run(tc.algo.String(), func(t *testing.T) {
			v := NewHMACValidator([]byte(testSecret), tc.algo, "X-Sig")
			sig := hex.EncodeToString(signRaw(t, []byte(testSecret), tc.newHash, payload))
			if err := v.Validate(mkReq(t, map[string]string{"X-Sig": sig}, payload), payload); err != nil {
				t.Fatalf("正确签名应通过, got %v", err)
			}
			if tc.algo != crypto.SHA256 {
				if err := v.Validate(mkReq(t, map[string]string{"X-Sig": sha256sig}, payload), payload); !errorsIs(err, ErrBadSignature) {
					t.Fatalf("sha256 签名喂给 %v 应被拒, got %v", tc.algo, err)
				}
			}
		})
	}
}

func TestHMACValidator_FailClosed(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"空secret", func() { _ = NewHMACValidator(nil, crypto.SHA256, "X-Sig") }},
		{"空secret切片", func() { _ = NewHMACValidator([]byte{}, crypto.SHA256, "X-Sig") }},
		{"零值算法", func() { _ = NewHMACValidator([]byte("s"), crypto.Hash(0), "X-Sig") }},
		{"白名单外算法", func() { _ = NewHMACValidator([]byte("s"), crypto.MD5, "X-Sig") }},
		{"空header", func() { _ = NewHMACValidator([]byte("s"), crypto.SHA256, "") }},
		{"nil选项", func() { _ = NewHMACValidator([]byte("s"), crypto.SHA256, "X-Sig", nil) }},
		{"空回退header名", func() {
			_ = NewHMACValidator([]byte("s"), crypto.SHA256, "X-Sig", WithFallbackHeaders(""))
		}},
		{"空前缀", func() {
			_ = NewHMACValidator([]byte("s"), crypto.SHA256, "X-Sig", WithSignaturePrefix(""))
		}},
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
