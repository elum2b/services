package tasks

import (
	"time"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	taskruntime "github.com/elum2b/services/tasks/runtime"
	"github.com/elum2b/services/tasks/service/integration"
	"github.com/elum2b/services/tasks/service/user"
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

type Options struct {
	MaxConnections            int
	QueryTimeout              time.Duration
	CacheL1Delay              time.Duration
	CacheL2Delay              time.Duration
	Cache                     Storage
	CacheEnabled              bool
	CacheSize                 int
	CacheTTLCheck             time.Duration
	Codec                     Codec
	Mutex                     Mutex
	OnCacheInvalidationError  func(error)
	Integration               integration.Options
	Runtime                   taskruntime.Options
	PartnerProviders          map[string]user.PartnerProvider
	PartnerStartLeaseDuration time.Duration
}

type DatabaseParams struct {
	User     string
	Password string
	Database string
	Host     string
	Port     int
	Options  Options
}

func toSQLWrapOptions(value Options) sqlwrap.Options {
	result := sqlwrap.Options{
		MaxConnections: value.MaxConnections,
		CacheEnabled:   value.CacheEnabled,
		CacheSize:      value.CacheSize,
		CacheTTLCheck:  value.CacheTTLCheck,
		QueryTimeout:   value.QueryTimeout,
	}

	if value.Cache != nil {
		result.Cache = storageAdapter{value: value.Cache}
	}

	if value.Codec != nil {
		result.Codec = codecAdapter{value: value.Codec}
	}

	if value.Mutex != nil {
		result.Mutex = mutexAdapter{value: value.Mutex}
	}

	return result
}

type storageAdapter struct{ value Storage }

func (a storageAdapter) GetWithTTL(key string) ([]byte, time.Duration, error) {
	return a.value.GetWithTTL(key)
}
func (a storageAdapter) Set(key string, val []byte, exp time.Duration) error {
	return a.value.Set(key, val, exp)
}
func (a storageAdapter) Delete(key string) error { return a.value.Delete(key) }
func (a storageAdapter) Reset() error            { return a.value.Reset() }
func (a storageAdapter) Close() error            { return a.value.Close() }

type codecAdapter struct{ value Codec }

func (a codecAdapter) Marshal(v any) ([]byte, error)      { return a.value.Marshal(v) }
func (a codecAdapter) Unmarshal(data []byte, v any) error { return a.value.Unmarshal(data, v) }

type mutexAdapter struct{ value Mutex }

func (a mutexAdapter) Lock(key string) error   { return a.value.Lock(key) }
func (a mutexAdapter) Unlock(key string) error { return a.value.Unlock(key) }
