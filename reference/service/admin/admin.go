package admin

import (
	"context"
	"io"
	"time"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/internal/utils/contextutil"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/reference/repository"
	resourcestorage "github.com/elum2b/services/reference/storage"
)

type Admin struct {
	repository *repository.Repository
	rootCtx    context.Context
	store      resourcestorage.Store
	jobs       *jobs.Manager
}

func NewWithRepositoryOptionsAndStore(ctx context.Context, db *sqlwrap.Client, options repository.Options, store resourcestorage.Store) *Admin {
	admin := NewWithRepositoryOptions(ctx, db, options)
	admin.store = store
	return admin
}

func NewWithRepositoryOptions(
	ctx context.Context,
	db *sqlwrap.Client,
	options repository.Options,
) *Admin {
	repo, err := repository.NewPreparedWithOptions(
		contextutil.Normalize(ctx),
		db,
		options,
	)
	if err != nil {
		repo = repository.NewWithOptions(db, options)
	}

	return &Admin{repository: repo, rootCtx: contextutil.Normalize(ctx)}
}

func New(ctx context.Context, db *sqlwrap.Client) *Admin {
	return NewWithRepositoryOptions(ctx, db, repository.Options{
		CacheL1Delay: 10 * time.Minute, CacheL2Delay: 10 * time.Minute,
	})
}

func (a *Admin) Close() error {
	if a == nil {
		return nil
	}
	if a.jobs != nil {
		a.jobs.Close()
	}
	if a.repository != nil {
		return a.repository.Close()
	}
	return nil
}

func (a *Admin) configureArchiveJobs(manager *jobs.Manager) { a.jobs = manager }

func (a *Admin) QueueArchiveExport(ctx context.Context, params QueueArchiveExportParams) (ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return jobs.Job{}, ErrArchiveJobsNotConfigured
	}
	options, err := json.Marshal(ArchiveExportRequest{IncludeMedia: params.IncludeMedia})
	if err != nil {
		return jobs.Job{}, err
	}
	return a.jobs.QueueExport(ctx, jobs.QueueExportParams{Service: "reference", WorkspaceID: params.WorkspaceID, FileName: params.FileName, Options: options})
}

func (a *Admin) QueueArchiveImport(ctx context.Context, params QueueArchiveImportParams) (ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return jobs.Job{}, ErrArchiveJobsNotConfigured
	}
	options, err := json.Marshal(ArchiveImportRequest{IncludeMedia: params.IncludeMedia, ConflictStrategy: params.ConflictStrategy})
	if err != nil {
		return jobs.Job{}, err
	}
	return a.jobs.QueueImport(ctx, jobs.QueueImportParams{Service: "reference", WorkspaceID: params.WorkspaceID, FileName: params.FileName, Options: options, Dump: params.Archive})
}

func (a *Admin) ArchiveJob(ctx context.Context, workspaceID string, id int64) (ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return jobs.Job{}, ErrArchiveJobsNotConfigured
	}
	return a.jobs.Status(ctx, jobs.StatusParams{Service: "reference", WorkspaceID: workspaceID, ID: id})
}

func (a *Admin) ArchiveHistory(ctx context.Context, workspaceID string, page Page) ([]ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return nil, ErrArchiveJobsNotConfigured
	}
	limit, offset := normalizePage(page)
	return a.jobs.History(ctx, jobs.HistoryParams{Service: "reference", WorkspaceID: workspaceID, Limit: limit, Offset: offset})
}

func (a *Admin) DownloadArchive(ctx context.Context, workspaceID string, id int64) (io.ReadCloser, ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return nil, jobs.Job{}, ErrArchiveJobsNotConfigured
	}
	return a.jobs.Download(ctx, jobs.DownloadParams{Service: "reference", WorkspaceID: workspaceID, ID: id})
}

func (a *Admin) ArchiveJobHistory(ctx context.Context, workspaceID string, id int64, page Page) ([]ArchiveJobHistoryEntry, error) {
	if a == nil || a.jobs == nil {
		return nil, ErrArchiveJobsNotConfigured
	}
	limit, offset := normalizePage(page)
	return a.jobs.JobHistory(ctx, jobs.JobHistoryParams{Service: "reference", WorkspaceID: workspaceID, ID: id, Limit: limit, Offset: offset})
}

func (a *Admin) withContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	return contextutil.Merge(a.rootCtx, ctx)
}

func normalizePage(page Page) (int32, int32) {
	if page.Limit <= 0 {
		page.Limit = 100
	}

	if page.Limit > 1000 {
		page.Limit = 1000
	}

	if page.Offset < 0 {
		page.Offset = 0
	}

	return page.Limit, page.Offset
}
