package sql

import (
	"testing"
	"time"
)

func TestDefaultOptions(t *testing.T) {
	opt := defaultOptions()
	if opt.CacheSize != 1000 || opt.CacheTTLCheck != 5*time.Minute {
		t.Fatalf("unexpected defaults: size=%d ttl=%s", opt.CacheSize, opt.CacheTTLCheck)
	}
}

func TestDefaultOptions_Merge(t *testing.T) {
	cache := &memCache{}
	codec := dummyCodec{}
	mutex := NewMutex()
	opt := defaultOptions(Options{
		CacheEnabled:   true,
		Cache:          cache,
		CacheSize:      7,
		CacheTTLCheck:  2 * time.Minute,
		MaxConnections: 9,
		Codec:          codec,
		Mutex:          mutex,
	})

	if !opt.CacheEnabled {
		t.Fatal("expected cache enabled")
	}
	if opt.Cache != cache {
		t.Fatal("expected cache object merged")
	}
	if opt.CacheSize != 7 || opt.CacheTTLCheck != 2*time.Minute {
		t.Fatalf("unexpected cache opts: size=%d ttl=%s", opt.CacheSize, opt.CacheTTLCheck)
	}
	if opt.MaxConnections != 9 {
		t.Fatalf("unexpected max connections: %d", opt.MaxConnections)
	}
	if opt.Codec != codec {
		t.Fatal("expected codec merged")
	}
	if opt.Mutex != mutex {
		t.Fatal("expected mutex merged")
	}
}
