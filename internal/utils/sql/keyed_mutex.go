package sql

import (
	"errors"
	"sync"
)

type entry struct {
	m    sync.Mutex
	refs int32
}

// KeyedMutex provides per-key lock with pooled entries.
type KeyedMutex struct {
	mu   sync.Mutex
	m    map[string]*entry
	pool sync.Pool
}

// NewMutex creates a new keyed mutex.
func NewMutex() *KeyedMutex {
	return &KeyedMutex{
		m: make(map[string]*entry),
		pool: sync.Pool{
			New: func() any {
				return &entry{}
			},
		},
	}
}

func (k *KeyedMutex) Lock(key string) error {
	k.mu.Lock()
	e, exists := k.m[key]
	if !exists {
		e = k.pool.Get().(*entry)
		e.refs = 1
		k.m[key] = e
	} else {
		e.refs++
	}
	k.mu.Unlock()

	e.m.Lock()
	return nil
}

func (k *KeyedMutex) Unlock(key string) error {
	k.mu.Lock()
	e, exists := k.m[key]
	if !exists {
		k.mu.Unlock()
		return errors.New("keyedmutex: unlock of unlocked key")
	}

	e.m.Unlock()
	e.refs--
	if e.refs <= 0 {
		delete(k.m, key)
		e.refs = 0
		k.pool.Put(e)
	}
	k.mu.Unlock()
	return nil
}
