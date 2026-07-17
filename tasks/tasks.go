package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	serviceerrors "github.com/elum2b/services/errors"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	"github.com/elum2b/services/internal/utils/contextutil"
	goroutinemanager "github.com/elum2b/services/internal/utils/goroutine"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/tasks/repository"
	taskruntime "github.com/elum2b/services/tasks/runtime"
	"github.com/elum2b/services/tasks/service/admin"
	"github.com/elum2b/services/tasks/service/integration"
	"github.com/elum2b/services/tasks/service/internalapi"
	"github.com/elum2b/services/tasks/service/user"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Tasks struct {
	Admin       *admin.Admin
	Internal    *internalapi.Internal
	Integration *integration.Integration
	User        *user.User

	callbacks  *callbackutil.Store
	runtime    *taskruntime.Manager
	client     *sqlwrap.Client
	ownsClient bool
	rootCtx    context.Context
	rootCancel context.CancelFunc
	goroutines *goroutinemanager.Manager

	lifecycleMu    sync.Mutex
	params         DatabaseParams
	options        Options
	callbacksToRun []callbackRegistration
	running        bool
}

func New(params DatabaseParams) *Tasks {
	return &Tasks{params: params, options: params.Options}
}

func NewWithDatabase(ctx context.Context, db *sql.DB, options Options) (*Tasks, error) {
	client, err := sqlwrap.New(db, toSQLWrapOptions(options))
	if err != nil {
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, "tasks sql client initialization failed", err)
	}
	service := newTasks(ctx, client, false, options)
	_ = service.SyncPartners(ctx)
	return service, nil
}

func (t *Tasks) Run(ctx context.Context) error {
	if t == nil {
		return ErrServiceNil
	}
	t.lifecycleMu.Lock()
	if t.running {
		t.lifecycleMu.Unlock()
		return ErrServiceRunning
	}
	t.running = true
	params := t.params
	registrations := append([]callbackRegistration(nil), t.callbacksToRun...)
	t.lifecycleMu.Unlock()

	running, err := open(ctx, params)
	if err != nil {
		t.lifecycleMu.Lock()
		t.running = false
		t.lifecycleMu.Unlock()
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return nil
		}
		return wrapLifecycleError(err)
	}
	t.adopt(running)
	defer t.Close()

	errCh := make(chan error, len(registrations))
	for _, registration := range registrations {
		registration := registration
		t.goroutines.Go("tasks.callback", func() {
			errCh <- t.runCallback(registration.ctx, registration.handler, registration.options...)
		})
	}
	select {
	case <-t.rootCtx.Done():
		return nil
	case err := <-errCh:
		if errors.Is(err, context.Canceled) && t.rootCtx.Err() != nil {
			return nil
		}
		return wrapLifecycleError(err)
	}
}

func open(ctx context.Context, params DatabaseParams) (*Tasks, error) {
	if params.User == "" || params.Database == "" {
		return nil, ErrDatabaseConfigRequired
	}
	db, err := openPostgres(ctx, params)
	if err != nil {
		return nil, serviceerrors.Wrap(serviceerrors.CodeUnavailable, "tasks database connection failed", err)
	}
	client, err := sqlwrap.New(db, toSQLWrapOptions(params.Options))
	if err != nil {
		_ = db.Close()
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, "tasks sql client initialization failed", err)
	}
	bootstrap := repository.NewWithOptions(client, repositoryOptions(params.Options))
	if err := bootstrap.Bootstrap(ctx); err != nil {
		_ = bootstrap.Close()
		_ = client.Close()
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, "tasks bootstrap failed", err)
	}
	if err := bootstrap.Close(); err != nil {
		_ = client.Close()
		return nil, serviceerrors.Wrap(serviceerrors.CodeInternalError, "tasks bootstrap shutdown failed", err)
	}
	service := newTasks(ctx, client, true, params.Options)
	_ = service.SyncPartners(ctx)
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

func (t *Tasks) adopt(running *Tasks) {
	t.lifecycleMu.Lock()
	defer t.lifecycleMu.Unlock()
	t.Admin, t.Internal, t.Integration, t.User = running.Admin, running.Internal, running.Integration, running.User
	t.callbacks, t.runtime, t.client, t.ownsClient = running.callbacks, running.runtime, running.client, running.ownsClient
	t.rootCtx, t.rootCancel = running.rootCtx, running.rootCancel
	t.goroutines = running.goroutines
	t.options = running.options
}

func newTasks(ctx context.Context, db *sqlwrap.Client, ownsClient bool, options Options) *Tasks {
	rootCtx, cancel := context.WithCancel(contextutil.Normalize(ctx))
	repositoryOptions := repositoryOptions(options)
	goroutines := goroutinemanager.New()
	runtimeOptions := options.Runtime
	if runtimeOptions.ScriptLoader == nil {
		runtimeOptions.ScriptLoader = partnerScriptLoader(db, repositoryOptions)
	}
	runtimeManager := taskruntime.New(rootCtx, runtimeOptions)

	return &Tasks{
		Admin: admin.NewWithOptions(rootCtx, db, repositoryOptions),
		Internal: internalapi.NewWithServiceOptions(rootCtx, db, internalapi.Options{
			RepositoryOptions: repositoryOptions,
			Runtime:           runtimeManager,
		}),
		Integration: integration.NewWithOptions(rootCtx, db, integrationOptions(options, repositoryOptions)),
		User: user.NewWithServiceOptions(rootCtx, db, user.Options{
			RepositoryOptions:         repositoryOptions,
			PartnerProviders:          options.PartnerProviders,
			Runtime:                   runtimeManager,
			Goroutines:                goroutines,
			PartnerStartLeaseDuration: options.PartnerStartLeaseDuration,
		}),
		callbacks:  callbackutil.NewWithTable(db.DB(), callbackutil.TasksTable),
		runtime:    runtimeManager,
		client:     db,
		ownsClient: ownsClient,
		rootCtx:    rootCtx,
		rootCancel: cancel,
		options:    options,
		goroutines: goroutines,
	}
}

func partnerScriptLoader(db *sqlwrap.Client, options repository.Options) taskruntime.ScriptLoader {
	return func(ctx context.Context, provider string) (taskruntime.Script, bool, error) {
		repo := repository.NewWithOptions(db, options)
		defer func() { _ = repo.Close() }()
		script, found, err := repo.GetEnabledPartnerScript(ctx, provider)
		if err != nil || !found {
			return taskruntime.Script{}, found, err
		}
		return taskruntime.Script{Provider: script.Provider, Source: script.Source, Version: script.Version}, true, nil
	}
}

func integrationOptions(options Options, repositoryOptions repository.Options) integration.Options {
	result := options.Integration
	result.RepositoryOptions = repositoryOptions
	return result
}

func repositoryOptions(options Options) repository.Options {
	return repository.Options{
		QueryTimeout:             options.QueryTimeout,
		CacheL1Delay:             options.CacheL1Delay,
		CacheL2Delay:             options.CacheL2Delay,
		OnCacheInvalidationError: options.OnCacheInvalidationError,
	}
}

func (t *Tasks) Close() error {
	if t == nil {
		return nil
	}
	if t.rootCancel != nil {
		t.rootCancel()
	}
	if t.goroutines != nil {
		t.goroutines.Close()
	}
	var err error
	if t.Admin != nil {
		err = errors.Join(err, t.Admin.Close())
	}
	if t.Internal != nil {
		err = errors.Join(err, t.Internal.Close())
	}
	if t.Integration != nil {
		err = errors.Join(err, t.Integration.Close())
	}
	if t.User != nil {
		err = errors.Join(err, t.User.Close())
	}
	if t.runtime != nil {
		err = errors.Join(err, t.runtime.Close())
	}
	if t.callbacks != nil {
		err = errors.Join(err, t.callbacks.Close())
	}
	if t.ownsClient && t.client != nil {
		err = errors.Join(err, t.client.Close())
	}
	return err
}

func (t *Tasks) SyncPartners(ctx context.Context) error {
	if t == nil || t.client == nil {
		return nil
	}
	mergedCtx, cancel := t.bindContext(ctx)
	defer cancel()
	repo := repository.NewWithOptions(t.client, repositoryOptions(t.options))
	defer func() { _ = repo.Close() }()
	configs, err := repo.WarmPartnerConfigCache(mergedCtx)
	if err != nil {
		return err
	}
	if t.runtime == nil {
		return nil
	}
	providers := make([]string, 0, len(configs))
	for _, config := range configs {
		if config.IsEnabled {
			providers = append(providers, config.Provider)
		}
	}
	return t.runtime.WarmProviders(mergedCtx, providers)
}

// IsReady reports whether the service is initialized and its lifecycle is active.
func (t *Tasks) IsReady() bool {
	if t == nil {
		return false
	}
	t.lifecycleMu.Lock()
	defer t.lifecycleMu.Unlock()
	return t.rootCtx != nil && t.rootCtx.Err() == nil &&
		t.Admin != nil && t.Internal != nil && t.Integration != nil && t.User != nil
}

func (t *Tasks) bindContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if t == nil {
		return contextutil.Merge(context.Background(), ctx)
	}
	return contextutil.Merge(t.rootCtx, ctx)
}
