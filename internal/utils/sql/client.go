package sql

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"
)

const defaultQueryTimeout = time.Second

var (
	// ErrNilDB is returned when a nil *sql.DB is passed.
	ErrNilDB = errors.New("sqlcwrap: nil db")
)

// Client is a sqlc-oriented DB wrapper with timeout + L1/L2 cache support.
type Client struct {
	db            *sql.DB
	queryTimeout  time.Duration
	cache         Storage
	inMemory      *l1Cache
	codec         Codec
	mutex         Mutex
	mx            sync.Mutex
	l2Expiry      sync.Map
	cacheVersions sync.Map
	CacheEnabled  bool
}

// Executor is a sqlc.DBTX-compatible view with an operation-specific timeout.
type Executor struct {
	client  *Client
	timeout time.Duration
}

// WithQueryTimeout returns a sqlc-compatible executor with the given timeout.
func (c *Client) WithQueryTimeout(timeout time.Duration) *Executor {
	return &Executor{client: c, timeout: timeout}
}

// New creates Client from existing sql.DB and options.
func New(db *sql.DB, opts ...Options) (*Client, error) {
	opt := defaultOptions(opts...)
	if db == nil {
		return nil, ErrNilDB
	}

	if opt.MaxConnections > 0 {
		db.SetMaxOpenConns(opt.MaxConnections)
		db.SetMaxIdleConns(opt.MaxConnections)
		db.SetConnMaxLifetime(5 * time.Minute)
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	core := &Client{
		db:           db,
		queryTimeout: opt.QueryTimeout,
		cache:        opt.Cache,
		inMemory:     newL1Cache(opt.CacheSize, opt.CacheTTLCheck),
		CacheEnabled: opt.CacheEnabled,
	}

	if opt.Codec != nil {
		core.codec = opt.Codec
	} else {
		core.codec = MsgpackCodec{}
	}
	if opt.Mutex != nil {
		core.mutex = opt.Mutex
	} else {
		core.mutex = NewMutex()
	}

	return core, nil
}

// ExecContext implements sqlc.DBTX with the configured query timeout.
func (c *Client) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	qctx, cancel := c.queryContext(ctx, 0)
	defer cancel()
	return c.db.ExecContext(qctx, query, args...)
}

// PrepareContext implements sqlc.DBTX with the configured query timeout.
func (c *Client) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	qctx, cancel := c.queryContext(ctx, 0)
	defer cancel()
	return c.db.PrepareContext(qctx, query)
}

// QueryContext implements sqlc.DBTX with the configured query timeout.
func (c *Client) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	qctx, _ := c.queryContext(ctx, 0)
	return c.db.QueryContext(qctx, query, args...)
}

// QueryRowContext implements sqlc.DBTX with the configured query timeout.
func (c *Client) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	qctx, _ := c.queryContext(ctx, 0)
	return c.db.QueryRowContext(qctx, query, args...)
}

func (e *Executor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	qctx, cancel := e.client.queryContext(ctx, e.timeout)
	defer cancel()
	return e.client.db.ExecContext(qctx, query, args...)
}

func (e *Executor) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	qctx, cancel := e.client.queryContext(ctx, e.timeout)
	defer cancel()
	return e.client.db.PrepareContext(qctx, query)
}

func (e *Executor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	qctx, _ := e.client.queryContext(ctx, e.timeout)
	return e.client.db.QueryContext(qctx, query, args...)
}

func (e *Executor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	qctx, _ := e.client.queryContext(ctx, e.timeout)
	return e.client.db.QueryRowContext(qctx, query, args...)
}

// DB returns the underlying *sql.DB.
func (c *Client) DB() *sql.DB {
	if c == nil {
		return nil
	}
	return c.db
}

// DeleteCache removes a single key from both L1 and L2 caches.
func (c *Client) DeleteCache(key string) error {
	if c == nil || key == "" {
		return nil
	}
	if c.inMemory != nil {
		c.inMemory.Delete(key)
	}
	c.l2Expiry.Delete(key)
	if c.cache != nil {
		return c.cache.Delete(key)
	}
	return nil
}

// ResetCache clears both L1 and L2 caches.
func (c *Client) ResetCache() error {
	if c == nil {
		return nil
	}
	if c.inMemory != nil {
		c.inMemory.Reset()
	}
	c.l2Expiry.Range(func(key, _ any) bool {
		c.l2Expiry.Delete(key)
		return true
	})
	c.cacheVersions.Range(func(key, _ any) bool {
		c.cacheVersions.Delete(key)
		return true
	})
	if c.cache != nil {
		return c.cache.Reset()
	}
	return nil
}

// Close closes DB and external cache (if provided).
func (c *Client) Close() error {
	if c == nil {
		return nil
	}

	if c.cache != nil {
		_ = c.cache.Close()
	}
	if c.inMemory != nil {
		c.inMemory.Close()
	}
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

func createContextWithTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if timeout <= 0 {
		timeout = defaultQueryTimeout
	}
	return context.WithTimeout(parent, timeout)
}

func (c *Client) queryContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 && c != nil {
		timeout = c.queryTimeout
	}
	return createContextWithTimeout(parent, timeout)
}

func (c *Client) getMutex() Mutex {
	c.mx.Lock()
	defer c.mx.Unlock()
	if c.mutex == nil {
		c.mutex = NewMutex()
	}
	return c.mutex
}

func (c *Client) rememberL2Expiry(key string, ttl time.Duration) {
	if c == nil || key == "" || ttl <= 0 {
		return
	}
	c.l2Expiry.Store(key, time.Now().Add(ttl))
}

func (c *Client) l2RemainingTTL(key string, fallback time.Duration) time.Duration {
	if c == nil || key == "" {
		return fallback
	}
	raw, ok := c.l2Expiry.Load(key)
	if !ok {
		return fallback
	}

	expiresAt, ok := raw.(time.Time)
	if !ok {
		return fallback
	}

	remaining := time.Until(expiresAt)
	if remaining <= 0 {
		c.l2Expiry.Delete(key)
		return 0
	}
	if fallback > 0 && remaining > fallback {
		return fallback
	}
	return remaining
}
