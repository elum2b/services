package admin

import (
	"context"
	"database/sql"

	"github.com/elum2b/services/internal/utils/contextutil"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/tasks/repository"
)

type Admin struct {
	rootCtx    context.Context
	repository *repository.Repository
	jobs       *jobs.Manager
}

func New(ctx context.Context, db *sqlwrap.Client) *Admin {
	return &Admin{
		rootCtx:    contextutil.Normalize(ctx),
		repository: repository.New(db),
	}
}

func NewWithOptions(
	ctx context.Context,
	db *sqlwrap.Client,
	options repository.Options,
) *Admin {
	return &Admin{
		rootCtx:    contextutil.Normalize(ctx),
		repository: repository.NewWithOptions(db, options),
	}
}

func (a *Admin) Close() error {
	if a == nil {
		return nil
	}

	if a.jobs != nil {
		a.jobs.Close()
	}

	if a.repository == nil {
		return nil
	}

	return a.repository.Close()
}

func (a *Admin) configureArchiveJobs(manager *jobs.Manager) { a.jobs = manager }

// ConfigureArchiveJobs attaches the persistent async ZIP queue to this Admin.
// The caller must bootstrap the jobs table before invoking it.
func (a *Admin) ConfigureArchiveJobs(db *sql.DB, archive jobs.Archive) error {
	manager, err := jobs.New(
		db,
		archive,
		archiveJobHandler{admin: a},
		jobs.Options{},
	)
	if err != nil {
		return err
	}

	a.configureArchiveJobs(manager)

	return nil
}

func (a *Admin) StartArchiveJobs(ctx context.Context) bool {
	return a != nil && a.jobs != nil && a.jobs.Start(ctx)
}

func (a *Admin) withContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	if a == nil {
		return contextutil.Merge(context.Background(), ctx)
	}

	return contextutil.Merge(a.rootCtx, ctx)
}

func (a *Admin) Bootstrap(ctx context.Context) error {
	if a == nil || a.repository == nil {
		return ErrRepositoryNotConfigured
	}

	mergedCtx, cancel := a.withContext(ctx)

	defer cancel()

	return a.repository.Bootstrap(mergedCtx)
}
