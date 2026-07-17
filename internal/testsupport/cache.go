package testsupport

import (
	"sync"
	"time"
)

type cacheEntry struct {
	value     []byte
	expiresAt time.Time
}

type Cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
}

func NewCache() *Cache {
	return &Cache{entries: make(map[string]cacheEntry)}
}

func (c *Cache) GetWithTTL(key string) ([]byte, time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.entries[key]
	if !exists {
		return nil, 0, nil
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return nil, 0, nil
	}

	ttl := time.Duration(0)
	if !entry.expiresAt.IsZero() {
		ttl = time.Until(entry.expiresAt)
	}

	return append([]byte(nil), entry.value...), ttl, nil
}

func (c *Cache) Set(key string, value []byte, expiration time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := cacheEntry{value: append([]byte(nil), value...)}
	if expiration > 0 {
		entry.expiresAt = time.Now().Add(expiration)
	}
	c.entries[key] = entry

	return nil
}

func (c *Cache) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)

	return nil
}

func (c *Cache) Reset() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	clear(c.entries)

	return nil
}

func (c *Cache) Close() error {
	return nil
}
