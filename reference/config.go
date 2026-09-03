package reference

import (
	"time"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	resourcecache "github.com/elum2b/services/reference/service/resource/cache"
	resourcestorage "github.com/elum2b/services/reference/storage"
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
	// ArchiveImportTimeout bounds the database transaction and media upload.
	ArchiveImportTimeout time.Duration
	// ArchiveJobLease must exceed ArchiveImportTimeout for async imports. Values
	// not exceeding it are raised to ArchiveImportTimeout plus five minutes.
	ArchiveJobLease time.Duration
	// ResourceStorage selects S3/MinIO when Bucket is set; otherwise resources
	// are saved beside the executable in the reference directory.
	// Async import/export archives use <Directory>/importexport. S3 resource
	// storage is rejected until a shared S3 archive adapter is configured.
	ResourceStorage     resourcestorage.Config
	ResourceMediaCache  resourcecache.Config
	ResourceGCInterval  time.Duration
	ResourceGCBatch     int32
	ResourceGCRetention time.Duration
}

type DatabaseParams struct {
	User, Password, Database, Host string
	Port                           int
	SSLMode, SSLRootCert           string
	Options                        Options
}

func toSQLWrapOptions(value Options) sqlwrap.Options {
	result := sqlwrap.Options{
		MaxConnections: value.MaxConnections, QueryTimeout: value.QueryTimeout,
		CacheEnabled: value.CacheEnabled, CacheSize: value.CacheSize,
		CacheTTLCheck: value.CacheTTLCheck,
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
