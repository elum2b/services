package reference

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	serviceerrors "github.com/elum2b/services/errors"
	"github.com/elum2b/services/internal/utils/contextutil"
	"github.com/elum2b/services/internal/utils/goroutine"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/reference/repository"
	"github.com/elum2b/services/reference/service/admin"
	resourceservice "github.com/elum2b/services/reference/service/resource"
	resourcecache "github.com/elum2b/services/reference/service/resource/cache"
	"github.com/elum2b/services/reference/service/user"
	resourcestorage "github.com/elum2b/services/reference/storage"
)

type Reference struct {
	Admin    *admin.Admin
	Resource *resourceservice.Resource
	User     *user.User

	client      *sqlwrap.Client
	storage     resourcestorage.Store
	mediaCache  *resourcecache.Cache
	ownsClient  bool
	rootCtx     context.Context
	rootCancel  context.CancelFunc
	workers     *goroutine.Manager
	gcInterval  time.Duration
	gcBatch     int32
	gcRetention time.Duration
	gcTrigger   chan struct{}

	lifecycleMu sync.Mutex
	running     bool
}

func New() *Reference {
	store, _ := resourcestorage.New(Options{}.ResourceStorage)

	return newReference(
		context.Background(),
		sqlwrap.NewUnavailable(),
		true,
		Options{},
		store,
	)
}

func NewWithDatabase(
	ctx context.Context,
	db *sql.DB,
	options Options,
) (*Reference, error) {
	store, err := resourcestorage.New(options.ResourceStorage)
	if err != nil {
		return nil, serviceerrors.Wrap(
			serviceerrors.CodeInvalidFields,
			"reference resource storage configuration is invalid",
			err,
		)
	}

	client, err := sqlwrap.New(db, toSQLWrapOptions(options))
	if err != nil {
		return nil, serviceerrors.Wrap(
			serviceerrors.CodeInternalError,
			"reference sql client initialization failed",
			err,
		)
	}

	service := newReference(ctx, client, false, options, store)
	if err := configureArchiveJobs(
		ctx,
		service,
		options.ResourceStorage,
	); err != nil {
		_ = service.Close()
		return nil, err
	}

	return service, nil
}

func (r *Reference) Run(ctx context.Context, params DatabaseParams) error {
	if r == nil {
		return ErrServiceNil
	}

	r.lifecycleMu.Lock()

	if r.running {
		r.lifecycleMu.Unlock()

		return ErrServiceRunning
	}

	r.running = true
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
	r.startWorkers()

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
		return nil, serviceerrors.Wrap(
			serviceerrors.CodeUnavailable,
			"reference database connection failed",
			err,
		)
	}

	client, err := sqlwrap.New(db, toSQLWrapOptions(params.Options))
	if err != nil {
		_ = db.Close()

		return nil, serviceerrors.Wrap(
			serviceerrors.CodeInternalError,
			"reference sql client initialization failed",
			err,
		)
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

		return nil, serviceerrors.Wrap(
			serviceerrors.CodeInternalError,
			"reference bootstrap failed",
			err,
		)
	}

	if err := bootstrap.Close(); err != nil {
		_ = client.Close()

		return nil, serviceerrors.Wrap(
			serviceerrors.CodeInternalError,
			"reference bootstrap shutdown failed",
			err,
		)
	}

	store, err := resourcestorage.New(params.Options.ResourceStorage)
	if err != nil {
		_ = client.Close()

		return nil, serviceerrors.Wrap(
			serviceerrors.CodeInvalidFields,
			"reference resource storage configuration is invalid",
			err,
		)
	}

	service := newReference(ctx, client, true, params.Options, store)
	if err := configureArchiveJobs(
		ctx,
		service,
		params.Options.ResourceStorage,
	); err != nil {
		_ = service.Close()
		return nil, err
	}

	return service, nil
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

func (r *Reference) adopt(running *Reference) {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	r.Admin, r.User = running.Admin, running.User
	r.Resource = running.Resource
	r.client, r.storage, r.mediaCache, r.ownsClient = running.client, running.storage, running.mediaCache, running.ownsClient
	r.rootCtx, r.rootCancel = running.rootCtx, running.rootCancel

	if r.workers != nil {
		r.workers.Close()
	}

	r.workers, r.gcInterval, r.gcBatch, r.gcRetention, r.gcTrigger = running.workers, running.gcInterval, running.gcBatch, running.gcRetention, running.gcTrigger
}

func newReference(
	ctx context.Context,
	db *sqlwrap.Client,
	ownsClient bool,
	options Options,
	store resourcestorage.Store,
) *Reference {
	rootCtx, cancel := context.WithCancel(contextutil.Normalize(ctx))
	repositoryOptions := repository.Options{
		QueryTimeout:             options.QueryTimeout,
		CacheL1Delay:             options.CacheL1Delay,
		CacheL2Delay:             options.CacheL2Delay,
		OnCacheInvalidationError: options.OnCacheInvalidationError,
	}
	mediaCache := resourcecache.New(options.ResourceMediaCache)
	gcInterval := options.ResourceGCInterval

	if gcInterval <= 0 {
		gcInterval = time.Minute
	}

	gcBatch := options.ResourceGCBatch
	if gcBatch <= 0 {
		gcBatch = 100
	}

	gcRetention := options.ResourceGCRetention
	if gcRetention <= 0 {
		gcRetention = time.Hour
	}

	gcTrigger := make(chan struct{}, 1)

	return &Reference{
		Admin: admin.NewWithRepositoryOptionsAndStore(
			rootCtx,
			db,
			repositoryOptions,
			store,
		),
		Resource: resourceservice.New(
			rootCtx,
			db,
			repositoryOptions,
			store,
			mediaCache,
			gcTrigger,
			gcRetention,
		),
		User: user.NewWithRepositoryOptions(
			rootCtx,
			db,
			repositoryOptions,
		),
		client:      db,
		storage:     store,
		mediaCache:  mediaCache,
		ownsClient:  ownsClient,
		rootCtx:     rootCtx,
		rootCancel:  cancel,
		workers:     goroutine.New(),
		gcInterval:  gcInterval,
		gcBatch:     gcBatch,
		gcRetention: gcRetention,
		gcTrigger:   gcTrigger,
	}
}

func (r *Reference) startWorkers() {
	if r == nil || r.workers == nil || r.Resource == nil {
		return
	}

	if r.Admin != nil {
		r.Admin.StartArchiveJobs(r.rootCtx)
	}

	r.workers.Go("reference-resource-gc", func() {
		ticker := time.NewTicker(r.gcInterval)
		defer ticker.Stop()

		for {
			if _, err := r.Resource.CollectGarbage(
				r.rootCtx,
				resourceservice.CollectGarbageParams{
					Limit: r.gcBatch,
				},
			); err != nil &&
				r.rootCtx.Err() == nil {
				log.Printf("reference resource GC: %v", err)
			}

			select {
			case <-r.rootCtx.Done():
				return
			case <-ticker.C:
			case <-r.gcTrigger:
			}
		}
	})
}

func (r *Reference) Close() error {
	if r == nil {
		return nil
	}

	if r.rootCancel != nil {
		r.rootCancel()
	}

	if r.workers != nil {
		r.workers.Close()
	}

	var err error

	if r.Admin != nil {
		err = errors.Join(err, r.Admin.Close())
	}

	if r.User != nil {
		err = errors.Join(err, r.User.Close())
	}

	if r.Resource != nil {
		err = errors.Join(err, r.Resource.Close())
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

	return r.rootCtx != nil && r.rootCtx.Err() == nil &&
		!r.client.IsUnavailable() &&
		r.storage != nil &&
		r.Admin != nil &&
		r.User != nil &&
		r.Resource != nil
}
