package cache

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCacheBoundsAndLRU(t *testing.T) {
	c := New(Config{MaxItems: 2, MaxBytes: 5})
	c.Set("a", Value{Data: []byte("12")})
	c.Set("b", Value{Data: []byte("34")})

	if _, ok := c.Get("a"); !ok {
		t.Fatal("a missing")
	}

	c.Set("c", Value{Data: []byte("5")})

	if _, ok := c.Get("b"); ok {
		t.Fatal("least recently used entry retained")
	}

	if _, ok := c.Get("a"); !ok {
		t.Fatal("recent entry evicted")
	}

	if _, ok := c.Get("c"); !ok {
		t.Fatal("new entry missing")
	}
}

func TestCacheExpiryAndCopies(t *testing.T) {
	c := New(Config{TTL: time.Millisecond})
	input := []byte("data")
	c.Set("key", Value{Data: input})

	input[0] = 'x'

	got, ok := c.Get("key")

	if !ok || string(got.Data) != "data" {
		t.Fatalf("got %+v", got)
	}

	got.Data[0] = 'y'
	got, ok = c.Get("key")

	if !ok || string(got.Data) != "data" {
		t.Fatalf("cache was mutated: %+v", got)
	}

	time.Sleep(2 * time.Millisecond)

	if _, ok := c.Get("key"); ok {
		t.Fatal("expired entry retained")
	}
}

func TestCacheGetOrLoadCoalescesMisses(t *testing.T) {
	c := New(Config{})

	var (
		calls atomic.Int32
		group sync.WaitGroup
	)

	for range 20 {
		group.Add(1)

		go func() {
			defer group.Done()

			got, err := c.GetOrLoad("key", func() (Value, error) {
				calls.Add(1)
				time.Sleep(time.Millisecond)

				return Value{Data: []byte("media")}, nil
			})
			if err != nil || string(got.Data) != "media" {
				t.Errorf("got=%q err=%v", got.Data, err)
			}
		}()
	}

	group.Wait()

	if calls.Load() != 1 {
		t.Fatalf("loads = %d", calls.Load())
	}
}
