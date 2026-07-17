package user

import (
	"context"
	"time"

	"github.com/elum2b/services/internal/utils/contextutil"
	goroutinemanager "github.com/elum2b/services/internal/utils/goroutine"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/tasks/repository"
	taskruntime "github.com/elum2b/services/tasks/runtime"
)

type User struct {
	rootCtx                   context.Context
	rootCancel                context.CancelFunc
	repository                *repository.Repository
	runtime                   *taskruntime.Manager
	providers                 map[string]PartnerProvider
	goroutines                *goroutinemanager.Manager
	ownsGoroutines            bool
	partnerStartLeaseDuration time.Duration
}

type Options struct {
	RepositoryOptions         repository.Options
	PartnerProviders          map[string]PartnerProvider
	Runtime                   *taskruntime.Manager
	Goroutines                *goroutinemanager.Manager
	PartnerStartLeaseDuration time.Duration
}

func New(ctx context.Context, db *sqlwrap.Client) *User {
	return newUser(
		ctx,
		repository.New(db),
		Options{},
	)
}

func NewWithOptions(ctx context.Context, db *sqlwrap.Client, options repository.Options) *User {
	return newUser(
		ctx,
		repository.NewWithOptions(db, options),
		Options{},
	)
}

func NewWithServiceOptions(ctx context.Context, db *sqlwrap.Client, options Options) *User {
	return newUser(
		ctx,
		repository.NewWithOptions(db, options.RepositoryOptions),
		options,
	)
}

func newUser(
	ctx context.Context,
	repo *repository.Repository,
	options Options,
) *User {

	rootCtx, rootCancel := context.WithCancel(contextutil.Normalize(ctx))
	manager := options.Goroutines
	ownsGoroutines := false
	if manager == nil {
		manager = goroutinemanager.New()
		ownsGoroutines = true
	}

	return &User{
		rootCtx:                   rootCtx,
		rootCancel:                rootCancel,
		repository:                repo,
		runtime:                   options.Runtime,
		providers:                 defaultPartnerProviders(options.PartnerProviders),
		goroutines:                manager,
		ownsGoroutines:            ownsGoroutines,
		partnerStartLeaseDuration: normalizePartnerStartLeaseDuration(options.PartnerStartLeaseDuration),
	}
}

func (u *User) Close() error {
	if u == nil {
		return nil
	}
	if u.rootCancel != nil {
		u.rootCancel()
	}
	if u.ownsGoroutines && u.goroutines != nil {
		u.goroutines.Close()
	}
	if u.repository == nil {
		return nil
	}

	return u.repository.Close()
}

func (u *User) withContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if u == nil {
		return contextutil.Merge(context.Background(), ctx)
	}
	return contextutil.Merge(u.rootCtx, ctx)
}

func clonePartnerProviders(values map[string]PartnerProvider) map[string]PartnerProvider {
	result := make(map[string]PartnerProvider, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func (u *User) partnerProvider(provider string) PartnerProvider {
	if u == nil {
		return nil
	}
	if value := u.providers[provider]; value != nil {
		return value
	}
	if u.runtime != nil {
		return LuaProvider{Runtime: u.runtime, Provider: provider}
	}
	return nil
}

func defaultPartnerProviders(overrides map[string]PartnerProvider) map[string]PartnerProvider {
	result := make(map[string]PartnerProvider, len(overrides))
	for key, value := range overrides {
		if value == nil {
			delete(result, key)
			continue
		}
		result[key] = value
	}
	return result
}
