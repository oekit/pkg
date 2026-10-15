package errors

import (
	"fmt"
	"testing"
)

func TestChain(t *testing.T) {
	t.Run("all nil returns nil", func(t *testing.T) {
		if err := Chain(
			func() error { return nil },
			func() error { return nil },
		); err != nil {
			t.Errorf("Chain() = %v, want nil", err)
		}
	})

	t.Run("empty returns nil", func(t *testing.T) {
		if err := Chain(); err != nil {
			t.Errorf("Chain() = %v, want nil", err)
		}
	})

	t.Run("stops at first error", func(t *testing.T) {
		first := fmt.Errorf("first")
		var ran []string
		err := Chain(
			func() error { ran = append(ran, "a"); return nil },
			func() error { ran = append(ran, "b"); return first },
			func() error { ran = append(ran, "c"); return nil },
			func() error { ran = append(ran, "d"); return fmt.Errorf("never") },
		)
		if err != first {
			t.Errorf("Chain() = %v, want %v", err, first)
		}
		if len(ran) != 2 || ran[0] != "a" || ran[1] != "b" {
			t.Errorf("executed steps = %v, want [a b]——首错之后必须停止", ran)
		}
	})
}

func TestIn(t *testing.T) {
	var (
		errA = fmt.Errorf("a")
		errB = fmt.Errorf("b")
	)

	tests := []struct {
		name    string
		err     error
		targets []error
		want    bool
	}{
		{"direct match", errA, []error{errA, errB}, true},
		{"no match", fmt.Errorf("other"), []error{errA, errB}, false},
		{"wrapped match", fmt.Errorf("ctx: %w", errA), []error{errB, errA}, true},
		{"deeply wrapped no match", fmt.Errorf("l1: %w", fmt.Errorf("l2: %w", fmt.Errorf("x"))), []error{errA}, false},
		{"nil err", nil, []error{errA}, false},
		{"no targets", errA, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := In(tt.err, tt.targets...); got != tt.want {
				t.Errorf("In(%v, %v) = %v, want %v", tt.err, tt.targets, got, tt.want)
			}
		})
	}

	t.Run("works with sentinel via Is", func(t *testing.T) {
		// 与本包 BizError 等实现 Is 的错误类型协作
		wrapped := Wrap(errA, "op", "key", "val")
		if !In(wrapped, errB, errA) {
			t.Error("In should match wrapped errors via errors.Is")
		}
	})
}
