package reference

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	serviceerrors "github.com/elum2b/services/errors"
	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/elum2b/services/reference/repository"
	"github.com/elum2b/services/reference/service/admin"
	"github.com/elum2b/services/reference/service/user"
)

type Reference struct {
	Admin *admin.Admin
	User  *user.User

	client     *sqlwrap.Client
	ownsClient bool
	rootCtx    context.Context
	rootCancel context.CancelFunc

	lifecycleMu sync.Mutex
	params      DatabaseParams
	running     bool
}

func New(params DatabaseParams) *Reference {
	return &Reference{params: params}
}

func NewWithDatabase(ctx context.Context, db *sql.DB, options Options) (*Reference, error) {
	client, err := sqlwrap.New(db, toSQLWrapOptions(options))
	if err != nil {
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, "reference sql client initialization failed", err)
	}
	return newReference(ctx, client, false, options), nil
}

func (r *Reference) Run(ctx context.Context) error {
	if r == nil {
		return ErrServiceNil
	}
	r.lifecycleMu.Lock()
	if r.running {
		r.lifecycleMu.Unlock()
		return ErrServiceRunning
	}
	r.running = true
	params := r.params
	r.lifecycleMu.Unlock()

	running, err := open(ctx, params)
	if err != nil {
		r.lifecycleMu.Lock()
		r.running = false
		r.lifecycleMu.Unlock()
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return nil
		}
		return wrapLifecycleError(err)
	}
	r.adopt(running)
	defer r.Close()
	<-r.rootCtx.Done()
	return nil
}

func open(ctx context.Context, params DatabaseParams) (*Reference, error) {
	if params.User == "" {
		return nil, ErrDatabaseConfigRequired
	}
	if params.Database == "" {
		return nil, ErrDatabaseConfigRequired
	}
	db, err := openPostgres(ctx, params)
	if err != nil {
		return nil, serviceerrors.Wrap(serviceerrors.CodeUnavailable, "reference database connection failed", err)
	}
	client, err := sqlwrap.New(db, toSQLWrapOptions(params.Options))
	if err != nil {
		_ = db.Close()
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, "reference sql client initialization failed", err)
	}
	bootstrap := repository.NewWithOptions(client, repository.Options{
		QueryTimeout:             params.Options.QueryTimeout,
		CacheL1Delay:             params.Options.CacheL1Delay,
		CacheL2Delay:             params.Options.CacheL2Delay,
		OnCacheInvalidationError: params.Options.OnCacheInvalidationError,
	})
	if err := bootstrap.Bootstrap(contextutil.Normalize(ctx)); err != nil {
		_ = bootstrap.Close()
		_ = client.Close()
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, "reference bootstrap failed", err)
	}
	if err := bootstrap.Close(); err != nil {
		_ = client.Close()
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, "reference bootstrap shutdown failed", err)
	}
	return newReference(ctx, client, true, params.Options), nil
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

func (r *Reference) adopt(running *Reference) {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	r.Admin, r.User = running.Admin, running.User
	r.client, r.ownsClient = running.client, running.ownsClient
	r.rootCtx, r.rootCancel = running.rootCtx, running.rootCancel
}

func newReference(ctx context.Context, db *sqlwrap.Client, ownsClient bool, options Options) *Reference {
	rootCtx, cancel := context.WithCancel(contextutil.Normalize(ctx))
	repositoryOptions := repository.Options{
		QueryTimeout:             options.QueryTimeout,
		CacheL1Delay:             options.CacheL1Delay,
		CacheL2Delay:             options.CacheL2Delay,
		OnCacheInvalidationError: options.OnCacheInvalidationError,
	}
	return &Reference{
		Admin:  admin.NewWithRepositoryOptions(rootCtx, db, repositoryOptions),
		User:   user.NewWithRepositoryOptions(rootCtx, db, repositoryOptions),
		client: db, ownsClient: ownsClient, rootCtx: rootCtx, rootCancel: cancel,
	}
}

func (r *Reference) Close() error {
	if r == nil {
		return nil
	}
	if r.rootCancel != nil {
		r.rootCancel()
	}
	var err error
	if r.Admin != nil {
		err = errors.Join(err, r.Admin.Close())
	}
	if r.User != nil {
		err = errors.Join(err, r.User.Close())
	}
	if r.ownsClient && r.client != nil {
		err = errors.Join(err, r.client.Close())
	}
	return err
}

// IsReady reports whether the service is initialized and its lifecycle is active.
func (r *Reference) IsReady() bool {
	if r == nil {
		return false
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	return r.rootCtx != nil && r.rootCtx.Err() == nil && r.Admin != nil && r.User != nil
}
