package sql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

type orderedCache struct {
	data  map[string][]byte
	order []string
}

func (m *orderedCache) GetWithTTL(key string) ([]byte, time.Duration, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, 0, errors.New("miss")
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, 0, nil
}

func (m *orderedCache) Set(key string, val []byte, _ time.Duration) error {
	if m.data == nil {
		m.data = make(map[string][]byte)
	}
	cp := make([]byte, len(val))
	copy(cp, val)
	m.data[key] = cp
	if strings.HasPrefix(key, "cache_version:") {
		m.order = append(m.order, "version")
	} else {
		m.order = append(m.order, "data")
	}
	return nil
}

func (m *orderedCache) Delete(key string) error {
	delete(m.data, key)
	return nil
}

func (m *orderedCache) Reset() error {
	m.data = make(map[string][]byte)
	m.order = nil
	return nil
}

func (m *orderedCache) Close() error {
	return nil
}

func TestVersionedCacheKey_UsesSharedL2Version(t *testing.T) {
	cache := &memCache{}
	c1 := &Client{cache: cache}
	c2 := &Client{cache: cache}

	scope := []any{"reference", "resolve", "workspace-a"}
	before := c1.VersionedCacheKey(scope, "ru", "a\x1fb")
	if before != c2.VersionedCacheKey(scope, "ru", "a\x1fb") {
		t.Fatal("expected clients to build the same key before bump")
	}

	if err := c2.BumpCacheVersion(scope...); err != nil {
		t.Fatalf("unexpected bump error: %v", err)
	}

	after := c1.VersionedCacheKey(scope, "ru", "a\x1fb")
	if after == before {
		t.Fatal("expected key to change after another client bumps L2 version")
	}
}

func TestVersionedCacheKey_IsScopedPerMethod(t *testing.T) {
	c := &Client{}
	resolveScope := []any{"reference", "resolve", "workspace-a"}
	getScope := []any{"reference", "get", "workspace-a"}

	resolveBefore := c.VersionedCacheKey(resolveScope, "ru", "a\x1fb")
	getBefore := c.VersionedCacheKey(getScope, "item-a", "ru")

	if err := c.BumpCacheVersion(resolveScope...); err != nil {
		t.Fatalf("unexpected bump error: %v", err)
	}

	resolveAfter := c.VersionedCacheKey(resolveScope, "ru", "a\x1fb")
	getAfter := c.VersionedCacheKey(getScope, "item-a", "ru")
	if resolveAfter == resolveBefore {
		t.Fatal("expected bumped method key to change")
	}
	if getAfter != getBefore {
		t.Fatal("expected other method key to stay unchanged")
	}
}

func TestQuery_WithMissingVersionPublishesDataBeforeVersion(t *testing.T) {
	cache := &orderedCache{}
	c := &Client{
		db:           &sql.DB{},
		cache:        cache,
		inMemory:     newL1Cache(100, time.Minute),
		codec:        MsgpackCodec{},
		CacheEnabled: true,
	}

	_, err := Query(context.Background(), c, Params{
		Key:               "payload",
		CacheL2Delay:      time.Minute,
		CacheVersionScope: []any{"reference", "resolve", "workspace-a"},
	}, func(context.Context) (testModel, error) {
		return testModel{ID: 10}, nil
	})
	if err != nil {
		t.Fatalf("unexpected query error: %v", err)
	}
	if got := strings.Join(cache.order, ","); got != "data,version" {
		t.Fatalf("expected data to be stored before version, got %q", got)
	}
}
