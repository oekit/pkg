package webhook

// Fallback 组合子与 StaticTokenValidator 测试（#270）：让位/判死语义、
// 多模消歧优先序、常量时间令牌比对、构造期 fail-closed。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// countingValidator 记录被询问次数的验证器（组合子语义断言用）。
type countingValidator struct {
	calls int
	err   error
}

func (c *countingValidator) Validate(_ *http.Request, _ []byte) error {
	c.calls++
	return c.err
}

func TestFallback_LetsMissingFallThrough(t *testing.T) {
	first := &countingValidator{err: ErrMissingSignature}
	second := &countingValidator{}
	v := Fallback(first, second)

	err := v.Validate(httptest.NewRequest(http.MethodPost, "/", nil), []byte("body"))
	if err != nil {
		t.Fatalf("第二候选应通过, got %v", err)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("候选应按序各被询问一次, first=%d second=%d", first.calls, second.calls)
	}
}

func TestFallback_HardFailNoDowngrade(t *testing.T) {
	// 错签硬失败：后续候选不得被询问（交叉认证防线）
	first := &countingValidator{err: ErrBadSignature}
	second := &countingValidator{}
	v := Fallback(first, second)

	err := v.Validate(httptest.NewRequest(http.MethodPost, "/", nil), nil)
	if !errors.Is(err, ErrBadSignature) {
		t.Fatalf("应 ErrBadSignature, got %v", err)
	}
	if second.calls != 0 {
		t.Fatalf("错签后不得降级到后续候选, second.calls=%d", second.calls)
	}

	// 自定义错误同样整体失败（组合子不吞未知错误形态）
	custom := &countingValidator{err: errors.New("自定义失败")}
	v2 := Fallback(custom, &countingValidator{})
	if err := v2.Validate(httptest.NewRequest(http.MethodPost, "/", nil), nil); err == nil || err.Error() != "自定义失败" {
		t.Fatalf("自定义错误应原样上抛, got %v", err)
	}
}

func TestFallback_AllMissing(t *testing.T) {
	a := &countingValidator{err: ErrMissingSignature}
	b := &countingValidator{err: ErrMissingSignature}
	v := Fallback(a, b)
	err := v.Validate(httptest.NewRequest(http.MethodPost, "/", nil), nil)
	if !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("全部让位应 ErrMissingSignature, got %v", err)
	}
	if a.calls != 1 || b.calls != 1 {
		t.Fatalf("让位链应遍历完, a=%d b=%d", a.calls, b.calls)
	}
}

func TestFallback_FailFast(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"空候选列表", func() { _ = Fallback() }},
		{"nil候选", func() { _ = Fallback(&countingValidator{}, nil) }},
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

func TestStaticTokenValidator(t *testing.T) {
	v := NewStaticTokenValidator("X-Token", []byte("hunter2"))

	if err := v.Validate(mkReq(t, map[string]string{"X-Token": "hunter2"}, nil), nil); err != nil {
		t.Fatalf("正确令牌应通过, got %v", err)
	}
	if err := v.Validate(mkReq(t, map[string]string{"X-Token": "hunter3"}, nil), nil); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("错令牌应 ErrBadSignature, got %v", err)
	}
	if err := v.Validate(mkReq(t, map[string]string{}, nil), nil); !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("header 缺席应 ErrMissingSignature, got %v", err)
	}
	// 前缀相同的更长令牌不得通过（常量时间比较按完整值，非前缀匹配）
	if err := v.Validate(mkReq(t, map[string]string{"X-Token": "hunter2xxx"}, nil), nil); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("前缀+尾部不得通过, got %v", err)
	}
}

func TestStaticToken_FailClosed(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"空令牌", func() { _ = NewStaticTokenValidator("X-Token", nil) }},
		{"空令牌切片", func() { _ = NewStaticTokenValidator("X-Token", []byte{}) }},
		{"空header", func() { _ = NewStaticTokenValidator("", []byte("t")) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("构造期非法值应 panic fail-closed（仓规总则①）")
				}
			}()
			tc.fn()
		})
	}
}
