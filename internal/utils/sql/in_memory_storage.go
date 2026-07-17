package sql

import (
	"sync"
	"time"

	goroutineutil "github.com/elum2b/services/internal/utils/goroutine"
)

type l1Entry struct {
	key      string
	value    any
	expires  time.Time
	prev     *l1Entry
	next     *l1Entry
	noExpiry bool
}

var l1EntryPool = sync.Pool{
	New: func() any { return &l1Entry{} },
}

type l1Cache struct {
	mu        sync.RWMutex
	items     map[string]*l1Entry
	head      *l1Entry
	tail      *l1Entry
	maxSize   int
	curSize   int
	ttlCheck  time.Duration
	stopCh    chan struct{}
	closeOnce sync.Once
	workers   *goroutineutil.Manager
}

func newL1Cache(maxSize int, ttlCheck time.Duration) *l1Cache {
	if maxSize <= 0 {
		maxSize = 1000
	}
	if ttlCheck <= 0 {
		ttlCheck = 5 * time.Minute
	}

	c := &l1Cache{
		items:    make(map[string]*l1Entry, maxSize),
		maxSize:  maxSize,
		ttlCheck: ttlCheck,
		stopCh:   make(chan struct{}),
		workers:  goroutineutil.New(),
	}

	c.workers.Go("sql-l1-cache-cleanup", c.cleanupLoop)
	return c
}

func (c *l1Cache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.items[key]
	if !ok {
		return nil, false
	}

	if !e.noExpiry && time.Now().After(e.expires) {
		c.removeElement(e)
		return nil, false
	}

	c.moveToFront(e)
	return e.value, true
}

func (c *l1Cache) Set(key string, value any, ttl time.Duration) {
	if key == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if old, ok := c.items[key]; ok {
		old.value = value
		if ttl > 0 {
			old.expires = time.Now().Add(ttl)
			old.noExpiry = false
		} else {
			old.expires = time.Time{}
			old.noExpiry = true
		}
		c.moveToFront(old)
		return
	}

	e := l1EntryPool.Get().(*l1Entry)
	e.key = key
	e.value = value
	e.prev = nil
	e.next = nil
	if ttl > 0 {
		e.expires = time.Now().Add(ttl)
		e.noExpiry = false
	} else {
		e.expires = time.Time{}
		e.noExpiry = true
	}

	c.pushFront(e)
	c.items[key] = e
	c.curSize++

	if c.curSize > c.maxSize {
		c.evict()
	}
}

func (c *l1Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.items[key]
	if !ok {
		return
	}
	c.removeElement(e)
}

func (c *l1Cache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, e := range c.items {
		c.recycle(e)
	}
	c.items = make(map[string]*l1Entry, c.maxSize)
	c.head = nil
	c.tail = nil
	c.curSize = 0
}

func (c *l1Cache) Close() {
	c.closeOnce.Do(func() {
		close(c.stopCh)
		c.workers.Close()
	})
}

func (c *l1Cache) pushFront(e *l1Entry) {
	e.prev = nil
	e.next = c.head
	if c.head != nil {
		c.head.prev = e
	}
	c.head = e
	if c.tail == nil {
		c.tail = e
	}
}

func (c *l1Cache) moveToFront(e *l1Entry) {
	if e == c.head {
		return
	}
	c.remove(e)
	c.pushFront(e)
}

func (c *l1Cache) remove(e *l1Entry) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		c.head = e.next
	}

	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail = e.prev
	}

	e.prev = nil
	e.next = nil
}

func (c *l1Cache) removeElement(e *l1Entry) {
	c.remove(e)
	delete(c.items, e.key)
	c.curSize--
	c.recycle(e)
}

func (c *l1Cache) recycle(e *l1Entry) {
	e.key = ""
	e.value = nil
	e.expires = time.Time{}
	e.prev = nil
	e.next = nil
	e.noExpiry = false
	l1EntryPool.Put(e)
}

func (c *l1Cache) evict() {
	if c.tail == nil {
		return
	}
	c.removeElement(c.tail)
}

func (c *l1Cache) cleanupLoop() {
	ticker := time.NewTicker(c.ttlCheck)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.removeExpired()
		case <-c.stopCh:
			return
		}
	}
}

func (c *l1Cache) removeExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for _, e := range c.items {
		if !e.noExpiry && now.After(e.expires) {
			c.removeElement(e)
		}
	}
}
