package sql

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestKeyedMutex_LockUnlock(t *testing.T) {
	m := NewMutex()
	if err := m.Lock("k"); err != nil {
		t.Fatalf("unexpected lock error: %v", err)
	}
	if err := m.Unlock("k"); err != nil {
		t.Fatalf("unexpected unlock error: %v", err)
	}
}

func TestKeyedMutex_SerializesSameKey(t *testing.T) {
	m := NewMutex()
	var active atomic.Int32
	var violated atomic.Bool
	var wg sync.WaitGroup

	run := func() {
		defer wg.Done()
		if err := m.Lock("same"); err != nil {
			t.Errorf("lock error: %v", err)
			return
		}
		if active.Add(1) > 1 {
			violated.Store(true)
		}
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
		if err := m.Unlock("same"); err != nil {
			t.Errorf("unlock error: %v", err)
		}
	}

	wg.Add(2)
	go run()
	go run()
	wg.Wait()

	if violated.Load() {
		t.Fatal("same key lock must be serialized")
	}
}

func TestKeyedMutex_UnlockUnknownKey(t *testing.T) {
	m := NewMutex()
	if err := m.Unlock("unknown"); err == nil {
		t.Fatal("expected unlock error")
	}
}
