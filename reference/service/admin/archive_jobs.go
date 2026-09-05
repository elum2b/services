package admin

import (
	"archive/zip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"time"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/internal/utils/importexport/jobs"
)

const (
	maxQueuedArchiveSize             = jobs.DefaultMaxUploadBytes
	maxQueuedArchiveUncompressedSize = 512 << 20
	maxQueuedArchiveFiles            = 10_000
)

// ConfigureArchiveJobs attaches the persistent async ZIP queue to this Admin.
// The caller must bootstrap the jobs table before invoking it.
func (a *Admin) ConfigureArchiveJobs(
	db *sql.DB,
	archive jobs.Archive,
	importTimeout, leaseTimeout time.Duration,
) error {
	if importTimeout > 0 {
		a.archiveImportTimeout = importTimeout
	}

	manager, err := jobs.New(
		db,
		archive,
		archiveJobHandler{admin: a},
		jobs.Options{Service: "reference", LeaseTimeout: leaseTimeout},
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

type archiveJobHandler struct{ admin *Admin }

func (h archiveJobHandler) Export(
	ctx context.Context,
	job jobs.Job,
) (io.ReadCloser, error) {
	var request ArchiveExportRequest

	if err := json.Unmarshal(job.Options, &request); err != nil {
		return nil, fmt.Errorf("decode export job options: %w", err)
	}

	file, err := jobs.NewStagingFile("importexport-reference-export-*.zip")
	if err != nil {
		return nil, err
	}
	if err = h.admin.exportZIP(ctx, job.WorkspaceID, request, file); err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, err
	}
	return &archiveSpool{File: file}, nil

}

func (h archiveJobHandler) Import(
	ctx context.Context,
	job jobs.Job,
	source io.Reader,
) error {
	var request ArchiveImportRequest

	if err := json.Unmarshal(job.Options, &request); err != nil {
		return fmt.Errorf("decode import job options: %w", err)
	}

	file, err := jobs.NewStagingFile("importexport-reference-import-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	size, err := io.Copy(file, io.LimitReader(&archiveContextReader{ctx: ctx, reader: source}, maxQueuedArchiveSize+1))
	if err != nil {
		return err
	}
	if size > maxQueuedArchiveSize {
		return fmt.Errorf("import archive exceeds %d byte limit", maxQueuedArchiveSize)
	}
	archive, err := zip.NewReader(file, size)
	if err != nil {
		return fmt.Errorf("open import ZIP: %w", err)
	}
	if err := validateQueuedZIP(archive, request.IncludeMedia); err != nil {
		return err
	}

	importCtx, cancel := context.WithTimeout(ctx, h.admin.importTimeout())
	defer cancel()

	_, err = h.admin.importZIPJob(
		importCtx,
		job.WorkspaceID,
		job.ID,
		archive,
		request,
	)

	return err
}

func (a *Admin) importTimeout() time.Duration {
	if a != nil && a.archiveImportTimeout > 0 {
		return a.archiveImportTimeout
	}

	return 15 * time.Minute
}

func validateQueuedZIP(archive *zip.Reader, includeMedia bool) error {
	if len(archive.File) > maxQueuedArchiveFiles {
		return fmt.Errorf("import archive has too many files")
	}

	var total uint64

	for _, file := range archive.File {
		if file.UncompressedSize64 > uint64(
			maxQueuedArchiveUncompressedSize,
		)-total {
			return fmt.Errorf(
				"import archive exceeds %d byte uncompressed limit",
				maxQueuedArchiveUncompressedSize,
			)
		}

		total += file.UncompressedSize64
	}

	return nil
}

// ReconcileImport finishes a committed import even if its source archive is gone.
func (h archiveJobHandler) ReconcileImport(ctx context.Context, job jobs.Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return h.admin.repository.ReconcileArchiveImport(job.WorkspaceID)
}

type archiveSpool struct{ *os.File }

func (s *archiveSpool) Close() error { err := s.File.Close(); _ = os.Remove(s.Name()); return err }

type archiveContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *archiveContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
