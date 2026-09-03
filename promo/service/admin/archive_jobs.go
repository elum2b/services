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
	var request ImportRequest

	if err := json.Unmarshal(job.Options, &request); err != nil {
		return fmt.Errorf("decode import job options: %w", err)
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

	return a.jobs.QueueExport(
		ctx,
		jobs.QueueExportParams{
			Service:     "promo",
			WorkspaceID: params.WorkspaceID,
			FileName:    params.FileName,
			Options:     options,
		},
	)
}

func (a *Admin) QueueArchiveImport(
	ctx context.Context,
	params QueueArchiveImportParams,
) (ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return jobs.Job{}, ErrArchiveJobsNotConfigured
	}

	options, err := json.Marshal(params.ImportRequest)
	if err != nil {
		return jobs.Job{}, err
	}

	return a.jobs.QueueImport(
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
}

func (a *Admin) ArchiveJob(
	ctx context.Context,
	workspaceID string,
	id int64,
) (ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return jobs.Job{}, ErrArchiveJobsNotConfigured
	}

	return a.jobs.Status(
		ctx,
		jobs.StatusParams{Service: "promo", WorkspaceID: workspaceID, ID: id},
	)
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

	return a.jobs.History(
		ctx,
		jobs.HistoryParams{
			Service:     "promo",
			WorkspaceID: workspaceID,
			Limit:       limit,
			Offset:      offset,
		},
	)
}

func (a *Admin) DownloadArchive(
	ctx context.Context,
	workspaceID string,
	id int64,
) (io.ReadCloser, ArchiveJob, error) {
	if a == nil || a.jobs == nil {
		return nil, jobs.Job{}, ErrArchiveJobsNotConfigured
	}

	return a.jobs.Download(
		ctx,
		jobs.DownloadParams{Service: "promo", WorkspaceID: workspaceID, ID: id},
	)
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
			Service:     "promo",
			WorkspaceID: workspaceID,
			ID:          id,
			Limit:       limit,
			Offset:      offset,
		},
	)
}
