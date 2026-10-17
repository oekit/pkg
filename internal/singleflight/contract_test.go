package singleflight

import (
	"testing"

	"github.com/oekit/pkg/singleflighttest"
)

// TestContract 跑共享语义契约（与 concurrency/singleflight 同一套）。
func TestContract(t *testing.T) {
	singleflighttest.Run[int](t, func() singleflighttest.Flight[int] {
		return New[int]()
	})
}
