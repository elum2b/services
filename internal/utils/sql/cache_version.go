package sql

import (
	"crypto/rand"
	"encoding/hex"
	"sync/atomic"
	"time"
)

var cacheVersionCounter atomic.Uint64

type cacheVersionState struct {
	key     string
	version string
	publish bool
}

// CacheVersion returns the current version for a cache namespace.
func (c *Client) CacheVersion(parts ...any) string {
	state := c.prepareCacheVersion(parts)
	_ = c.publishCacheVersion(state)
	return state.version
}

func (c *Client) prepareCacheVersion(parts []any) cacheVersionState {
	if c == nil {
		return cacheVersionState{version: "0"}
	}
	key := cacheVersionKey(parts...)
	if c.cache != nil {
		if data, _, err := c.cache.GetWithTTL(key); err == nil && len(data) > 0 {
			version := string(data)
			c.cacheVersions.Store(key, version)
			return cacheVersionState{key: key, version: version}
		}
		version := newCacheVersion()
		return cacheVersionState{key: key, version: version, publish: true}
	}
	if raw, ok := c.cacheVersions.Load(key); ok {
		if version, ok := raw.(string); ok && version != "" {
			return cacheVersionState{key: key, version: version}
		}
	}
	version := newCacheVersion()
	return cacheVersionState{key: key, version: version, publish: true}
}

func (c *Client) publishCacheVersion(state cacheVersionState) error {
	if c == nil || !state.publish || state.key == "" || state.version == "" {
		return nil
	}
	c.cacheVersions.Store(state.key, state.version)
	if c.cache != nil {
		return c.cache.Set(state.key, []byte(state.version), 0)
	}
	return nil
}

// BumpCacheVersion changes the version for a cache namespace.
func (c *Client) BumpCacheVersion(parts ...any) error {
	if c == nil {
		return nil
	}
	key := cacheVersionKey(parts...)
	version := newCacheVersion()
	c.cacheVersions.Store(key, version)
	if c.cache != nil {
		return c.cache.Set(key, []byte(version), 0)
	}
	return nil
}

// VersionedCacheKey builds a cache key that includes a namespace version.
func (c *Client) VersionedCacheKey(scope []any, parts ...any) string {
	version := "0"
	if c != nil {
		version = c.CacheVersion(scope...)
	}
	args := append([]any{"versioned_cache", scope, version}, parts...)
	return CreateKey(args...)
}

func cacheVersionKey(parts ...any) string {
	return "cache_version:" + CreateKey(parts...)
}

func versionMutexKey(versionKey string) string {
	return "mutex_" + versionKey
}

func newCacheVersion() string {
	var data [8]byte
	if _, err := rand.Read(data[:]); err == nil {
		return hex.EncodeToString(data[:])
	}
	return CreateKey(time.Now().UnixNano(), cacheVersionCounter.Add(1))[:16]
}
