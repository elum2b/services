package admin

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/elum2b/services/collections/model"
	"github.com/elum2b/services/collections/repository"
	"strings"
	"unicode/utf8"

	serviceerrors "github.com/elum2b/services/errors"
	"github.com/elum2b/services/internal/utils/importexport/jobs"

	json "github.com/goccy/go-json"
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
			Service:     "collections",
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

	if strings.TrimSpace(params.ActorID) != params.ActorID || params.ActorID == "" || len(params.ActorID) > 191 || !utf8.ValidString(params.ActorID) || strings.ContainsRune(params.ActorID, 0) {
		return ArchiveJob{}, repository.ErrInvalid
	}
	options, err := json.Marshal(struct {
		ActorID          string                 `json:"actor_id"`
		ConflictStrategy model.ConflictStrategy `json:"conflict_strategy,omitempty"`
	}{ActorID: params.ActorID, ConflictStrategy: params.ConflictStrategy})
	if err != nil {
		return ArchiveJob{}, err
	}

	job, err := a.jobs.QueueImport(
		ctx,
		jobs.QueueImportParams{
			Service:      "collections",
			WorkspaceID:  params.WorkspaceID,
			FileName:     params.FileName,
			ManifestName: archiveManifestName,
			Options:      options,
			Dump:         params.Archive,
		},
	)
	return publicArchiveJob(job), err
}

func (a *Admin) ArchiveJob(ctx context.Context, p ArchiveStatusParams) (ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return ArchiveJob{}, ErrArchiveJobsNotConfigured
	}
	ctx, cancel := a.withContext(ctx)
	defer cancel()

	job, err := a.jobs.Status(
		ctx,
		jobs.StatusParams{
			Service:     "collections",
			WorkspaceID: p.WorkspaceID,
			ID:          p.ID,
		},
	)
	return publicArchiveJob(job), err
}

func (a *Admin) ArchiveHistory(ctx context.Context, p ArchiveHistoryParams) ([]ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return nil, ErrArchiveJobsNotConfigured
	}
	ctx, cancel := a.withContext(ctx)
	defer cancel()

	limit, offset := normalizePage(p.Page)

	values, err := a.jobs.History(
		ctx,
		jobs.HistoryParams{
			Service:     "collections",
			WorkspaceID: p.WorkspaceID,
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

func (a *Admin) DownloadArchive(ctx context.Context, p ArchiveStatusParams) (io.ReadCloser, ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return nil, ArchiveJob{}, ErrArchiveJobsNotConfigured
	}

	mergedCtx, cancel := a.withContext(ctx)
	dump, job, err := a.jobs.Download(
		mergedCtx,
		jobs.DownloadParams{
			Service:     "collections",
			WorkspaceID: p.WorkspaceID,
			ID:          p.ID,
		},
	)
	if err != nil {
		cancel()
		return nil, publicArchiveJob(job), err
	}
	return &archiveDownload{
		ReadCloser: dump,
		ctx:        mergedCtx,
		cancel:     cancel,
	}, publicArchiveJob(job), nil

}

func (a *Admin) ArchiveJobHistory(ctx context.Context, p ArchiveHistoryParams) ([]ArchiveJobHistoryEntry, error) {
	if a == nil || a.jobs == nil {
		return nil, ErrArchiveJobsNotConfigured
	}
	ctx, cancel := a.withContext(ctx)
	defer cancel()

	limit, offset := normalizePage(p.Page)

	values, err := a.jobs.JobHistory(
		ctx,
		jobs.JobHistoryParams{
			Service:     "collections",
			WorkspaceID: p.WorkspaceID,
			ID:          p.ID,
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

var ErrArchiveJobsNotConfigured = serviceerrors.New(serviceerrors.CodeNotReady, "collections archive jobs not configured")

type ArchiveStatusParams struct {
	WorkspaceID string
	ID          int64
}
type ArchiveHistoryParams struct {
	WorkspaceID string
	ID          int64
	Page        Page
}
type ExportParams struct {
	WorkspaceID string
	Request     model.ExportRequest
}
type ImportParams struct {
	WorkspaceID string
	Request     model.ImportRequest
}

func (a *Admin) Export(ctx context.Context, p ExportParams) (model.ExportPackage, error) {
	ctx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.Export(ctx, p.WorkspaceID, p.Request)
}
func (a *Admin) PreviewImport(ctx context.Context, p ImportParams) (model.ImportPreview, error) {
	ctx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.PreviewImport(ctx, p.WorkspaceID, p.Request.Package)
}
func (a *Admin) Import(ctx context.Context, p ImportParams) (model.ImportResult, error) {
	ctx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.Import(ctx, p.WorkspaceID, p.Request)
}
