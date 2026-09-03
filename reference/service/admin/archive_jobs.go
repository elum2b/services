package admin

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"time"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/internal/utils/importexport/jobs"
)

const (
	maxQueuedArchiveSize             = 512 << 20
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

	reader, writer := io.Pipe()

	go func() {
		err := h.admin.exportZIP(ctx, job.WorkspaceID, request, writer)

		_ = writer.CloseWithError(err)
	}()

	return reader, nil
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

	data, err := io.ReadAll(io.LimitReader(source, maxQueuedArchiveSize+1))
	if err != nil {
		return fmt.Errorf("read import archive: %w", err)
	}

	if len(data) > maxQueuedArchiveSize {
		return fmt.Errorf(
			"import archive exceeds %d byte limit",
			maxQueuedArchiveSize,
		)
	}

	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
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

	if !includeMedia {
		return nil
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
