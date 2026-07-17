package sql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Params controls timeout and cache behavior for Query.
type Params struct {
	Key      string
	KeyParts []any
	Timeout  time.Duration

	CacheL2Delay time.Duration
	CacheL1Delay time.Duration

	// Deprecated: use CacheL2Delay.
	CacheDelay time.Duration
	// Deprecated: use CacheL1Delay.
	NodeCacheDelay time.Duration

	// CacheVersionScope adds a namespace version to the cache key. If the
	// version is missing, Query publishes the new version only after caching data.
	CacheVersionScope []any
}

// Query executes a typed loader under timeout and optional L1/L2 cache.
func Query[T any](
	ctx context.Context,
	client *Client,
	params Params,
	loader func(context.Context) (T, error),
) (T, error) {
	var zero T
	if client == nil || client.db == nil {
		return zero, ErrNilDB
	}
	if loader == nil {
		return zero, errors.New("sqlcwrap: loader is nil")
	}

	l1TTL := params.l1Delay()
	l2TTL := params.l2Delay()
	useCache := client.CacheEnabled && (l1TTL > 0 || l2TTL > 0)
	key := params.Key
	if useCache && key == "" && len(params.KeyParts) > 0 {
		key = CreateKey(params.KeyParts...)
	}
	versionState := cacheVersionState{}
	var unlockVersion func()
	if useCache && key != "" && len(params.CacheVersionScope) > 0 {
		versionState = client.prepareCacheVersion(params.CacheVersionScope)
		if versionState.publish {
			mutex := client.getMutex()
			mutexKey := versionMutexKey(versionState.key)
			if err := mutex.Lock(mutexKey); err != nil {
				return zero, err
			}
			unlockVersion = func() { _ = mutex.Unlock(mutexKey) }
			defer func() {
				if unlockVersion != nil {
					unlockVersion()
				}
			}()
			versionState = client.prepareCacheVersion(params.CacheVersionScope)
		}
		key = CreateKey("versioned_cache", params.CacheVersionScope, versionState.version, key)
	}

	if useCache && key != "" {
		if value, ok := checkCaches[T](client, key, l1TTL, l2TTL); ok {
			return value, nil
		}

		mutexKey := "mutex_" + key
		mutex := client.getMutex()
		if err := mutex.Lock(mutexKey); err != nil {
			return zero, err
		}
		defer func() { _ = mutex.Unlock(mutexKey) }()

		if value, ok := checkCaches[T](client, key, l1TTL, l2TTL); ok {
			return value, nil
		}
	}

	qctx, cancel := client.queryContext(ctx, params.Timeout)
	defer cancel()

	value, err := loader(qctx)
	if err != nil {
		return zero, err
	}

	if useCache && key != "" {
		l1EffectiveTTL := effectiveL1TTL(l1TTL, l2TTL)
		cacheStored := false
		if l1EffectiveTTL > 0 {
			client.inMemory.Set(key, value, l1EffectiveTTL)
			cacheStored = true
		}
		if l2TTL > 0 && client.cache != nil {
			if data, encodeErr := client.codec.Marshal(value); encodeErr == nil {
				if setErr := client.cache.Set(key, data, l2TTL); setErr == nil {
					client.rememberL2Expiry(key, l2TTL)
					cacheStored = true
				}
			}
		}
		if cacheStored {
			_ = client.publishCacheVersion(versionState)
		}
	}

	return value, nil
}

// Exec executes a command loader under timeout.
func Exec(
	ctx context.Context,
	client *Client,
	params Params,
	loader func(context.Context) error,
) error {
	if loader == nil {
		return errors.New("sqlcwrap: loader is nil")
	}
	_, err := Query(ctx, client, Params{Timeout: params.Timeout}, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, loader(ctx)
	})
	return err
}

// Transaction executes a typed loader in a transaction under timeout.
func Transaction[T any](
	ctx context.Context,
	client *Client,
	params Params,
	loader func(context.Context, *sql.Tx) (T, error),
) (out T, outErr error) {
	if client == nil || client.db == nil {
		return out, ErrNilDB
	}
	if loader == nil {
		return out, errors.New("sqlcwrap: loader is nil")
	}

	qctx, cancel := client.queryContext(ctx, params.Timeout)
	defer cancel()

	tx, err := client.db.BeginTx(qctx, nil)
	if err != nil {
		return out, err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = tx.Rollback()
			panic(recovered)
		}
		if outErr != nil {
			_ = tx.Rollback()
		}
	}()

	out, outErr = loader(qctx, tx)
	if outErr != nil {
		return out, outErr
	}
	if err := tx.Commit(); err != nil {
		var zero T
		return zero, fmt.Errorf("failed to commit tx: %w", err)
	}
	return out, nil
}

func checkCaches[T any](client *Client, key string, l1TTL, l2TTL time.Duration) (T, bool) {
	var zero T
	if l1TTL > 0 {
		if value, ok := client.inMemory.Get(key); ok {
			if typed, ok := value.(T); ok {
				return typed, true
			}
		}
	}

	if l2TTL > 0 && client.cache != nil {
		data, l2RemainingTTL, err := getL2Value(client, key, l2TTL)
		if err == nil && len(data) > 0 {
			var value T
			if decodeErr := client.codec.Unmarshal(data, &value); decodeErr == nil {
				l1EffectiveTTL := effectiveL1TTL(l1TTL, l2RemainingTTL)
				if l1EffectiveTTL > 0 {
					client.inMemory.Set(key, value, l1EffectiveTTL)
				}
				return value, true
			}
		}
	}
	return zero, false
}

func getL2Value(client *Client, key string, fallbackTTL time.Duration) ([]byte, time.Duration, error) {
	data, ttl, err := client.cache.GetWithTTL(key)
	if err != nil {
		return nil, 0, err
	}
	if ttl <= 0 {
		ttl = client.l2RemainingTTL(key, fallbackTTL)
	} else {
		if fallbackTTL > 0 && ttl > fallbackTTL {
			ttl = fallbackTTL
		}
		client.rememberL2Expiry(key, ttl)
	}
	return data, ttl, nil
}

func (p Params) l1Delay() time.Duration {
	if p.CacheL1Delay > 0 {
		return p.CacheL1Delay
	}
	return p.NodeCacheDelay
}

func (p Params) l2Delay() time.Duration {
	if p.CacheL2Delay > 0 {
		return p.CacheL2Delay
	}
	return p.CacheDelay
}

func effectiveL1TTL(l1TTL, l2TTL time.Duration) time.Duration {
	if l1TTL <= 0 {
		return 0
	}
	if l2TTL > 0 && l2TTL < l1TTL {
		return l2TTL
	}
	return l1TTL
}
