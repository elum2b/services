package payment

import (
	"net/http"
	"time"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/payment/adapters/platega"
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
	MaxConnections int
	QueryTimeout   time.Duration
	CacheL1Delay   time.Duration
	CacheL2Delay   time.Duration

	Cache        Storage
	CacheEnabled bool

	CacheSize     int
	CacheTTLCheck time.Duration

	Codec Codec
	Mutex Mutex

	PriceUpdateHTTPClient *http.Client
	PriceUpdateInterval   time.Duration
	PriceUpdateBaseURL    string
	DisablePriceUpdater   bool

	OrderExpirationInterval time.Duration
	OrderExpirationAge      time.Duration
	OrderExpirationBatch    int32
	DisableOrderExpiration  bool

	PlategaCredentialsResolver   platega.CredentialsResolver
	PlategaReconcileInterval     time.Duration
	PlategaReconcileMinAge       time.Duration
	PlategaReconcileMissingAfter time.Duration
	PlategaReconcileBatch        int32

	TONWalletSyncInterval    time.Duration
	OnCacheInvalidationError func(error)
}

type DatabaseParams struct {
	User     string
	Password string
	Database string
	Host     string
	Port     int

	Options Options
}

func toSQLWrapOptions(options Options) sqlwrap.Options {
	converted := sqlwrap.Options{
		MaxConnections: options.MaxConnections,
		QueryTimeout:   options.QueryTimeout,
		CacheEnabled:   options.CacheEnabled,
		CacheSize:      options.CacheSize,
		CacheTTLCheck:  options.CacheTTLCheck,
	}
	if options.Cache != nil {
		converted.Cache = wrapStorage{storage: options.Cache}
	}
	if options.Codec != nil {
		converted.Codec = wrapCodec{codec: options.Codec}
	}
	if options.Mutex != nil {
		converted.Mutex = wrapMutex{mutex: options.Mutex}
	}
	return converted
}

type wrapStorage struct {
	storage Storage
}

func (w wrapStorage) GetWithTTL(key string) ([]byte, time.Duration, error) {
	if w.storage == nil {
		return nil, 0, nil
	}
	return w.storage.GetWithTTL(key)
}

func (w wrapStorage) Set(key string, val []byte, exp time.Duration) error {
	if w.storage == nil {
		return nil
	}
	return w.storage.Set(key, val, exp)
}

func (w wrapStorage) Delete(key string) error {
	if w.storage == nil {
		return nil
	}
	return w.storage.Delete(key)
}

func (w wrapStorage) Reset() error {
	if w.storage == nil {
		return nil
	}
	return w.storage.Reset()
}

func (w wrapStorage) Close() error {
	if w.storage == nil {
		return nil
	}
	return w.storage.Close()
}

type wrapMutex struct {
	mutex Mutex
}

func (w wrapMutex) Lock(key string) error {
	if w.mutex == nil {
		return nil
	}
	return w.mutex.Lock(key)
}

func (w wrapMutex) Unlock(key string) error {
	if w.mutex == nil {
		return nil
	}
	return w.mutex.Unlock(key)
}

type wrapCodec struct {
	codec Codec
}

func (w wrapCodec) Marshal(v any) ([]byte, error) {
	if w.codec == nil {
		return nil, nil
	}
	return w.codec.Marshal(v)
}

func (w wrapCodec) Unmarshal(data []byte, v any) error {
	if w.codec == nil {
		return nil
	}
	return w.codec.Unmarshal(data, v)
}
