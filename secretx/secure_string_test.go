package secretx

import (
	"strings"
	"sync"
	"testing"
)

func TestSecureString_RoundTrip(t *testing.T) {
	s := NewSecureString("s3cret-密码-🔐")
	if got := s.Get(); got != "s3cret-密码-🔐" {
		t.Fatalf("Get = %q, want roundtrip", got)
	}
}

func TestSecureString_SetReplaces(t *testing.T) {
	s := NewSecureString("first")
	s.Set("second")
	if got := s.Get(); got != "second" {
		t.Fatalf("Get = %q, want %q", got, "second")
	}
	s.Set("")
	if got := s.Get(); got != "" {
		t.Fatalf("Set(\"\") 后 Get = %q, want 空", got)
	}
}

func TestSecureString_Clear(t *testing.T) {
	s := NewSecureString("payload")
	s.Clear()
	if got := s.Get(); got != "" {
		t.Fatalf("Clear 后 Get = %q, want 空", got)
	}
	// Clear 后可重新 Set 复用
	s.Set("again")
	if got := s.Get(); got != "again" {
		t.Fatalf("复用 Set 后 Get = %q", got)
	}
}

func TestSecureString_EqualConstantTime(t *testing.T) {
	s := NewSecureString("token")
	if !s.EqualConstantTime("token") {
		t.Fatal("同值比较应命中")
	}
	if s.EqualConstantTime("tokenx") {
		t.Fatal("不同值不应命中")
	}
	empty := NewSecureString("")
	if !empty.EqualConstantTime("") {
		t.Fatal("空值对空串应命中")
	}
	if empty.EqualConstantTime("x") {
		t.Fatal("空值对非空不应命中")
	}
}

// 混淆存储的基本有效性：底层不应出现连续明文（XOR 随机密钥逐字节混淆，
// 明文概率性自混淆仅当 key 字节与明文字节逐位相等，长串下可忽略）。
func TestSecureString_NotPlaintextInStorage(t *testing.T) {
	s := NewSecureString(strings.Repeat("A", 64))
	if len(s.obfuscated) != 64 || len(s.key) != 64 {
		t.Fatalf("存储长度异常: %d/%d", len(s.obfuscated), len(s.key))
	}
	same := true
	for i := range s.obfuscated {
		if s.obfuscated[i] != 'A' {
			same = false
			break
		}
	}
	if same {
		t.Fatal("混淆层出现连续明文")
	}
}

func TestSecureBytes_CopyIsolation(t *testing.T) {
	orig := []byte{1, 2, 3}
	s := NewSecureBytes(orig)
	orig[0] = 9 // 外部改写不得穿透
	if got := s.Get(); got[0] != 1 {
		t.Fatalf("入参改写穿透: %v", got)
	}
	got := s.Get()
	got[0] = 7 // 返回副本改写不得穿透
	if again := s.Get(); again[0] != 1 {
		t.Fatalf("Get 副本被共享: %v", again)
	}
}

func TestSecureBytes_ClearAndNil(t *testing.T) {
	s := NewSecureBytes([]byte{1})
	s.Clear()
	if got := s.Get(); got != nil {
		t.Fatalf("Clear 后 Get = %v, want nil", got)
	}
	s.Set(nil)
	if got := s.Get(); got != nil {
		t.Fatalf("Set(nil) 后 Get = %v, want nil", got)
	}
}

// 持有器是并发读写面（Set/Clear/Get 并发配置热更新 + 请求读）——race 下不炸。
func TestSecureString_Concurrent(t *testing.T) {
	s := NewSecureString("v0")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				s.Set(strings.Repeat("x", i+1))
				_ = s.Get()
				_ = s.EqualConstantTime("x")
			}
		}(i)
	}
	wg.Wait()
}
