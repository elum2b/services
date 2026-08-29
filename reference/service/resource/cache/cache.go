// Package cache keeps bounded immutable media bytes in process memory.
package cache

import (
	"container/list"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxItems = 512
	defaultMaxBytes = 64 << 20
	defaultTTL      = time.Hour
)

type Config struct {
	MaxItems int
	MaxBytes int64
	TTL      time.Duration
}

type Value struct {
	Data        []byte
	ContentType string
}

type entry struct {
	key       string
	value     Value
	expiresAt time.Time
}

type call struct {
	done  chan struct{}
	value Value
	err   error
}

type Cache struct {
	mu       sync.Mutex
	entries  map[string]*list.Element
	lru      *list.List
	inflight map[string]*call
	bytes    int64
	config   Config
}

func New(config Config) *Cache {
	if config.MaxItems <= 0 {
		config.MaxItems = defaultMaxItems
	}

	if config.MaxBytes <= 0 {
		config.MaxBytes = defaultMaxBytes
	}

	if config.TTL <= 0 {
		config.TTL = defaultTTL
	}

	return &Cache{
		entries:  make(map[string]*list.Element),
		lru:      list.New(),
		inflight: make(map[string]*call),
		config:   config,
	}
}

func (c *Cache) Get(key string) (Value, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.getLocked(key, time.Now())
}

func (c *Cache) Set(key string, value Value) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.setLocked(key, value, time.Now())
}

func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if element := c.entries[key]; element != nil {
		c.removeLocked(element)
	}
}

func (c *Cache) DeletePrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, element := range c.entries {
		if strings.HasPrefix(key, prefix) {
			c.removeLocked(element)
		}
	}
}

// GetOrLoad runs load once for concurrent misses of the same key.
func (c *Cache) GetOrLoad(
	key string,
	load func() (Value, error),
) (Value, error) {
	if value, ok := c.Get(key); ok {
		return value, nil
	}

	c.mu.Lock()

	if value, ok := c.getLocked(key, time.Now()); ok {
		c.mu.Unlock()

		return value, nil
	}

	if pending := c.inflight[key]; pending != nil {
		c.mu.Unlock()
		<-pending.done

		return clone(pending.value), pending.err
	}

	pending := &call{done: make(chan struct{})}

	c.inflight[key] = pending
	c.mu.Unlock()

	value, err := load()

	c.mu.Lock()

	if err == nil {
		c.setLocked(key, value, time.Now())
	}

	pending.value, pending.err = clone(value), err

	delete(c.inflight, key)
	close(pending.done)
	c.mu.Unlock()

	return clone(value), err
}

func (c *Cache) getLocked(key string, now time.Time) (Value, bool) {
	element := c.entries[key]
	if element == nil {
		return Value{}, false
	}

	item := element.Value.(*entry)
	if !now.Before(item.expiresAt) {
		c.removeLocked(element)

		return Value{}, false
	}

	c.lru.MoveToFront(element)

	return clone(item.value), true
}

func (c *Cache) setLocked(key string, value Value, now time.Time) {
	value = clone(value)
	if int64(len(value.Data)) > c.config.MaxBytes {
		return
	}

	if element := c.entries[key]; element != nil {
		c.removeLocked(element)
	}

	item := &entry{key: key, value: value, expiresAt: now.Add(c.config.TTL)}

	c.entries[key] = c.lru.PushFront(item)

	c.bytes += int64(len(value.Data))

	for c.lru.Len() > c.config.MaxItems || c.bytes > c.config.MaxBytes {
		c.removeLocked(c.lru.Back())
	}
}

func (c *Cache) removeLocked(element *list.Element) {
	item := element.Value.(*entry)
	delete(c.entries, item.key)
	c.lru.Remove(element)

	c.bytes -= int64(len(item.value.Data))
}

func clone(value Value) Value {
	value.Data = append([]byte(nil), value.Data...)
	return value
}
