package sql

import (
	"testing"
)

type dummyCodec struct{}

func (dummyCodec) Marshal(v any) ([]byte, error) {
	return []byte{1}, nil
}
func (dummyCodec) Unmarshal(data []byte, v any) error {
	return nil
}

func TestNew_InitializesDBAndCache(t *testing.T) {
	db, _ := openTestDB(t)

	cache := &memCache{}
	c, err := New(db, Options{
		Cache:        cache,
		CacheEnabled: true,
		Codec:        dummyCodec{},
	})
	if err != nil {
		t.Fatalf("unexpected new error: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if c.db == nil {
		t.Fatal("expected db to be initialized")
	}
	if c.cache != cache {
		t.Fatal("expected passed L2 cache")
	}
	if !c.CacheEnabled {
		t.Fatal("expected cache enabled")
	}
	if _, ok := c.codec.(dummyCodec); !ok {
		t.Fatal("expected custom codec")
	}
	if c.mutex == nil {
		t.Fatal("expected mutex to be initialized")
	}
}

func TestNew_NilDBAndPingFailures(t *testing.T) {
	if _, err := New(nil, Options{}); err == nil {
		t.Fatal("expected nil db error")
	}

	db, _ := openTestDB(t)

	// Driver supports ping, so New should pass and assign default msgpack codec.
	c, err := New(db, Options{})
	if err != nil {
		t.Fatalf("unexpected new error: %v", err)
	}
	if _, ok := c.codec.(MsgpackCodec); !ok {
		t.Fatal("expected default msgpack codec")
	}
	if _, ok := c.mutex.(*KeyedMutex); !ok {
		t.Fatalf("expected default in-memory keyed mutex, got %T", c.mutex)
	}

	closed, _ := openTestDB(t)
	_ = closed.Close()
	if _, err := New(closed, Options{}); err == nil {
		t.Fatal("expected ping error for closed db")
	}
}

func TestNew_UsesProvidedMutex(t *testing.T) {
	db, _ := openTestDB(t)

	custom := &spyMutex{base: NewMutex()}
	c, err := New(db, Options{
		Mutex: custom,
	})
	if err != nil {
		t.Fatalf("unexpected new error: %v", err)
	}
	if c.mutex != custom {
		t.Fatal("expected provided mutex to be used")
	}
}
