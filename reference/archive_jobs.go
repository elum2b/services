package reference

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/elum2b/services/internal/utils/importexport/jobs"
	resourcestorage "github.com/elum2b/services/reference/storage"
)

func configureArchiveJobs(
	ctx context.Context,
	service *Reference,
	config resourcestorage.Config,
) error {
	if err := jobs.Bootstrap(ctx, service.client.DB()); err != nil {
		return fmt.Errorf("bootstrap reference archive jobs: %w", err)
	}

	var archive jobs.Archive

	if config.Bucket != "" {
		var err error

		archive, err = newS3Archive(config)
		if err != nil {
			return fmt.Errorf("configure reference S3 archive: %w", err)
		}
	} else {
		directory, err := archiveDirectory(config.Directory)
		if err != nil {
			return err
		}

		archive, err = jobs.NewDiskArchive(directory)
		if err != nil {
			return err
		}
	}

	if err := service.Admin.ConfigureArchiveJobs(
		service.client.DB(),
		archive,
		service.archiveImportTimeout,
		service.archiveJobLease,
	); err != nil {
		return fmt.Errorf("configure reference archive jobs: %w", err)
	}

	return nil
}

func archiveDirectory(resourceDirectory string) (string, error) {
	if resourceDirectory != "" {
		return filepath.Join(resourceDirectory, "importexport"), nil
	}

	binaryPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve reference storage directory: %w", err)
	}

	return filepath.Join(
		filepath.Dir(binaryPath),
		"reference",
		"importexport",
	), nil
}
