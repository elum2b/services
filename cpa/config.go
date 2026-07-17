package cpa

import (
	"time"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

const defaultCacheDelay = 10 * time.Minute

type Storage interface {
	GetWithTTL(key string) (val []byte, ttl time.Duration, err error)
	Set(key string, val []byte, exp time.Duration) error
	Delete(key string) error
	Reset() error
	Close() error
}

type Mutex interface {
	Lock(key string) error
	Unlock(key string) error
}

type Codec interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
}

type CacheInvalidationErrorHandler func(error)

type Options struct {
	MaxConnections           int
	QueryTimeout             time.Duration
	CacheL1Delay             time.Duration
	CacheL2Delay             time.Duration
	Cache                    Storage
	CacheEnabled             bool
	CacheSize                int
	CacheTTLCheck            time.Duration
	Codec                    Codec
	Mutex                    Mutex
	OnCacheInvalidationError CacheInvalidationErrorHandler
}

type DatabaseParams struct {
	User     string
	Password string
	Database string
	Host     string
	Port     int
	Options  Options
}

func normalizeOptions(options Options) Options {
	if !options.CacheEnabled {
		return options
	}
	if options.CacheL1Delay <= 0 {
		options.CacheL1Delay = defaultCacheDelay
	}
	if options.CacheL2Delay <= 0 {
		options.CacheL2Delay = defaultCacheDelay
	}
	return options
}

func toSQLWrapOptions(options Options) sqlwrap.Options {
	options = normalizeOptions(options)
	result := sqlwrap.Options{
		MaxConnections: options.MaxConnections,
		QueryTimeout:   options.QueryTimeout,
		CacheEnabled:   options.CacheEnabled,
		CacheSize:      options.CacheSize,
		CacheTTLCheck:  options.CacheTTLCheck,
	}
	if options.Cache != nil {
		result.Cache = storageAdapter{value: options.Cache}
	}
	if options.Codec != nil {
		result.Codec = codecAdapter{value: options.Codec}
	}
	if options.Mutex != nil {
		result.Mutex = mutexAdapter{value: options.Mutex}
	}
	return result
}

type storageAdapter struct{ value Storage }

func (a storageAdapter) GetWithTTL(key string) ([]byte, time.Duration, error) {
	return a.value.GetWithTTL(key)
}
func (a storageAdapter) Set(key string, value []byte, expiration time.Duration) error {
	return a.value.Set(key, value, expiration)
}
func (a storageAdapter) Delete(key string) error {
	return a.value.Delete(key)
}

func (a storageAdapter) Reset() error {
	return a.value.Reset()
}

func (a storageAdapter) Close() error {
	return a.value.Close()
}

type mutexAdapter struct{ value Mutex }

func (a mutexAdapter) Lock(key string) error {
	return a.value.Lock(key)
}

func (a mutexAdapter) Unlock(key string) error {
	return a.value.Unlock(key)
}

type codecAdapter struct{ value Codec }

func (a codecAdapter) Marshal(value any) ([]byte, error) {
	return a.value.Marshal(value)
}
func (a codecAdapter) Unmarshal(data []byte, value any) error {
	return a.value.Unmarshal(data, value)
}
