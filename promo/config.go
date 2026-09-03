package promo

import (
	"time"

	"github.com/elum2b/services/internal/utils/importexport/jobs"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

type Storage interface {
	GetWithTTL(key string) ([]byte, time.Duration, error)
	Set(key string, value []byte, expiration time.Duration) error
	Delete(key string) error
	Reset() error
	Close() error
}

type Mutex interface {
	Lock(key string) error
	Unlock(key string) error
}

type Codec interface {
	Marshal(value any) ([]byte, error)
	Unmarshal(data []byte, value any) error
}

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
	OnCacheInvalidationError func(error)
	// Archive stores async import/export ZIPs. Set this to shared durable
	// storage when workers run on more than one node.
	Archive jobs.Archive
	// ArchiveDirectory overrides the local disk archive directory when Archive
	// is nil. The executable-relative development directory remains the default.
	ArchiveDirectory string
}

type DatabaseParams struct {
	User, Password, Database, Host string
	Port                           int
	SSLMode, SSLRootCert           string
	Options                        Options
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
		result.Cache = storageAdapter{value.Cache}
	}

	if value.Codec != nil {
		result.Codec = codecAdapter{value.Codec}
	}

	if value.Mutex != nil {
		result.Mutex = mutexAdapter{value.Mutex}
	}

	return result
}

type storageAdapter struct{ Storage }
type codecAdapter struct{ Codec }
type mutexAdapter struct{ Mutex }
