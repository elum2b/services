package goroutine

import (
	"sync/atomic"
	"testing"
)

func TestManagerGoRecoversPanic(t *testing.T) {
	manager := New()
	var ran atomic.Bool

	if !manager.Go("test.panic", func() {
		ran.Store(true)
		panic("boom")
	}) {
		t.Fatal("expected goroutine to start")
	}
	manager.Close()

	if !ran.Load() {
		t.Fatal("goroutine did not run")
	}
	if manager.Go("test.closed", func() {}) {
		t.Fatal("closed manager must not start goroutines")
	}
}
