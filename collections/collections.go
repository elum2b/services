package collections

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/elum2b/services/collections/internal/archiveadapter"
	"github.com/elum2b/services/collections/repository"
	"github.com/elum2b/services/collections/service/admin"
	"github.com/elum2b/services/collections/service/internalapi"
	"github.com/elum2b/services/collections/service/user"

	serviceerrors "github.com/elum2b/services/errors"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	"github.com/elum2b/services/internal/utils/contextutil"
	goroutinemanager "github.com/elum2b/services/internal/utils/goroutine"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Collections struct {
	Admin    *admin.Admin
	User     *user.User
	Internal *internalapi.Internal

	callbacks  *callbackutil.Store
	client     *sqlwrap.Client
	ownsClient bool
	rootCtx    context.Context
	rootCancel context.CancelFunc
	goroutines *goroutinemanager.Manager

	lifecycleMu    sync.Mutex
	callbacksToRun []callbackRegistration
	running        bool
	closed         bool
	startupDone    chan struct{}
	startupCancel  context.CancelFunc
	activeCancel   context.CancelFunc
	closeDone      chan struct{}
	closeErr       error
	callbackRuns   sync.WaitGroup
}

func New() *Collections {
	return newCollections(context.Background(), sqlwrap.NewUnavailable(), true, Options{})
}

func NewWithDatabase(
	ctx context.Context,
	db *sql.DB,
	options Options,
) (*Collections, error) {
	client, err := sqlwrap.NewWithContext(ctx, db, toSQLWrapOptions(options))
	if err != nil {
		return nil, serviceerrors.Wrap(
			serviceerrors.CodeInternalError,
			"collections sql client initialization failed",
			err,
		)
	}

	service := newCollections(ctx, client, false, options)
	if err := configureArchiveJobs(ctx, service, options); err != nil {
		_ = service.Close()
		return nil, err
	}

	return service, nil
}

func (p *Collections) Run(ctx context.Context, params DatabaseParams) error {
	if p == nil || p.rootCtx == nil || p.goroutines == nil {
		return ErrServiceNil
	}
	p.lifecycleMu.Lock()
	if p.closed {
		p.lifecycleMu.Unlock()
		return ErrServiceClosed
	}
	if p.running || !p.client.IsUnavailable() {
		p.lifecycleMu.Unlock()
		return ErrServiceRunning
	}
	runCtx, cancel := contextutil.Merge(p.rootCtx, ctx)
	p.running = true
	p.startupCancel = cancel
	startupDone := make(chan struct{})
	p.startupDone = startupDone
	registrations := append([]callbackRegistration(nil), p.callbacksToRun...)
	p.lifecycleMu.Unlock()
	defer cancel()

	active, err := open(runCtx, params)
	p.lifecycleMu.Lock()
	if err != nil || p.closed || runCtx.Err() != nil {
		p.running = false
		p.lifecycleMu.Unlock()
		if active != nil {
			_ = active.Close()
		}
		close(startupDone)
		if runCtx.Err() != nil {
			return nil
		}
		return wrapLifecycleError(err)
	}
	// The root context/manager remain stable throughout the shell's lifetime.
	// Only fully initialized dependencies are published, while Close is excluded.
	oldClient := p.client
	p.Admin, p.User, p.Internal = active.Admin, active.User, active.Internal
	p.callbacks, p.client, p.ownsClient = active.callbacks, active.client, active.ownsClient
	p.activeCancel = active.rootCancel
	p.lifecycleMu.Unlock()
	_ = oldClient.Close()
	active.goroutines.Close()
	close(startupDone)
	defer p.Close()

	p.startWorkers()
	errCh := make(chan error, len(registrations))
	for _, registration := range registrations {
		p.goroutines.Go("collections.callback", func() { errCh <- p.runCallback(registration.ctx, registration.handler, registration.options...) })
	}
	select {
	case <-runCtx.Done():
		return nil
	case err := <-errCh:
		if runCtx.Err() != nil && errors.Is(err, context.Canceled) {
			return nil
		}
		return wrapLifecycleError(err)
	}
}

func open(ctx context.Context, params DatabaseParams) (*Collections, error) {
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
			"collections database connection failed",
			err,
		)
	}

	client, err := sqlwrap.NewWithContext(ctx, db, toSQLWrapOptions(params.Options))
	if err != nil {
		_ = db.Close()

		return nil, serviceerrors.Wrap(
			serviceerrors.CodeInternalError,
			"collections sql client initialization failed",
			err,
		)
	}

	bootstrap := repository.NewWithOptions(client, repository.Options{
		QueryTimeout:             params.Options.QueryTimeout,
		CacheL1Delay:             params.Options.CacheL1Delay,
		CacheL2Delay:             params.Options.CacheL2Delay,
		CatalogCacheTTL:          params.Options.CatalogCacheTTL,
		OnCacheInvalidationError: params.Options.OnCacheInvalidationError,
	})
	if err := bootstrap.Bootstrap(ctx); err != nil {
		_ = bootstrap.Close()
		_ = client.Close()

		return nil, serviceerrors.Wrap(
			serviceerrors.CodeInternalError,
			"collections bootstrap failed",
			err,
		)
	}

	if err := bootstrap.Close(); err != nil {
		_ = client.Close()

		return nil, serviceerrors.Wrap(
			serviceerrors.CodeInternalError,
			"collections bootstrap shutdown failed",
			err,
		)
	}

	service := newCollections(ctx, client, true, params.Options)
	if err := configureArchiveJobs(ctx, service, params.Options); err != nil {
		_ = service.Close()
		return nil, err
	}

	return service, nil
}

func configureArchiveJobs(
	ctx context.Context,
	service *Collections,
	options Options,
) error {
	if err := jobs.Bootstrap(ctx, service.client.DB()); err != nil {
		return fmt.Errorf("bootstrap collections archive jobs: %w", err)
	}

	var archive jobs.Archive
	if options.Archive != nil {
		archive = archiveadapter.ToQueue(options.Archive)
	}
	if options.ArchiveS3 != nil {
		if archive != nil || options.ArchiveDirectory != "" {
			return fmt.Errorf("ArchiveS3 conflicts with Archive or ArchiveDirectory")
		}
		cfg := *options.ArchiveS3
		if cfg.Prefix == "" {
			cfg.Prefix = "collections/importexport/"
		}
		var err error
		archive, err = jobs.NewS3Archive(ctx, cfg)
		if err != nil {
			return err
		}
	}

	if archive == nil {
		directory := options.ArchiveDirectory
		if directory == "" {
			binaryPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve collections archive directory: %w", err)
			}

			directory = filepath.Join(
				filepath.Dir(binaryPath),
				"collections",
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
		archiveadapter.FromQueue(archive),
	); err != nil {
		return fmt.Errorf("configure collections archive jobs: %w", err)
	}

	return nil
}

func (p *Collections) startWorkers() {
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

func newCollections(
	ctx context.Context,
	db *sqlwrap.Client,
	ownsClient bool,
	options Options,
) *Collections {
	rootCtx, cancel := context.WithCancel(contextutil.Normalize(ctx))
	repositoryOptions := repository.Options{
		QueryTimeout:             options.QueryTimeout,
		CacheL1Delay:             options.CacheL1Delay,
		CacheL2Delay:             options.CacheL2Delay,
		CatalogCacheTTL:          options.CatalogCacheTTL,
		OnCacheInvalidationError: options.OnCacheInvalidationError,
	}

	return &Collections{
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
		Internal:   internalapi.NewWithRepositoryOptions(rootCtx, db, repositoryOptions),
		callbacks:  callbackutil.NewWithTable(db.DB(), "collections_clb_event"),
		client:     db,
		ownsClient: ownsClient,
		rootCtx:    rootCtx,
		rootCancel: cancel,
		goroutines: goroutinemanager.New(),
		closeDone:  make(chan struct{}),
	}
}

func (p *Collections) Close() error {
	if p == nil {
		return nil
	}
	p.lifecycleMu.Lock()
	if p.closed {
		done := p.closeDone
		p.lifecycleMu.Unlock()
		<-done
		return p.closeErr
	}
	p.closed = true
	if p.closeDone == nil {
		p.closeDone = make(chan struct{})
	}
	startupDone := p.startupDone
	startupCancel := p.startupCancel
	p.lifecycleMu.Unlock()

	if p.rootCancel != nil {
		p.rootCancel()
	}
	if startupCancel != nil {
		startupCancel()
	}
	if startupDone != nil {
		<-startupDone
	}
	if p.activeCancel != nil {
		p.activeCancel()
	}
	p.goroutines.Close()
	p.callbackRuns.Wait()
	var err error
	if p.Admin != nil {
		err = errors.Join(err, p.Admin.Close())
	}
	if p.User != nil {
		err = errors.Join(err, p.User.Close())
	}
	if p.Internal != nil {
		err = errors.Join(err, p.Internal.Close())
	}
	if p.callbacks != nil {
		err = errors.Join(err, p.callbacks.Close())
	}
	if p.client != nil {
		if p.ownsClient {
			err = errors.Join(err, p.client.Close())
		} else {
			p.client.CloseLocalCache()
		}
	}
	p.closeErr = err
	close(p.closeDone)
	return err
}

// IsReady reports whether the service is initialized and its lifecycle is active.
func (p *Collections) IsReady() bool {
	if p == nil {
		return false
	}

	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	return !p.closed && p.rootCtx != nil && p.rootCtx.Err() == nil &&
		!p.client.IsUnavailable() &&
		p.Admin != nil &&
		p.User != nil && p.Internal != nil
}

func (p *Collections) bindContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	if p == nil {
		return contextutil.Merge(context.Background(), ctx)
	}

	return contextutil.Merge(p.rootCtx, ctx)
}
