package admin

import (
	"context"
	"fmt"
	"io"
	"time"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/internal/utils/importexport/jobs"
)

const archiveManifestName = "manifest.json"

type archiveJobHandler struct{ admin *Admin }

func (h archiveJobHandler) ReconcileImport(ctx context.Context, job jobs.Job) error {
	return h.admin.repository.ReconcileArchiveImport(ctx, job.WorkspaceID)
}

func (h archiveJobHandler) Export(
	ctx context.Context,
	job jobs.Job,
) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var request ExportRequest

	if err := json.Unmarshal(job.Options, &request); err != nil {
		return nil, fmt.Errorf("decode export job options: %w", err)
	}

	pkg, err := h.admin.repository.Export(ctx, job.WorkspaceID, request)
	if err != nil {
		return nil, err
	}

	return jobs.WriteManifestZIP(ctx, archiveManifestName, int64(32<<20), func(w io.Writer) error {
		return json.NewEncoder(w).Encode(pkg)
	}, func(reader io.Reader) error { return jobs.ValidateJSONShape(ctx, reader, 100000) })
}

func (h archiveJobHandler) Import(
	ctx context.Context,
	job jobs.Job,
	source io.Reader,
) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var request ImportRequest

	if err := json.Unmarshal(job.Options, &request); err != nil {
		return fmt.Errorf("decode import job options: %w", err)
	}

	err := jobs.ReadManifestZIP(ctx, source, archiveManifestName, jobs.ZIPLimits{MaxUncompressedBytes: int64(32 << 20)}, func(reader io.Reader) error {
		decoder := json.NewDecoder(reader)
		if err := decoder.Decode(&request.Package); err != nil {
			return err
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return fmt.Errorf("manifest must contain exactly one JSON document")
		}
		return nil
	}, func(reader io.Reader) error {
		return jobs.ValidateJSONShape(ctx, reader, 100000)
	})
	if err != nil {
		return err
	}

	_, err = h.admin.repository.ImportJob(ctx, job.WorkspaceID, job.ID, request)

	return err
}

func (a *Admin) QueueArchiveExport(
	ctx context.Context,
	params QueueArchiveExportParams,
) (ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return ArchiveJob{}, ErrArchiveJobsNotConfigured
	}
	ctx, cancel := a.withContext(ctx)
	defer cancel()

	options, err := json.Marshal(params.ExportRequest)
	if err != nil {
		return ArchiveJob{}, err
	}

	job, err := a.jobs.QueueExport(
		ctx,
		jobs.QueueExportParams{
			Service:     "promo",
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
	if a == nil || a.jobs == nil {
		return ArchiveJob{}, ErrArchiveJobsNotConfigured
	}
	ctx, cancel := a.withContext(ctx)
	defer cancel()

	options, err := json.Marshal(struct {
		ConflictStrategy string `json:"conflict_strategy,omitempty"`
	}{ConflictStrategy: params.ConflictStrategy})
	if err != nil {
		return ArchiveJob{}, err
	}

	job, err := a.jobs.QueueImport(
		ctx,
		jobs.QueueImportParams{
			Service:      "promo",
			WorkspaceID:  params.WorkspaceID,
			FileName:     params.FileName,
			ManifestName: archiveManifestName,
			Options:      options,
			Dump:         params.Archive,
		},
	)
	return publicArchiveJob(job), err
}

func (a *Admin) ArchiveJob(
	ctx context.Context,
	workspaceID string,
	id int64,
) (ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return ArchiveJob{}, ErrArchiveJobsNotConfigured
	}
	ctx, cancel := a.withContext(ctx)
	defer cancel()

	job, err := a.jobs.Status(
		ctx,
		jobs.StatusParams{Service: "promo", WorkspaceID: workspaceID, ID: id},
	)
	return publicArchiveJob(job), err
}

func (a *Admin) ArchiveHistory(
	ctx context.Context,
	workspaceID string,
	page Page,
) ([]ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return nil, ErrArchiveJobsNotConfigured
	}
	ctx, cancel := a.withContext(ctx)
	defer cancel()

	limit, offset := normalizePage(page)

	values, err := a.jobs.History(
		ctx,
		jobs.HistoryParams{
			Service:     "promo",
			WorkspaceID: workspaceID,
			Limit:       limit,
			Offset:      offset,
		},
	)
	result := make([]ArchiveJob, 0, len(values))
	for _, value := range values {
		result = append(result, publicArchiveJob(value))
	}
	return result, err
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
	dump, job, err := a.jobs.Download(
		mergedCtx,
		jobs.DownloadParams{Service: "promo", WorkspaceID: workspaceID, ID: id},
	)
	if err != nil {
		cancel()
		return nil, publicArchiveJob(job), err
	}
	return &archiveDownload{ReadCloser: dump, ctx: mergedCtx, cancel: cancel}, publicArchiveJob(job), nil

}

func (a *Admin) ArchiveJobHistory(
	ctx context.Context,
	workspaceID string,
	id int64,
	page Page,
) ([]ArchiveJobHistoryEntry, error) {
	if a == nil || a.jobs == nil {
		return nil, ErrArchiveJobsNotConfigured
	}
	ctx, cancel := a.withContext(ctx)
	defer cancel()

	limit, offset := normalizePage(page)

	values, err := a.jobs.JobHistory(
		ctx,
		jobs.JobHistoryParams{
			Service:     "promo",
			WorkspaceID: workspaceID,
			ID:          id,
			Limit:       limit,
			Offset:      offset,
		},
	)
	for i := range values {
		if values[i].Message != "" {
			values[i].Message = "Archive operation status changed"
		}
	}
	return values, err
}

type archiveDownload struct {
	io.ReadCloser
	ctx    context.Context
	cancel context.CancelFunc
}

func (r *archiveDownload) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.ReadCloser.Read(p)
}
func (r *archiveDownload) Close() error { r.cancel(); return r.ReadCloser.Close() }

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
