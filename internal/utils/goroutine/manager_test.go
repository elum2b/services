package goroutine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
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

func TestManagerCloseStopsRestartingGoroutine(t *testing.T) {

	manager := New()
	if !manager.GoRestart(context.Background(), "test.restart", time.Millisecond, func() {
		panic("restart")
	}) {
		t.Fatal("expected restarting goroutine to start")
	}

	done := make(chan struct{})
	go func() {
		manager.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("manager Close did not stop restarting goroutine")
	}

}

func TestNilManagerDoesNotStartOrphanedGoroutine(t *testing.T) {

	var manager *Manager
	if manager.Go("test.nil", func() {}) {
		t.Fatal("nil manager must not start an unowned goroutine")
	}

}
