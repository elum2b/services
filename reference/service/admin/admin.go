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
	repository           *repository.Repository
	rootCtx              context.Context
	store                resourcestorage.Store
	jobs                 *jobs.Manager
	archiveImportTimeout time.Duration
}

func NewWithRepositoryOptionsAndStore(
	ctx context.Context,
	db *sqlwrap.Client,
	options repository.Options,
	store resourcestorage.Store,
) *Admin {
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

func (a *Admin) QueueArchiveExport(
	ctx context.Context,
	params QueueArchiveExportParams,
) (ArchiveJob, error) {
	ctx, cancel := a.withContext(ctx)
	defer cancel()

	if a == nil || a.jobs == nil {
		return ArchiveJob{}, ErrArchiveJobsNotConfigured
	}

	options, err := json.Marshal(
		ArchiveExportRequest{IncludeMedia: params.IncludeMedia},
	)
	if err != nil {
		return ArchiveJob{}, err
	}

	job, err := a.jobs.QueueExport(
		ctx,
		jobs.QueueExportParams{
			Service:     "reference",
			WorkspaceID: params.WorkspaceID,
			FileName:    params.FileName,
			Options:     options,
		},
	)
	return publicArchiveJob(job), err

}

func (a *Admin) QueueArchiveImport(
	ctx context.Context,
	params QueueArchiveImportParams,
) (ArchiveJob, error) {
	ctx, cancel := a.withContext(ctx)
	defer cancel()

	if a == nil || a.jobs == nil {
		return ArchiveJob{}, ErrArchiveJobsNotConfigured
	}

	options, err := json.Marshal(
		ArchiveImportRequest{
			IncludeMedia:     params.IncludeMedia,
			ConflictStrategy: params.ConflictStrategy,
		},
	)
	if err != nil {
		return ArchiveJob{}, err
	}

	job, err := a.jobs.QueueImport(
		ctx,
		jobs.QueueImportParams{
			Service:     "reference",
			WorkspaceID: params.WorkspaceID,
			FileName:    params.FileName,
			Options:     options,
			Dump:        params.Archive,
		},
	)
	return publicArchiveJob(job), err

}

func (a *Admin) ArchiveJob(
	ctx context.Context,
	workspaceID string,
	id int64,
) (ArchiveJob, error) {
	ctx, cancel := a.withContext(ctx)
	defer cancel()

	if a == nil || a.jobs == nil {
		return ArchiveJob{}, ErrArchiveJobsNotConfigured
	}

	job, err := a.jobs.Status(
		ctx,
		jobs.StatusParams{
			Service:     "reference",
			WorkspaceID: workspaceID,
			ID:          id,
		},
	)
	return publicArchiveJob(job), err

}

func (a *Admin) ArchiveHistory(
	ctx context.Context,
	workspaceID string,
	page Page,
) ([]ArchiveJob, error) {
	ctx, cancel := a.withContext(ctx)
	defer cancel()

	if a == nil || a.jobs == nil {
		return nil, ErrArchiveJobsNotConfigured
	}

	limit, offset := normalizePage(page)

	values, err := a.jobs.History(
		ctx,
		jobs.HistoryParams{
			Service:     "reference",
			WorkspaceID: workspaceID,
			Limit:       limit,
			Offset:      offset,
		},
	)
	if err != nil {
		return nil, err
	}
	result := make([]ArchiveJob, 0, len(values))
	for _, job := range values {
		result = append(result, publicArchiveJob(job))
	}
	return result, nil

}

func (a *Admin) DownloadArchive(
	ctx context.Context,
	workspaceID string,
	id int64,
) (io.ReadCloser, ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return nil, ArchiveJob{}, ErrArchiveJobsNotConfigured
	}

	mergedCtx, cancel := a.withContext(ctx)
	reader, job, err := a.jobs.Download(mergedCtx, jobs.DownloadParams{Service: "reference", WorkspaceID: workspaceID, ID: id})
	if err != nil {
		cancel()
		return nil, publicArchiveJob(job), err
	}
	return &archiveReader{ReadCloser: reader, ctx: mergedCtx, cancel: cancel}, publicArchiveJob(job), nil

}

func (a *Admin) ArchiveJobHistory(
	ctx context.Context,
	workspaceID string,
	id int64,
	page Page,
) ([]ArchiveJobHistoryEntry, error) {
	ctx, cancel := a.withContext(ctx)
	defer cancel()

	if a == nil || a.jobs == nil {
		return nil, ErrArchiveJobsNotConfigured
	}

	limit, offset := normalizePage(page)

	values, err := a.jobs.JobHistory(
		ctx,
		jobs.JobHistoryParams{
			Service:     "reference",
			WorkspaceID: workspaceID,
			ID:          id,
			Limit:       limit,
			Offset:      offset,
		},
	)
	for i := range values {
		if values[i].Message != "" {
			values[i].Message = "Archive state updated"
		}
	}
	return values, err

}

func (a *Admin) withContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	if a == nil {
		return contextutil.Merge(nil, ctx)
	}
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

type archiveReader struct {
	io.ReadCloser
	ctx    context.Context
	cancel context.CancelFunc
}

func (r *archiveReader) Close() error { defer r.cancel(); return r.ReadCloser.Close() }

func (r *archiveReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.ReadCloser.Read(p)
}

func publicArchiveJob(job jobs.Job) ArchiveJob {
	message := ""
	if job.Error != "" {
		message = "Archive operation failed; inspect server diagnostics"
	}
	return ArchiveJob{
		ID:             job.ID,
		Service:        job.Service,
		WorkspaceID:    job.WorkspaceID,
		Type:           job.Type,
		Status:         job.Status,
		FileName:       job.FileName,
		ArchiveExpires: job.ArchiveExpires,
		Error:          message,
		CreatedAt:      job.CreatedAt,
		StartedAt:      job.StartedAt,
		FinishedAt:     job.FinishedAt,
		UpdatedAt:      job.UpdatedAt,
		Attempt:        job.Attempt,
		NextAttemptAt:  job.NextAttemptAt,
	}
}
