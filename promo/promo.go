package promo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib"

	serviceerrors "github.com/elum2b/services/errors"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	"github.com/elum2b/services/internal/utils/contextutil"
	goroutinemanager "github.com/elum2b/services/internal/utils/goroutine"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/promo/repository"
	"github.com/elum2b/services/promo/service/admin"
	"github.com/elum2b/services/promo/service/user"
)

type Promo struct {
	Admin *admin.Admin
	User  *user.User

	callbacks  *callbackutil.Store
	client     *sqlwrap.Client
	ownsClient bool
	rootCtx    context.Context
	rootCancel context.CancelFunc
	goroutines *goroutinemanager.Manager

	lifecycleMu    sync.Mutex
	callbacksToRun []callbackRegistration
	running        bool
}

func New() *Promo { return newPromo(context.Background(), sqlwrap.NewUnavailable(), true, Options{}) }

func NewWithDatabase(
	ctx context.Context,
	db *sql.DB,
	options Options,
) (*Promo, error) {
	client, err := sqlwrap.New(db, toSQLWrapOptions(options))
	if err != nil {
		return nil, serviceerrors.Wrap(
			serviceerrors.CodeInternalError,
			"promo sql client initialization failed",
			err,
		)
	}

	service := newPromo(ctx, client, false, options)
	if err := configureArchiveJobs(ctx, service, options); err != nil {
		_ = service.Close()
		return nil, err
	}

	return service, nil
}

func (p *Promo) Run(ctx context.Context, params DatabaseParams) error {
	if p == nil {
		return ErrServiceNil
	}

	p.lifecycleMu.Lock()

	if p.running {
		p.lifecycleMu.Unlock()

		return ErrServiceRunning
	}

	p.running = true

	registrations := append([]callbackRegistration(nil), p.callbacksToRun...)
	p.lifecycleMu.Unlock()

	running, err := open(ctx, params)
	if err != nil {
		p.lifecycleMu.Lock()

		p.running = false
		p.lifecycleMu.Unlock()

		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return nil
		}

		return wrapLifecycleError(err)
	}

	p.adopt(running)
	p.startWorkers()

	defer p.Close()

	errCh := make(chan error, len(registrations))
	for _, registration := range registrations {
		p.goroutines.Go("promo.callback", func() {
			errCh <- p.runCallback(registration.ctx, registration.handler, registration.options...)
		})
	}

	select {
	case <-p.rootCtx.Done():
		return nil
	case err := <-errCh:
		if errors.Is(err, context.Canceled) && p.rootCtx.Err() != nil {
			return nil
		}

		return wrapLifecycleError(err)
	}
}

func open(ctx context.Context, params DatabaseParams) (*Promo, error) {
	if params.User == "" {
		return nil, ErrDatabaseConfigRequired
	}

	if params.Database == "" {
		return nil, ErrDatabaseConfigRequired
	}

	db, err := openPostgres(ctx, params)
	if err != nil {
		return nil, serviceerrors.Wrap(
			serviceerrors.CodeUnavailable,
			"promo database connection failed",
			err,
		)
	}

	client, err := sqlwrap.New(db, toSQLWrapOptions(params.Options))
	if err != nil {
		_ = db.Close()

		return nil, serviceerrors.Wrap(
			serviceerrors.CodeInternalError,
			"promo sql client initialization failed",
			err,
		)
	}

	bootstrap := repository.NewWithOptions(client, repository.Options{
		QueryTimeout:             params.Options.QueryTimeout,
		CacheL1Delay:             params.Options.CacheL1Delay,
		CacheL2Delay:             params.Options.CacheL2Delay,
		OnCacheInvalidationError: params.Options.OnCacheInvalidationError,
	})
	if err := bootstrap.Bootstrap(ctx); err != nil {
		_ = bootstrap.Close()
		_ = client.Close()

		return nil, serviceerrors.Wrap(
			serviceerrors.CodeInternalError,
			"promo bootstrap failed",
			err,
		)
	}

	if err := bootstrap.Close(); err != nil {
		_ = client.Close()

		return nil, serviceerrors.Wrap(
			serviceerrors.CodeInternalError,
			"promo bootstrap shutdown failed",
			err,
		)
	}

	service := newPromo(ctx, client, true, params.Options)
	if err := configureArchiveJobs(ctx, service, params.Options); err != nil {
		_ = service.Close()
		return nil, err
	}

	return service, nil
}

func configureArchiveJobs(
	ctx context.Context,
	service *Promo,
	options Options,
) error {
	if err := jobs.Bootstrap(ctx, service.client.DB()); err != nil {
		return fmt.Errorf("bootstrap promo archive jobs: %w", err)
	}

	archive := options.Archive
	if archive == nil {
		directory := options.ArchiveDirectory
		if directory == "" {
			binaryPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve promo archive directory: %w", err)
			}

			directory = filepath.Join(
				filepath.Dir(binaryPath),
				"promo",
				"importexport",
			)
		}

		var err error

		archive, err = jobs.NewDiskArchive(directory)
		if err != nil {
			return err
		}
	}

	if err := service.Admin.ConfigureArchiveJobs(
		service.client.DB(),
		archive,
	); err != nil {
		return fmt.Errorf("configure promo archive jobs: %w", err)
	}

	return nil
}

func (p *Promo) startWorkers() {
	p.Admin.StartArchiveJobs(p.rootCtx)
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

	dsn, err := sqlwrap.PostgresDSN(sqlwrap.PostgresParams{
		User:        params.User,
		Password:    params.Password,
		Database:    params.Database,
		Host:        host,
		Port:        port,
		SSLMode:     params.SSLMode,
		SSLRootCert: params.SSLRootCert,
	})
	if err != nil {
		return nil, err
	}

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

func (p *Promo) adopt(running *Promo) {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	p.Admin, p.User = running.Admin, running.User
	p.callbacks, p.client, p.ownsClient = running.callbacks, running.client, running.ownsClient
	p.rootCtx, p.rootCancel = running.rootCtx, running.rootCancel
	p.goroutines = running.goroutines
}

func newPromo(
	ctx context.Context,
	db *sqlwrap.Client,
	ownsClient bool,
	options Options,
) *Promo {
	rootCtx, cancel := context.WithCancel(contextutil.Normalize(ctx))
	repositoryOptions := repository.Options{
		QueryTimeout:             options.QueryTimeout,
		CacheL1Delay:             options.CacheL1Delay,
		CacheL2Delay:             options.CacheL2Delay,
		OnCacheInvalidationError: options.OnCacheInvalidationError,
	}

	return &Promo{
		Admin: admin.NewWithRepositoryOptions(
			rootCtx,
			db,
			repositoryOptions,
		),
		User: user.NewWithRepositoryOptions(
			rootCtx,
			db,
			repositoryOptions,
		),
		callbacks:  callbackutil.NewWithTable(db.DB(), callbackutil.PromoTable),
		client:     db,
		ownsClient: ownsClient,
		rootCtx:    rootCtx,
		rootCancel: cancel,
		goroutines: goroutinemanager.New(),
	}
}

func (p *Promo) Close() error {
	if p == nil {
		return nil
	}

	if p.rootCancel != nil {
		p.rootCancel()
	}

	if p.goroutines != nil {
		p.goroutines.Close()
	}

	var err error

	if p.Admin != nil {
		err = errors.Join(err, p.Admin.Close())
	}

	if p.User != nil {
		err = errors.Join(err, p.User.Close())
	}

	if p.callbacks != nil {
		err = errors.Join(err, p.callbacks.Close())
	}

	if p.ownsClient && p.client != nil {
		err = errors.Join(err, p.client.Close())
	}

	return err
}

// IsReady reports whether the service is initialized and its lifecycle is active.
func (p *Promo) IsReady() bool {
	if p == nil {
		return false
	}

	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	return p.rootCtx != nil && p.rootCtx.Err() == nil &&
		!p.client.IsUnavailable() &&
		p.Admin != nil &&
		p.User != nil
}

func (p *Promo) bindContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	if p == nil {
		return contextutil.Merge(context.Background(), ctx)
	}

	return contextutil.Merge(p.rootCtx, ctx)
}
