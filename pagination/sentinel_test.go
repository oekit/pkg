package pagination

// 分页哨兵的错误分类契约（#565）。

import (
	"errors"
	"testing"

	pkgerrors "github.com/oekit/pkg/errors"
)

// TestSentinels_ArePlainErrors 钉住 #565：五个分页哨兵只承载 errors.Is
// 身份，**不是** BizError——pagination 是底层库，不替 delivery 层决定
// HTTP 映射。此前 NewBizError(1, ...) 被 middleware 统一落成 400，
// 「上游契约损坏/调用方编程错误/预算耗尽」共享同一 wire code，监控失真。
func TestSentinels_ArePlainErrors(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrBadPageSize", ErrBadPageSize},
		{"ErrNilFetcher", ErrNilFetcher},
		{"ErrTooManyPages", ErrTooManyPages},
		{"ErrBudgetExceeded", ErrBudgetExceeded},
		{"ErrNilYielder", ErrNilYielder},
	}
	for _, s := range sentinels {
		if _, ok := pkgerrors.AsBizError(s.err); ok {
			t.Errorf("%s 不应是 BizError（底层库不决定 HTTP 映射）", s.name)
		}
		if !errors.Is(s.err, s.err) {
			t.Errorf("%s 应可通过 errors.Is 自匹配", s.name)
		}
	}
}
