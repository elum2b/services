package cpa

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/elum2b/services/cpa/repository"
	"github.com/elum2b/services/cpa/service/admin"
	"github.com/elum2b/services/cpa/service/user"
	serviceerrors "github.com/elum2b/services/errors"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	"github.com/elum2b/services/internal/utils/contextutil"
	goroutinemanager "github.com/elum2b/services/internal/utils/goroutine"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

type CPA struct {
	Admin *admin.Admin
	User  *user.User

	callbacks  *callbackutil.Store
	client     *sqlwrap.Client
	ownsClient bool
	rootCtx    context.Context
	rootCancel context.CancelFunc
	goroutines *goroutinemanager.Manager

	lifecycleMu    sync.Mutex
	params         DatabaseParams
	callbacksToRun []callbackRegistration
	running        bool
}

func New(params DatabaseParams) *CPA {
	return &CPA{params: params}
}

func NewWithDatabase(ctx context.Context, db *sql.DB, options Options) (*CPA, error) {
	options = normalizeOptions(options)
	client, err := sqlwrap.New(db, toSQLWrapOptions(options))
	if err != nil {
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, "cpa sql client initialization failed", err)
	}
	return newCPA(ctx, client, false, options), nil
}

func (c *CPA) Run(ctx context.Context) error {
	if c == nil {
		return ErrServiceNil
	}
	c.lifecycleMu.Lock()
	if c.running {
		c.lifecycleMu.Unlock()
		return ErrServiceRunning
	}
	c.running = true
	params := c.params
	registrations := append([]callbackRegistration(nil), c.callbacksToRun...)
	c.lifecycleMu.Unlock()

	running, err := open(ctx, params)
	if err != nil {
		c.lifecycleMu.Lock()
		c.running = false
		c.lifecycleMu.Unlock()
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return nil
		}
		return wrapLifecycleError(err)
	}
	c.adopt(running)
	defer c.Close()

	errCh := make(chan error, len(registrations))
	for _, registration := range registrations {
		registration := registration
		c.goroutines.Go("cpa.callback", func() {
			errCh <- c.runCallback(registration.ctx, registration.handler, registration.options...)
		})
	}
	select {
	case <-c.rootCtx.Done():
		return nil
	case err := <-errCh:
		if errors.Is(err, context.Canceled) && c.rootCtx.Err() != nil {
			return nil
		}
		return wrapLifecycleError(err)
	}
}

func open(ctx context.Context, params DatabaseParams) (*CPA, error) {
	if params.User == "" {
		return nil, ErrDatabaseUserRequired
	}
	if params.Database == "" {
		return nil, ErrDatabaseNameRequired
	}
	options := normalizeOptions(params.Options)
	db, err := openPostgres(ctx, params)
	if err != nil {
		return nil, serviceerrors.Wrap(serviceerrors.CodeUnavailable, "cpa database connection failed", err)
	}
	client, err := sqlwrap.New(db, toSQLWrapOptions(params.Options))
	if err != nil {
		_ = db.Close()
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, "cpa sql client initialization failed", err)
	}
	bootstrap := repository.NewWithOptions(client, repository.Options{
		QueryTimeout:             options.QueryTimeout,
		CacheL1Delay:             options.CacheL1Delay,
		CacheL2Delay:             options.CacheL2Delay,
		OnCacheInvalidationError: options.OnCacheInvalidationError,
	})
	if err := bootstrap.Bootstrap(ctx); err != nil {
		_ = bootstrap.Close()
		_ = client.Close()
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, "cpa bootstrap failed", err)
	}
	if err := bootstrap.Close(); err != nil {
		_ = client.Close()
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, "cpa bootstrap shutdown failed", err)
	}
	return newCPA(ctx, client, true, options), nil
}

func openPostgres(ctx context.Context, params DatabaseParams) (*sql.DB, error) {
	host := params.Host
	if host == "" {
		host = "localhost"
	}
	port := params.Port
	if port == 0 {
		port = 5432
	}
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", host, port, params.User, params.Password, params.Database)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (c *CPA) adopt(running *CPA) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	c.Admin, c.User = running.Admin, running.User
	c.callbacks, c.client, c.ownsClient = running.callbacks, running.client, running.ownsClient
	c.rootCtx, c.rootCancel = running.rootCtx, running.rootCancel
	c.goroutines = running.goroutines
}

func newCPA(ctx context.Context, db *sqlwrap.Client, ownsClient bool, options Options) *CPA {
	options = normalizeOptions(options)
	rootCtx, rootCancel := context.WithCancel(contextutil.Normalize(ctx))
	repositoryOptions := repository.Options{
		QueryTimeout:             options.QueryTimeout,
		CacheL1Delay:             options.CacheL1Delay,
		CacheL2Delay:             options.CacheL2Delay,
		OnCacheInvalidationError: options.OnCacheInvalidationError,
	}
	return &CPA{
		Admin:      admin.NewWithRepositoryOptions(rootCtx, db, repositoryOptions),
		User:       user.NewWithRepositoryOptions(rootCtx, db, repositoryOptions),
		callbacks:  callbackutil.NewWithTable(db.DB(), callbackutil.CPATable),
		client:     db,
		ownsClient: ownsClient,
		rootCtx:    rootCtx,
		rootCancel: rootCancel,
		goroutines: goroutinemanager.New(),
	}
}

func (c *CPA) Close() error {
	if c == nil {
		return nil
	}
	if c.rootCancel != nil {
		c.rootCancel()
	}
	if c.goroutines != nil {
		c.goroutines.Close()
	}
	var err error
	if c.Admin != nil {
		err = errors.Join(err, c.Admin.Close())
	}
	if c.User != nil {
		err = errors.Join(err, c.User.Close())
	}
	if c.callbacks != nil {
		err = errors.Join(err, c.callbacks.Close())
	}
	if c.ownsClient && c.client != nil {
		err = errors.Join(err, c.client.Close())
	}
	return err
}

// IsReady reports whether the service is initialized and its lifecycle is active.
func (c *CPA) IsReady() bool {
	if c == nil {
		return false
	}
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	return c.rootCtx != nil && c.rootCtx.Err() == nil && c.Admin != nil && c.User != nil
}

func (c *CPA) bindContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c == nil {
		return contextutil.Merge(context.Background(), ctx)
	}
	return contextutil.Merge(c.rootCtx, ctx)
}
