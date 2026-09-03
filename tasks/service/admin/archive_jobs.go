package admin

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/internal/utils/importexport/jobs"
)

const archiveManifestName = "manifest.json"

type archiveJobHandler struct{ admin *Admin }

// archiveImportOptions is persisted in a generic job, so its secret values are
// encrypted before queueing and decrypted only by this Tasks import handler.
type archiveImportOptions struct {
	ConflictStrategy string            `json:"conflict_strategy"`
	Secrets          map[string]string `json:"secrets,omitempty"`
}

func (h archiveJobHandler) Export(
	ctx context.Context,
	job jobs.Job,
) (io.ReadCloser, error) {
	var request ExportRequest

	if err := json.Unmarshal(job.Options, &request); err != nil {
		return nil, fmt.Errorf("decode export job options: %w", err)
	}

	pkg, err := h.admin.repository.Export(ctx, job.WorkspaceID, request)
	if err != nil {
		return nil, err
	}

	var data bytes.Buffer

	writer := zip.NewWriter(&data)

	manifest, err := writer.Create(archiveManifestName)
	if err == nil {
		err = json.NewEncoder(manifest).Encode(pkg)
	}

	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		return nil, fmt.Errorf("write export ZIP: %w", err)
	}

	return io.NopCloser(bytes.NewReader(data.Bytes())), nil
}

func (h archiveJobHandler) Import(
	ctx context.Context,
	job jobs.Job,
	source io.Reader,
) error {
	var options archiveImportOptions

	if err := json.Unmarshal(job.Options, &options); err != nil {
		return fmt.Errorf("decode import job options: %w", err)
	}

	secrets, err := h.admin.repository.DecryptImportSecrets(options.Secrets)
	if err != nil {
		return fmt.Errorf("decrypt import job secrets: %w", err)
	}

	request := ImportRequest{
		ConflictStrategy: options.ConflictStrategy,
		Secrets:          secrets,
	}

	data, err := io.ReadAll(source)
	if err != nil {
		return fmt.Errorf("read import ZIP: %w", err)
	}

	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open import ZIP: %w", err)
	}

	if len(archive.File) != 1 || archive.File[0].Name != archiveManifestName {
		return fmt.Errorf(
			"import ZIP must contain exactly one %s",
			archiveManifestName,
		)
	}

	reader, err := archive.File[0].Open()
	if err != nil {
		return err
	}
	defer reader.Close()

	if err := json.NewDecoder(reader).Decode(&request.Package); err != nil {
		return fmt.Errorf("decode import manifest: %w", err)
	}

	_, err = h.admin.repository.ImportJob(ctx, job.WorkspaceID, job.ID, request)

	return err
}

func (a *Admin) QueueArchiveExport(
	ctx context.Context,
	params QueueArchiveExportParams,
) (ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return jobs.Job{}, ErrArchiveJobsNotConfigured
	}

	options, err := json.Marshal(params.ExportRequest)
	if err != nil {
		return jobs.Job{}, err
	}

	job, err := a.jobs.QueueExport(
		ctx,
		jobs.QueueExportParams{
			Service:     "tasks",
			WorkspaceID: params.WorkspaceID,
			FileName:    params.FileName,
			Options:     options,
		},
	)
	if err != nil {
		return jobs.Job{}, err
	}

	return publicArchiveJob(job), nil
}

func (a *Admin) QueueArchiveImport(
	ctx context.Context,
	params QueueArchiveImportParams,
) (ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return jobs.Job{}, ErrArchiveJobsNotConfigured
	}

	secrets, err := a.repository.EncryptImportSecrets(params.Secrets)
	if err != nil {
		return jobs.Job{}, err
	}

	options, err := json.Marshal(archiveImportOptions{
		ConflictStrategy: params.ConflictStrategy,
		Secrets:          secrets,
	})
	if err != nil {
		return jobs.Job{}, err
	}

	job, err := a.jobs.QueueImport(
		ctx,
		jobs.QueueImportParams{
			Service:      "tasks",
			WorkspaceID:  params.WorkspaceID,
			FileName:     params.FileName,
			ManifestName: archiveManifestName,
			Options:      options,
			Dump:         params.Archive,
		},
	)
	if err != nil {
		return jobs.Job{}, err
	}

	return publicArchiveJob(job), nil
}

func (a *Admin) ArchiveJob(
	ctx context.Context,
	workspaceID string,
	id int64,
) (ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return jobs.Job{}, ErrArchiveJobsNotConfigured
	}

	job, err := a.jobs.Status(
		ctx,
		jobs.StatusParams{Service: "tasks", WorkspaceID: workspaceID, ID: id},
	)
	if err != nil {
		return jobs.Job{}, err
	}

	return publicArchiveJob(job), nil
}

func (a *Admin) ArchiveHistory(
	ctx context.Context,
	workspaceID string,
	page Page,
) ([]ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return nil, ErrArchiveJobsNotConfigured
	}

	limit, offset := normalizePage(page)

	items, err := a.jobs.History(
		ctx,
		jobs.HistoryParams{
			Service:     "tasks",
			WorkspaceID: workspaceID,
			Limit:       limit,
			Offset:      offset,
		},
	)
	if err != nil {
		return nil, err
	}

	for index := range items {
		items[index] = publicArchiveJob(items[index])
	}

	return items, nil
}

func (a *Admin) DownloadArchive(
	ctx context.Context,
	workspaceID string,
	id int64,
) (io.ReadCloser, ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return nil, jobs.Job{}, ErrArchiveJobsNotConfigured
	}

	job, err := a.jobs.Status(
		ctx,
		jobs.StatusParams{Service: "tasks", WorkspaceID: workspaceID, ID: id},
	)
	if err != nil {
		return nil, jobs.Job{}, err
	}

	if job.Type == jobs.TypeImport {
		return nil, jobs.Job{}, jobs.ErrArchiveNotReady
	}

	dump, job, err := a.jobs.Download(
		ctx,
		jobs.DownloadParams{Service: "tasks", WorkspaceID: workspaceID, ID: id},
	)
	if err != nil {
		return nil, jobs.Job{}, err
	}

	return dump, publicArchiveJob(job), nil
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

	limit, offset := normalizePage(page)

	return a.jobs.JobHistory(
		ctx,
		jobs.JobHistoryParams{
			Service:     "tasks",
			WorkspaceID: workspaceID,
			ID:          id,
			Limit:       limit,
			Offset:      offset,
		},
	)
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

// publicArchiveJob prevents asynchronous import options and worker internals
// from becoming a read channel for write-only partner secrets.
func publicArchiveJob(job jobs.Job) jobs.Job {
	job.Options = nil
	job.ArchiveKey = ""
	job.LockedBy = ""
	job.LeaseToken = ""
	job.LockedUntil = nil

	return job
}
