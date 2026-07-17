package sql

import (
	"testing"
	"time"
)

func TestL1Cache_SetGetDeleteResetAndTTL(t *testing.T) {
	c := newL1Cache(1, time.Millisecond)
	t.Cleanup(c.Close)
	c.Set("a", 10, time.Minute)
	if v, ok := c.Get("a"); !ok || v.(int) != 10 {
		t.Fatalf("expected cache hit 10, got %v %v", v, ok)
	}

	c.Set("b", 20, time.Minute)
	if _, ok := c.Get("a"); ok {
		t.Fatal("expected one key evicted due to max size")
	}

	c.Delete("b")
	if _, ok := c.Get("b"); ok {
		t.Fatal("expected deleted key miss")
	}

	c.Set("ttl", 1, 5*time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	if _, ok := c.Get("ttl"); ok {
		t.Fatal("expected ttl key expired")
	}

	c.Set("x", 1, 0)
	c.Reset()
	if _, ok := c.Get("x"); ok {
		t.Fatal("expected reset to clear cache")
	}

	c = newL1Cache(0, time.Millisecond)
	t.Cleanup(c.Close)
	if c.maxSize != 1000 {
		t.Fatalf("expected default max size 1000, got %d", c.maxSize)
	}
	c.Set("", 1, time.Minute)
	if len(c.items) != 0 {
		t.Fatal("expected empty key not to be inserted")
	}
}

func TestL1Cache_InternalPaths(t *testing.T) {
	c := newL1Cache(2, time.Millisecond)
	t.Cleanup(c.Close)

	// update existing key path
	c.Set("a", 1, time.Minute)
	c.Set("a", 2, 0)
	if v, ok := c.Get("a"); !ok || v.(int) != 2 {
		t.Fatalf("expected updated value=2, got %v ok=%v", v, ok)
	}

	// moveToFront path for non-head
	c.Set("b", 3, time.Minute)
	if _, ok := c.Get("a"); !ok {
		t.Fatal("expected get(a) to move entry to front")
	}

	// delete miss path
	c.Delete("missing")

	// evict with nil tail path
	empty := newL1Cache(1, time.Millisecond)
	t.Cleanup(empty.Close)
	empty.evict()

	// no-expiry path should survive quick wait
	c.Set("persist", 5, 0)
	time.Sleep(2 * time.Millisecond)
	if v, ok := c.Get("persist"); !ok || v.(int) != 5 {
		t.Fatalf("expected persist value, got %v ok=%v", v, ok)
	}
}

func TestL1Cache_AutoCleanupTTL(t *testing.T) {
	c := newL1Cache(10, time.Millisecond)
	t.Cleanup(c.Close)

	c.Set("ephemeral", 1, 2*time.Millisecond)
	time.Sleep(15 * time.Millisecond)

	c.mu.RLock()
	_, ok := c.items["ephemeral"]
	c.mu.RUnlock()
	if ok {
		t.Fatal("expected background cleanup to remove expired key")
	}
}
