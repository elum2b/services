package sql

import (
	"time"
)

// Storage defines external key-value storage used as L2 cache.
type Storage interface {
	GetWithTTL(key string) (val []byte, ttl time.Duration, err error)
	Set(key string, val []byte, exp time.Duration) error
	Delete(key string) error
	Reset() error
	Close() error
}

// Mutex provides key-based mutual exclusion.
type Mutex interface {
	Lock(key string) error
	Unlock(key string) error
}

// Options configures DB connection and cache/codec behavior.
type Options struct {
	MaxConnections int
	QueryTimeout   time.Duration

	Cache        Storage
	CacheEnabled bool

	CacheSize     int
	CacheTTLCheck time.Duration

	Codec Codec
	Mutex Mutex
}

func defaultOptions(opts ...Options) Options {
	options := Options{
		CacheEnabled:  false,
		CacheSize:     1000,
		CacheTTLCheck: 5 * time.Minute,
		QueryTimeout:  defaultQueryTimeout,
	}

	if len(opts) > 0 {
		userOpts := opts[0]

		if userOpts.MaxConnections > 0 {
			options.MaxConnections = userOpts.MaxConnections
		}
		if userOpts.CacheSize > 0 {
			options.CacheSize = userOpts.CacheSize
		}
		if userOpts.CacheTTLCheck > 0 {
			options.CacheTTLCheck = userOpts.CacheTTLCheck
		}
		if userOpts.QueryTimeout > 0 {
			options.QueryTimeout = userOpts.QueryTimeout
		}

		options.Cache = userOpts.Cache
		options.CacheEnabled = userOpts.CacheEnabled
		options.Codec = userOpts.Codec
		options.Mutex = userOpts.Mutex
	}

	return options
}
