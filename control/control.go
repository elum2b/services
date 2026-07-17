package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/elum2b/services/control/repository"
	"github.com/elum2b/services/control/service/admin"
	"github.com/elum2b/services/control/service/internalapi"
	serviceerrors "github.com/elum2b/services/errors"
	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Control struct {
	Admin    *admin.Admin
	Internal *internalapi.Internal

	client     *sqlwrap.Client
	ownsClient bool
	rootCtx    context.Context
	rootCancel context.CancelFunc

	lifecycleMu sync.Mutex
	params      DatabaseParams
	running     bool
}

func New(params DatabaseParams) *Control { return &Control{params: params} }

func NewWithDatabase(ctx context.Context, db *sql.DB, options Options) (*Control, error) {
	client, err := sqlwrap.New(db, toSQLWrapOptions(options))
	if err != nil {
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, "control sql client initialization failed", err)
	}
	return newControl(ctx, client, false, options), nil
}

func (c *Control) Run(ctx context.Context) error {
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
	c.lifecycleMu.Unlock()

	running, err := open(ctx, params)
	if err != nil {
		c.lifecycleMu.Lock()
		c.running = false
		c.lifecycleMu.Unlock()
		return wrapLifecycleError(err)
	}
	c.adopt(running)
	defer c.Close()
	<-c.rootCtx.Done()
	return nil
}

func open(ctx context.Context, params DatabaseParams) (*Control, error) {
	if params.User == "" || params.Database == "" {
		return nil, ErrDatabaseConfigRequired
	}
	db, err := openPostgres(ctx, params)
	if err != nil {
		return nil, serviceerrors.Wrap(serviceerrors.CodeUnavailable, "control database connection failed", err)
	}
	client, err := sqlwrap.New(db, toSQLWrapOptions(params.Options))
	if err != nil {
		_ = db.Close()
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, "control sql client initialization failed", err)
	}
	bootstrap := repository.NewWithOptions(client, repository.Options{
		QueryTimeout:             params.Options.QueryTimeout,
		CacheL1Delay:             params.Options.CacheL1Delay,
		CacheL2Delay:             params.Options.CacheL2Delay,
		OnCacheInvalidationError: params.Options.OnCacheInvalidationError,
		SecretEncryptionKey:      params.Options.SecretEncryptionKey,
	})
	if err := bootstrap.Bootstrap(contextutil.Normalize(ctx)); err != nil {
		_ = bootstrap.Close()
		_ = client.Close()
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, "control bootstrap failed", err)
	}
	_ = bootstrap.Close()
	return newControl(ctx, client, true, params.Options), nil
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
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		params.User,
		params.Password,
		host,
		port,
		params.Database,
	)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(params.Options.MaxConnections)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func newControl(ctx context.Context, db *sqlwrap.Client, ownsClient bool, options Options) *Control {
	rootCtx, cancel := context.WithCancel(contextutil.Normalize(ctx))
	repositoryOptions := repository.Options{
		QueryTimeout:             options.QueryTimeout,
		CacheL1Delay:             options.CacheL1Delay,
		CacheL2Delay:             options.CacheL2Delay,
		OnCacheInvalidationError: options.OnCacheInvalidationError,
		SecretEncryptionKey:      options.SecretEncryptionKey,
	}
	return &Control{
		Admin:      admin.NewWithOptions(rootCtx, db, repositoryOptions),
		Internal:   internalapi.NewWithOptions(rootCtx, db, repositoryOptions),
		client:     db,
		ownsClient: ownsClient,
		rootCtx:    rootCtx,
		rootCancel: cancel,
	}
}

func (c *Control) adopt(running *Control) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	c.Admin, c.Internal = running.Admin, running.Internal
	c.client, c.ownsClient, c.rootCtx, c.rootCancel = running.client, running.ownsClient, running.rootCtx, running.rootCancel
}

func (c *Control) Close() error {
	if c == nil {
		return nil
	}
	if c.rootCancel != nil {
		c.rootCancel()
	}
	var err error
	if c.Admin != nil {
		err = errors.Join(err, c.Admin.Close())
	}
	if c.Internal != nil {
		err = errors.Join(err, c.Internal.Close())
	}
	if c.ownsClient && c.client != nil {
		err = errors.Join(err, c.client.Close())
	}
	return err
}

func (c *Control) IsReady() bool {
	if c == nil {
		return false
	}
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	return c.rootCtx != nil && c.rootCtx.Err() == nil && c.Admin != nil && c.Internal != nil
}
