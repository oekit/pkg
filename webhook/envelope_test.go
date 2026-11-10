package webhook

// 事件信封测试（#328）：线格式字段齐备、payload 字节原样嵌入、与签名器
// 正交组合（签的是信封字节）、构造期 fail-closed。

import (
	"crypto"
	"encoding/json"
	"testing"
)

func TestEventEnvelope_WireFormat(t *testing.T) {
	payload := []byte(`{"run_id":7,"ok":true}`)
	id, _ := NewEventID("git-ferry", "sync_run")
	env := NewEventEnvelope("sync_run", id, payload)
	env.Timestamp = fixedClock.Unix() // 固定时间戳做确定性断言

	raw, err := env.Bytes()
	if err != nil {
		t.Fatalf("Bytes 不应出错, got %v", err)
	}
	if !json.Valid(raw) {
		t.Fatalf("信封字节应是合法 JSON, got %s", raw)
	}

	var dec struct {
		Event     string          `json:"event"`
		ID        string          `json:"id"`
		Timestamp int64           `json:"timestamp"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &dec); err != nil {
		t.Fatalf("解码不应出错, got %v", err)
	}
	if dec.Event != "sync_run" || dec.ID != id || dec.Timestamp != fixedClock.Unix() {
		t.Fatalf("信封头字段不符: event=%s id=%s ts=%d", dec.Event, dec.ID, dec.Timestamp)
	}
	// payload 字节原样嵌入（不重排序、不重编码）
	if string(dec.Data) != string(payload) {
		t.Fatalf("Data 应字节原样嵌入 %s, got %s", payload, dec.Data)
	}
}

// TestEventEnvelope_WithSigner 正交组合：信封字节进签名器，收端验签通过
// 后按信封路由/去重——#328 完整发出链路样例。
func TestEventEnvelope_WithSigner(t *testing.T) {
	payload := []byte(`{"b":2,"a":1}`)
	id, _ := NewEventID("prod", "push")
	env := NewEventEnvelope("push", id, payload)
	body, err := env.Bytes()
	if err != nil {
		t.Fatalf("Bytes 不应出错, got %v", err)
	}

	signer := NewHMACSigner([]byte(testSecret), crypto.SHA256, "X-Sig")
	hdr, err := signer.Sign(body)
	if err != nil {
		t.Fatalf("Sign 不应出错, got %v", err)
	}

	// 收端：验签 → 解信封 → 路由/去重
	v := NewHMACValidator([]byte(testSecret), crypto.SHA256, "X-Sig")
	req := mkReq(t, map[string]string{"X-Sig": hdr.Get("X-Sig")}, body)
	if err := v.Validate(req, body); err != nil {
		t.Fatalf("信封字节验签应通过, got %v", err)
	}
	var back EventEnvelope
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("信封解码不应出错, got %v", err)
	}
	if back.Event != "push" || back.ID != id {
		t.Fatalf("信封回读不符: %+v", back)
	}
}

func TestEventEnvelope_FailClosed(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"空事件", func() { _ = NewEventEnvelope("", "id", []byte(`{}`)) }},
		{"空ID", func() { _ = NewEventEnvelope("push", "", []byte(`{}`)) }},
		{"nilpayload", func() { _ = NewEventEnvelope("push", "id", nil) }},
		{"空payload", func() { _ = NewEventEnvelope("push", "id", []byte{}) }},
		{"非法JSON", func() { _ = NewEventEnvelope("push", "id", []byte(`{"n":`)) }},
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
