package ton

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunCallbackRetriesUntilSuccess(t *testing.T) {
	sub := &Sub{
		Context:   context.Background(),
		retryWait: time.Millisecond,
	}
	attempts := 0

	ok := sub.runCallback(func() error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary failure")
		}
		return nil
	})

	if !ok {
		t.Fatal("expected callback to succeed")
	}
	if attempts != 3 {
		t.Fatalf("unexpected callback attempts: got %d want 3", attempts)
	}
}

func TestRunCallbackStopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sub := &Sub{
		Context:   ctx,
		retryWait: time.Millisecond,
	}
	attempts := 0

	ok := sub.runCallback(func() error {
		attempts++
		cancel()
		return errors.New("temporary failure")
	})

	if ok {
		t.Fatal("expected callback retry loop to stop")
	}
	if attempts != 1 {
		t.Fatalf("unexpected callback attempts: got %d want 1", attempts)
	}
}
